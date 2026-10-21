package setup

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/agim/lidza/packs/mail"
	"github.com/agim/lidza/pkg/env"
	"github.com/jackc/pgx/v5"
	"net/url"
	"sort"
	"strings"
	q "thura/db/queries/gen"
	"thura/internal/smipmail"
	"thura/internal/tenancy"
)

func claimInitialMailbox(ctx context.Context, queries *q.Queries, workspace, address, subject string) error {
	boxes, err := queries.ListMailboxes(ctx, workspace)
	if err != nil {
		return err
	}
	for _, box := range boxes {
		if box.Address != address {
			continue
		}
		if box.OwnerSubject != nil && *box.OwnerSubject != subject {
			return errors.New("administrator inbox belongs to another account; resolve the address conflict before activation")
		}
		if box.Private && box.OwnerSubject != nil && *box.OwnerSubject == subject {
			return nil
		}
		n, err := queries.ClaimInitialMailbox(ctx, q.ClaimInitialMailboxParams{WorkspaceID: workspace, Address: address, Subject: subject})
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("administrator inbox ownership could not be established")
		}
		return nil
	}
	return errors.New("administrator inbox is missing; restore its configuration before activation")
}

// Bootstrap the first team's operator-selected domains and existing credentials.
// This is also used on upgrade; an existing team configuration is authoritative
// and is never replaced by the server wizard's defaults.
func initialTeam(ctx context.Context, tx pgx.Tx, id string, v map[string]string) error {
	queries := q.New(tx)
	if _, err := queries.GetTeamSetup(ctx, id); err == nil {
		return nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	public, err := url.Parse(v["APP_URL"])
	if err != nil {
		return err
	}
	_, emailDomain, _ := strings.Cut(inboxAddress(v), "@")
	webDomain := strings.ToLower(public.Hostname())
	names := []string{emailDomain}
	if webDomain != emailDomain {
		names = append(names, webDomain)
	}
	sort.Strings(names)
	var mailDomain q.TeamDomain
	for _, name := range names {
		if err = tenancy.LockDomain(ctx, tx, name); err != nil {
			return err
		}
		d, e := queries.BoundTeamDomain(ctx, name)
		if e == nil && d.WorkspaceID != id {
			return errors.New("initial team domain is already assigned to another team")
		}
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		if errors.Is(e, pgx.ErrNoRows) {
			d, e = queries.AddTeamDomain(ctx, q.AddTeamDomainParams{WorkspaceID: id, Name: name, Challenge: "operator-setup", Mail: name == emailDomain, Web: name == webDomain})
			if e != nil {
				return e
			}
			d, e = queries.ApproveTeamDomain(ctx, q.ApproveTeamDomainParams{WorkspaceID: id, ID: d.ID})
			if e != nil {
				return e
			}
			if _, e = queries.BindTeamDomain(ctx, q.BindTeamDomainParams{Name: name, WorkspaceID: id, DomainID: d.ID}); e != nil {
				return e
			}
		} else if !d.Mail && name == emailDomain || !d.Web && name == webDomain {
			return errors.New("existing initial team domain has conflicting roles; retain its explicit configuration")
		}
		if name == emailDomain {
			mailDomain = d
		}
	}
	var cfg mail.Config
	if err = env.Fill(&cfg, v); err != nil {
		return err
	}
	secret := tenancy.MailSecrets{WorkspaceID: id, DomainID: mailDomain.ID, Config: cfg, InboundSecret: v["MAIL_INBOUND_SECRET"]}
	if v["SMIP_MAIL_ENABLED"] == "true" {
		var identity smipmail.Config
		if err = json.Unmarshal([]byte(v["SMIP_MAIL_CONFIG"]), &identity); err != nil {
			return err
		}
		if identity.Domain != emailDomain {
			return errors.New("initial team SMIP identity domain mismatch")
		}
		secret.Seed = identity.Seed
		secret.NotBefore = identity.NotBefore
		secret.NotAfter = identity.NotAfter
		secret.Keys = identity.Keys
	}
	sealed, err := tenancy.Seal(secret)
	if err != nil {
		return err
	}
	if _, err = queries.SaveTeamMailConfig(ctx, q.SaveTeamMailConfigParams{WorkspaceID: id, DomainID: mailDomain.ID, Provider: v["MAIL_PROVIDER"], Sealed: sealed, Enabled: true, Smip: v["SMIP_MAIL_ENABLED"] == "true", Postfix: v["THURA_MAIL_INBOUND"] == "postfix"}); err != nil {
		return err
	}
	_, err = queries.SaveTeamSetup(ctx, q.SaveTeamSetupParams{WorkspaceID: id, PrimaryDomain: webDomain, Status: "ready"})
	return err
}
