package federation

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/audit"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	q "thura/db/queries/gen"
	"thura/internal/smip"
)

// Peer states in smip_peer. A pending row is a fetched proposal awaiting the
// operator's fingerprint confirmation and never enters trust; a trusted row
// is confirmed; a revoked row refuses its key and the whole domain's sends.
const (
	PeerPending = "pending"
	PeerTrusted = "trusted"
	PeerRevoked = "revoked"
)

// ProposalLifetime bounds how long a fetched proposal can be confirmed.
const ProposalLifetime = time.Hour

// Trust is the merged view of the legacy JSON peers and the database peers,
// read for one request or attempt. Per key ID the database wins; a key
// revoked in either source is revoked; a pending proposal has no effect. A
// domain keeps a sending endpoint only while it has an unrevoked key.
type Trust struct {
	Peers   map[string]map[string]smip.Key
	Origins map[string]string
}

func (g *Gateway) Trust(ctx context.Context) (Trust, error) {
	trust := Trust{Peers: map[string]map[string]smip.Key{}, Origins: map[string]string{}}
	for domain, keys := range g.peers {
		copied := map[string]smip.Key{}
		for id, key := range keys {
			copied[id] = key
		}
		trust.Peers[domain] = copied
	}
	for domain, origin := range g.origins {
		trust.Origins[domain] = origin
	}
	if g.domain == "" {
		return trust, nil
	}
	rows, err := q.New(db.From(ctx)).ListSmipPeers(ctx, g.domain)
	if err != nil {
		return Trust{}, err
	}
	for _, row := range rows {
		if row.State == PeerPending || row.State == PeerTrusted && !row.Confirmed {
			continue
		}
		key, err := peerKey(row)
		if err != nil {
			continue
		}
		if old, ok := trust.Peers[row.Domain][row.KeyID]; ok && old.Revoked {
			key.Revoked = true
		}
		if row.State == PeerRevoked {
			key.Revoked = true
		}
		if trust.Peers[row.Domain] == nil {
			trust.Peers[row.Domain] = map[string]smip.Key{}
		}
		trust.Peers[row.Domain][row.KeyID] = key
		if row.State == PeerTrusted && row.Origin != "" {
			trust.Origins[row.Domain] = row.Origin
		}
	}
	for domain, origin := range trust.Origins {
		if !hasActiveKey(trust.Peers[domain]) {
			delete(trust.Origins, domain)
			continue
		}
		if client, err := g.newClient(origin, domain, trust.Peers[domain]); err != nil {
			delete(trust.Origins, domain)
		} else {
			client.Close()
		}
	}
	return trust, nil
}

func hasActiveKey(keys map[string]smip.Key) bool {
	for _, key := range keys {
		if !key.Revoked {
			return true
		}
	}
	return false
}

func peerKey(row q.SmipPeer) (smip.Key, error) {
	public, err := base64.StdEncoding.DecodeString(row.PublicKey)
	if err != nil || len(public) != ed25519.PublicKeySize || smip.KeyID(public) != row.KeyID || !row.NotAfter.After(row.NotBefore) {
		return smip.Key{}, errors.New("corrupt SMIP peer key")
	}
	return smip.Key{Public: public, NotBefore: row.NotBefore.Unix(), NotAfter: row.NotAfter.Unix(), Revoked: row.Revoked}, nil
}

// PeerKey and PeerView are what the admin page shows: public material only.
type PeerKey struct {
	KeyID, Fingerprint  string
	NotBefore, NotAfter time.Time
	Revoked, Current    bool
}
type PeerView struct {
	Domain, Origin, State, PairedBy, Notes string
	PairedAt                               *time.Time
	FetchedAt                              time.Time
	Keys                                   []PeerKey
	// Configured marks a peer the sealed SMIP_CONFIG JSON also lists.
	Configured, Expired, Sendable bool
}

// OwnKeys lists this server's signing keys for the operator to read out.
func (g *Gateway) OwnKeys() []PeerKey {
	current := smip.KeyID(g.signer.Private.Public().(ed25519.PublicKey))
	var out []PeerKey
	for id, key := range g.originKeys {
		out = append(out, PeerKey{KeyID: id, Fingerprint: Fingerprint(id), NotBefore: time.Unix(key.NotBefore, 0).UTC(), NotAfter: time.Unix(key.NotAfter, 0).UTC(), Revoked: key.Revoked, Current: id == current})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Current || !out[j].Current && out[i].KeyID < out[j].KeyID })
	return out
}

// Peers lists every paired, pending and revoked peer of this server, with
// the legacy JSON peers folded in so the page shows one list.
func (g *Gateway) Peers(ctx context.Context) ([]PeerView, error) {
	views := map[string]*PeerView{}
	for domain, keys := range g.peers {
		view := &PeerView{Domain: domain, Origin: g.origins[domain], State: PeerTrusted, Configured: true, Notes: "From the sealed SMIP_CONFIG"}
		for id, key := range keys {
			view.Keys = append(view.Keys, PeerKey{KeyID: id, Fingerprint: Fingerprint(id), NotBefore: time.Unix(key.NotBefore, 0).UTC(), NotAfter: time.Unix(key.NotAfter, 0).UTC(), Revoked: key.Revoked})
		}
		views[domain] = view
	}
	if g.domain != "" {
		rows, err := q.New(db.From(ctx)).ListSmipPeers(ctx, g.domain)
		if err != nil {
			return nil, err
		}
		now := lidza.Now(ctx)
		for _, row := range rows {
			id := row.Domain
			if row.State == PeerPending {
				id = row.Domain + "\x00pending"
			}
			view := views[id]
			if view == nil {
				view = &PeerView{Domain: row.Domain, State: row.State}
				views[id] = view
			}
			if row.State == PeerTrusted && row.Origin != "" {
				view.Origin = row.Origin
			}
			if row.PairedBy != "" {
				view.PairedBy, view.PairedAt = row.PairedBy, row.PairedAt
			}
			if row.FetchedAt.After(view.FetchedAt) {
				view.FetchedAt = row.FetchedAt
			}
			if row.Notes != "" {
				view.Notes = row.Notes
			}
			view.Expired = row.State == PeerPending && now.After(row.FetchedAt.Add(ProposalLifetime))
			replaced := false
			key := PeerKey{KeyID: row.KeyID, Fingerprint: Fingerprint(row.KeyID), NotBefore: row.NotBefore, NotAfter: row.NotAfter, Revoked: row.Revoked}
			for i := range view.Keys {
				if view.Keys[i].KeyID == row.KeyID {
					view.Keys[i], replaced = key, true
				}
			}
			if !replaced {
				view.Keys = append(view.Keys, key)
			}
		}
	}
	trust, err := g.Trust(ctx)
	if err != nil {
		return nil, err
	}
	var out []PeerView
	for _, view := range views {
		// A settled peer is revoked once none of its keys can sign any more.
		if view.State != PeerPending {
			view.State = PeerRevoked
			for i := range view.Keys {
				if key, ok := trust.Peers[view.Domain][view.Keys[i].KeyID]; ok {
					view.Keys[i].Revoked = key.Revoked
				}
				if !view.Keys[i].Revoked {
					view.State = PeerTrusted
				}
			}
		}
		view.Sendable = view.State == PeerTrusted && trust.Origins[view.Domain] != ""
		sort.Slice(view.Keys, func(i, j int) bool { return view.Keys[i].KeyID < view.Keys[j].KeyID })
		out = append(out, *view)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Domain != out[j].Domain {
			return out[i].Domain < out[j].Domain
		}
		return out[i].State < out[j].State
	})
	return out, nil
}

// Propose fetches and verifies the peer's identity document and records it
// as a pending proposal: nothing is trusted until Confirm. A newer proposal
// for the same domain replaces an older one.
func (g *Gateway) Propose(ctx context.Context, domain, origin string) (PeerView, error) {
	if !g.Enabled() {
		return PeerView{}, errors.New("SMIP is disabled on this server")
	}
	document, used, err := g.FetchIdentity(ctx, domain, origin)
	if err != nil {
		return PeerView{}, err
	}
	stored := ""
	if strings.TrimSpace(origin) != "" {
		stored = used
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return PeerView{}, err
	}
	defer tx.Rollback(ctx)
	queries := q.New(tx)
	if err = queries.LockSmipPeers(ctx, g.domain); err != nil {
		return PeerView{}, err
	}
	count, err := queries.CountSmipPeerDomains(ctx, g.domain)
	if err != nil {
		return PeerView{}, err
	}
	if count >= MaxPeers {
		return PeerView{}, fmt.Errorf("at most %d peers can be paired", MaxPeers)
	}
	if _, err = queries.DeleteSmipPeerState(ctx, q.DeleteSmipPeerStateParams{Owner: g.domain, Domain: document.Domain, State: PeerPending}); err != nil {
		return PeerView{}, err
	}
	// A key this server already trusts stays trusted and needs no second
	// confirmation; a key it revoked stays revoked for good, so a compromised
	// key can never be re-paired: the peer has to rotate.
	settled := map[string]string{}
	for _, state := range []string{PeerTrusted, PeerRevoked} {
		rows, err := queries.ListSmipPeerDomain(ctx, q.ListSmipPeerDomainParams{Owner: g.domain, Domain: document.Domain, State: state})
		if err != nil {
			return PeerView{}, err
		}
		for _, row := range rows {
			settled[row.KeyID] = state
		}
	}
	now := lidza.Now(ctx)
	view := PeerView{Domain: document.Domain, Origin: stored, State: PeerPending, FetchedAt: now}
	pending := 0
	for id, key := range document.Keys {
		item := PeerKey{KeyID: id, Fingerprint: Fingerprint(id), NotBefore: time.Unix(key.NotBefore, 0).UTC(), NotAfter: time.Unix(key.NotAfter, 0).UTC(), Revoked: key.Revoked || settled[id] == PeerRevoked, Current: id == document.KeyID}
		view.Keys = append(view.Keys, item)
		if settled[id] != "" {
			continue
		}
		if err = queries.UpsertSmipPeer(ctx, q.UpsertSmipPeerParams{Owner: g.domain, Domain: document.Domain, Origin: stored, KeyID: id, PublicKey: base64.StdEncoding.EncodeToString(key.Public), NotBefore: item.NotBefore, NotAfter: item.NotAfter, Revoked: key.Revoked, State: PeerPending, Confirmed: false, PairedBy: "", FetchedAt: now, Notes: "Fetched from " + used}); err != nil {
			return PeerView{}, err
		}
		pending++
	}
	if pending == 0 {
		if settled[document.KeyID] == PeerRevoked {
			return PeerView{}, errors.New("every key in that identity is revoked here; the peer has to rotate its signing key before it can be paired again")
		}
		return PeerView{}, errors.New("every key in that identity is already paired; nothing to confirm")
	}
	sort.Slice(view.Keys, func(i, j int) bool { return view.Keys[i].KeyID < view.Keys[j].KeyID })
	if err = audit.From(ctx).RecordTx(ctx, tx, audit.Event{Action: "smip.peer.propose", Resource: "smip-peer/" + document.Domain, Meta: map[string]string{"domain": document.Domain, "endpoint": used, "keys": fmt.Sprint(pending)}}); err != nil {
		return PeerView{}, err
	}
	return view, tx.Commit(ctx)
}

// Confirm turns the pending proposal for domain into trust, only when the
// operator ticked the verification box and the confirmed key IDs are exactly
// the proposal's. The proposal must not have expired.
func (g *Gateway) Confirm(ctx context.Context, domain string, keyIDs []string, verified bool) error {
	if !g.Enabled() {
		return errors.New("SMIP is disabled on this server")
	}
	if !verified {
		return errors.New("confirm that you verified the fingerprint with the peer's operator before pairing")
	}
	domain = strings.ToLower(strings.TrimSpace(domain))
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	queries := q.New(tx)
	if err = queries.LockSmipPeers(ctx, g.domain); err != nil {
		return err
	}
	pending, err := queries.ListSmipPeerDomain(ctx, q.ListSmipPeerDomainParams{Owner: g.domain, Domain: domain, State: PeerPending})
	if err != nil {
		return err
	}
	now := lidza.Now(ctx)
	if len(pending) == 0 {
		return errors.New("no pending proposal for that domain; fetch its identity first")
	}
	confirmed := map[string]bool{}
	for _, id := range keyIDs {
		confirmed[id] = true
	}
	if len(confirmed) != len(pending) {
		return errors.New("the confirmed fingerprints do not match the fetched identity; fetch it again")
	}
	for _, row := range pending {
		if !confirmed[row.KeyID] {
			return errors.New("the confirmed fingerprints do not match the fetched identity; fetch it again")
		}
		if now.After(row.FetchedAt.Add(ProposalLifetime)) {
			return errors.New("the proposal is older than an hour; fetch the identity again")
		}
	}
	user := auth.CurrentUser(ctx)
	if user == nil {
		return errors.New("sign in as the server administrator")
	}
	if _, err = queries.DeleteSmipPeerState(ctx, q.DeleteSmipPeerStateParams{Owner: g.domain, Domain: domain, State: PeerPending}); err != nil {
		return err
	}
	for _, row := range pending {
		pairedAt := now
		if err = queries.UpsertSmipPeer(ctx, q.UpsertSmipPeerParams{Owner: g.domain, Domain: domain, Origin: row.Origin, KeyID: row.KeyID, PublicKey: row.PublicKey, NotBefore: row.NotBefore, NotAfter: row.NotAfter, Revoked: row.Revoked, State: PeerTrusted, Confirmed: true, PairedBy: user.ID, PairedAt: &pairedAt, FetchedAt: row.FetchedAt, Notes: row.Notes}); err != nil {
			return err
		}
	}
	if err = audit.From(ctx).RecordTx(ctx, tx, audit.Event{Action: "smip.peer.pair", Resource: "smip-peer/" + domain, Meta: map[string]string{"domain": domain, "keys": fmt.Sprint(len(pending))}}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Discard drops the pending proposal for domain without trusting anything.
func (g *Gateway) Discard(ctx context.Context, domain string) error {
	domain = strings.ToLower(strings.TrimSpace(domain))
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	queries := q.New(tx)
	if err = queries.LockSmipPeers(ctx, g.domain); err != nil {
		return err
	}
	n, err := queries.DeleteSmipPeerState(ctx, q.DeleteSmipPeerStateParams{Owner: g.domain, Domain: domain, State: PeerPending})
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.New("no pending proposal for that domain")
	}
	if err = audit.From(ctx).RecordTx(ctx, tx, audit.Event{Action: "smip.peer.discard", Resource: "smip-peer/" + domain, Meta: map[string]string{"domain": domain}}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Revoke refuses every key of domain from now on, including keys the sealed
// JSON lists (they get a revoked row that wins the merge), and removes its
// sending endpoint. Accepted receipts and duplicate detection are kept.
func (g *Gateway) Revoke(ctx context.Context, domain string) error {
	if !g.Enabled() {
		return errors.New("SMIP is disabled on this server")
	}
	domain = strings.ToLower(strings.TrimSpace(domain))
	if !smip.ValidDomain(domain) {
		return errors.New("choose a paired domain")
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	queries := q.New(tx)
	if err = queries.LockSmipPeers(ctx, g.domain); err != nil {
		return err
	}
	now := lidza.Now(ctx)
	user := auth.CurrentUser(ctx)
	if user == nil {
		return errors.New("sign in as the server administrator")
	}
	note := "Revoked by the server administrator"
	if _, err = queries.DeleteSmipPeerState(ctx, q.DeleteSmipPeerStateParams{Owner: g.domain, Domain: domain, State: PeerPending}); err != nil {
		return err
	}
	n, err := queries.RevokeSmipPeer(ctx, q.RevokeSmipPeerParams{Owner: g.domain, Domain: domain, Notes: note, FetchedAt: now})
	if err != nil {
		return err
	}
	for id, key := range g.peers[domain] {
		rows, err := queries.ListSmipPeerDomain(ctx, q.ListSmipPeerDomainParams{Owner: g.domain, Domain: domain, State: PeerRevoked})
		if err != nil {
			return err
		}
		known := false
		for _, row := range rows {
			known = known || row.KeyID == id
		}
		if known {
			continue
		}
		if err = queries.UpsertSmipPeer(ctx, q.UpsertSmipPeerParams{Owner: g.domain, Domain: domain, Origin: "", KeyID: id, PublicKey: base64.StdEncoding.EncodeToString(key.Public), NotBefore: time.Unix(key.NotBefore, 0).UTC(), NotAfter: time.Unix(key.NotAfter, 0).UTC(), Revoked: true, State: PeerRevoked, Confirmed: false, PairedBy: user.ID, FetchedAt: now, Notes: note}); err != nil {
			return err
		}
		n++
	}
	if n == 0 {
		return errors.New("that domain is not paired")
	}
	if err = audit.From(ctx).RecordTx(ctx, tx, audit.Event{Action: "smip.peer.revoke", Resource: "smip-peer/" + domain, Meta: map[string]string{"domain": domain, "keys": fmt.Sprint(n)}}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
