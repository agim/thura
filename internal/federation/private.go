package federation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/admin"
	"thura/internal/smipmail"
	"thura/internal/smiptrust"
)

// PrivateTrust is the "Private deployment" section of the SMIP peers page:
// the pinned SMIP/0.2 mail origins and the CA bundle shared by SMIP/0.1 and
// SMIP/0.2. Saved values are edited here; deployment JSON values are shown
// read-only. Only public material (origins, certificate subjects, issuers,
// validity and fingerprints) reaches the view.
type PrivateTrust struct {
	Origins      []PinnedOriginView
	Saved        *smiptrust.Bundle
	Configured   []ConfiguredBundle
	MaxOrigins   int
	MaxBundleKiB int
	Error        string
}

// PinnedOriginView is one effective pinned origin. Saved rows can be removed;
// a Configured one comes from SMIP_MAIL_CONFIG, and Replaces is the JSON
// origin a saved row replaces for the same domain.
type PinnedOriginView struct {
	Domain, Origin, AddedBy, Replaces string
	AddedAt                           time.Time
	Saved, Configured                 bool
}

// ConfiguredBundle is a caFile from the deployment JSON of one profile.
type ConfiguredBundle struct {
	Profile, File, Error string
	Certificates         []smiptrust.Certificate
	Overridden           bool
}

// maxUpload bounds the CA upload form; the bundle itself is bounded smaller.
const maxUpload = 2 * smiptrust.MaxBundleBytes

func privateTrust(ctx context.Context) PrivateTrust {
	view := PrivateTrust{MaxOrigins: smiptrust.MaxOrigins, MaxBundleKiB: smiptrust.MaxBundleBytes >> 10}
	mail := smipmail.From(ctx)
	configured := mail.ConfiguredOrigins()
	saved, err := smiptrust.Origins(ctx)
	if err != nil {
		view.Error = "The saved private-deployment settings could not be read."
		return view
	}
	byDomain := map[string]*PinnedOriginView{}
	for domain, origin := range configured {
		byDomain[domain] = &PinnedOriginView{Domain: domain, Origin: origin, Configured: true}
	}
	for _, row := range saved {
		entry := &PinnedOriginView{Domain: row.Domain, Origin: row.Origin, AddedBy: row.AddedBy, AddedAt: row.AddedAt, Saved: true}
		if old, ok := byDomain[row.Domain]; ok {
			entry.Configured, entry.Replaces = true, old.Origin
		}
		byDomain[row.Domain] = entry
	}
	for _, entry := range byDomain {
		view.Origins = append(view.Origins, *entry)
	}
	sort.Slice(view.Origins, func(i, j int) bool { return view.Origins[i].Domain < view.Origins[j].Domain })
	bundle, ok, err := smiptrust.SavedBundle(ctx)
	if err != nil {
		view.Error = err.Error()
	} else if ok {
		view.Saved = &bundle
	}
	now := lidza.Now(ctx)
	for _, source := range []struct {
		profile string
		load    func() (string, []byte)
	}{{"SMIP/0.2 mail (SMIP_MAIL_CONFIG)", mail.ConfiguredCA}, {"SMIP/0.1 chat and files (SMIP_CONFIG)", From(ctx).ConfiguredCA}} {
		file, raw := source.load()
		if file == "" {
			continue
		}
		entry := ConfiguredBundle{Profile: source.profile, File: file, Overridden: ok}
		if certs, err := smiptrust.ParseBundle(raw, now); err != nil {
			entry.Error = err.Error()
		} else {
			entry.Certificates = certs
		}
		view.Configured = append(view.Configured, entry)
	}
	return view
}

func pinOrigin(r *http.Request) (string, error) {
	domain, origin, err := smiptrust.PinOrigin(r.Context(), r.Form.Get("domain"), r.Form.Get("origin"))
	if err != nil {
		return "", err
	}
	smipmail.ForgetStatus(domain)
	return "Pinned " + domain + " to " + origin + ". Mail discovery and delivery to it use this origin from the next attempt on every node; its certificate must verify against the system roots or the CA bundle below.", nil
}

func unpinOrigin(r *http.Request) (string, error) {
	domain, err := smiptrust.UnpinOrigin(r.Context(), r.Form.Get("domain"))
	if err != nil {
		return "", err
	}
	smipmail.ForgetStatus(domain)
	return "Removed the pinned origin of " + domain + ". A deployment configuration origin for it, if any, applies again; otherwise smip." + domain + " is discovered through public DNS.", nil
}

func saveBundle(r *http.Request) (string, error) {
	raw := []byte(r.Form.Get("pem"))
	if r.MultipartForm != nil {
		if files := r.MultipartForm.File["file"]; len(files) > 0 {
			if strings.TrimSpace(string(raw)) != "" {
				return "", errors.New("paste the bundle or choose a file, not both")
			}
			file, err := files[0].Open()
			if err != nil {
				return "", errors.New("the uploaded file could not be read")
			}
			defer file.Close()
			raw, err = io.ReadAll(io.LimitReader(file, smiptrust.MaxBundleBytes+1))
			if err != nil {
				return "", errors.New("the uploaded file could not be read")
			}
		}
	}
	if strings.TrimSpace(string(raw)) == "" {
		return "", errors.New("paste a PEM CA bundle or choose a .pem file")
	}
	certs, err := smiptrust.SaveBundle(r.Context(), raw)
	if err != nil {
		return "", err
	}
	smipmail.ForgetAllStatus()
	return fmt.Sprintf("Saved %d CA certificate(s). SMIP/0.1 and SMIP/0.2 connections trust them beside the system roots from the next request on every node; the deployment configuration caFile no longer applies.", len(certs)), nil
}

func clearBundle(r *http.Request) (string, error) {
	if r.Form.Get("confirm") != "clear" {
		return "", errors.New("confirm removing the CA bundle")
	}
	if err := smiptrust.ClearBundle(r.Context()); err != nil {
		return "", err
	}
	smipmail.ForgetAllStatus()
	return "Removed the saved CA bundle. Connections trust the system roots and any deployment configuration caFile from the next request.", nil
}

// privateActions are the forms of the Private deployment section.
func privateActions() map[string]admin.Action {
	return map[string]admin.Action{"origin-pin": pinOrigin, "origin-unpin": unpinOrigin, "ca-save": saveBundle, "ca-clear": clearBundle}
}
