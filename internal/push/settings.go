package push

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/router"
	"github.com/jackc/pgx/v5"
	q "thura/db/queries/gen"
	"thura/internal/workspace"
	"thura/schema"
)

// maxDevices bounds the browsers one member registers in one workspace.
const maxDevices = 20

// Settings, devices and preferences are a member's own: every read and
// write is scoped to the workspace and the signed-in subject, and another
// member's device is a 404.
func member(ctx context.Context, w string) (string, error) {
	if err := workspace.RequireMember(ctx, w); err != nil {
		return "", err
	}
	return auth.CurrentUser(ctx).ID, nil
}

func preferenceView(p q.NotificationPreference) schema.NotificationPreferences {
	return schema.NotificationPreferences{Mail: p.Mail, MailPeopleOnly: p.MailPeopleOnly, Chat: p.Chat, ChatRequests: p.ChatRequests, Calendar: p.Calendar, Meet: p.Meet}
}

func emptySettings() schema.NotificationSettings {
	return schema.NotificationSettings{Devices: []schema.PushDevice{}, Removed: []schema.PushRemoval{}, Recent: []schema.NotificationLogEntry{}}
}

func deviceView(r q.PushSubscription) schema.PushDevice {
	return schema.PushDevice{ID: r.ID, Label: r.Label, EndpointHash: r.EndpointHash, CreatedAt: r.CreatedAt, LastUsedAt: r.LastUsedAt,
		LastAttemptAt: r.LastAttemptAt, LastKind: r.LastKind, LastErrorAt: r.LastErrorAt, LastErrorKind: r.LastErrorKind, LastError: r.LastError,
		DeliveredCount: int(r.DeliveredCount), FailedCount: int(r.FailedCount)}
}

func settings(ctx context.Context, queries *q.Queries, w, subject string) (schema.NotificationSettings, error) {
	out := emptySettings()
	out.Preferences = preferenceView(defaults())
	p, err := queries.GetNotificationPreference(ctx, q.GetNotificationPreferenceParams{WorkspaceID: w, Subject: subject})
	if err == nil {
		out.Preferences = preferenceView(p)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	rows, err := queries.ListPushDevices(ctx, q.ListPushDevicesParams{WorkspaceID: w, Subject: subject})
	if err != nil {
		return out, err
	}
	for _, r := range rows {
		out.Devices = append(out.Devices, deviceView(r))
	}
	removed, err := queries.ListPushRemovedDevices(ctx, q.ListPushRemovedDevicesParams{WorkspaceID: w, Subject: subject})
	if err != nil {
		return out, err
	}
	for _, r := range removed {
		out.Removed = append(out.Removed, schema.PushRemoval{Label: r.Label, EndpointHash: r.EndpointHash, ErrorKind: r.ErrorKind, Error: r.Error, RemovedAt: r.RemovedAt})
	}
	recent, err := queries.ListNotificationLog(ctx, q.ListNotificationLogParams{WorkspaceID: w, Subject: subject, Limit: recentLimit})
	if err != nil {
		return out, err
	}
	for _, r := range recent {
		out.Recent = append(out.Recent, schema.NotificationLogEntry{ID: r.ID, Kind: r.Kind, Link: r.Link, Status: r.Status, Reason: r.Reason, Error: r.Error, CreatedAt: r.CreatedAt})
	}
	out.PublicKey = PublicKey(ctx)
	return out, nil
}

// Settings is the signed-in member's notification settings in a workspace.
func Settings(ctx context.Context, w string) (schema.NotificationSettings, error) {
	subject, err := member(ctx, w)
	if err != nil {
		return emptySettings(), err
	}
	return settings(ctx, q.New(db.From(ctx)), w, subject)
}

// SavePreferences stores which events notify the member.
func SavePreferences(ctx context.Context, w string, in schema.NotificationPreferences) (schema.NotificationSettings, error) {
	subject, err := member(ctx, w)
	if err != nil {
		return emptySettings(), err
	}
	queries := q.New(db.From(ctx))
	if _, err = queries.SaveNotificationPreference(ctx, q.SaveNotificationPreferenceParams{WorkspaceID: w, Subject: subject, Mail: in.Mail, MailPeopleOnly: in.MailPeopleOnly, Chat: in.Chat, ChatRequests: in.ChatRequests, Calendar: in.Calendar, Meet: in.Meet}); err != nil {
		return emptySettings(), err
	}
	return settings(ctx, queries, w, subject)
}

// EndpointHash identifies a browser's subscription without revealing its
// endpoint (a capability URL): the page hashes its own endpoint the same way
// to find "This device".
func EndpointHash(endpoint string) string {
	sum := sha256.Sum256([]byte(endpoint))
	return hex.EncodeToString(sum[:])
}

func keyLength(raw string, want int) bool {
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(raw, "="))
	return err == nil && len(b) == want
}

// cleanLabel keeps a short printable device name ("Chrome on Android").
func cleanLabel(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	s = clip(s, 120)
	if s == "" {
		return "Browser"
	}
	return s
}

// Subscribe registers this browser for the member. The endpoint must be a
// known browser push service and the keys the sizes RFC 8291 defines.
func Subscribe(ctx context.Context, w string, in schema.PushSubscribeInput) (schema.NotificationSettings, error) {
	empty := emptySettings()
	subject, err := member(ctx, w)
	if err != nil {
		return empty, err
	}
	if err = in.Validate(); err != nil {
		return empty, err
	}
	if err = CheckEndpoint(in.Endpoint); err != nil {
		return empty, router.Errorf(422, "%s", err.Error())
	}
	if !keyLength(in.P256dh, 65) || !keyLength(in.Auth, 16) {
		return empty, router.Errorf(422, "invalid subscription keys")
	}
	if PublicKey(ctx) == "" {
		return empty, router.Errorf(503, "%s", ErrUnavailable.Error())
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	queries := q.New(tx)
	if _, err = queries.LockWorkspace(ctx, w); err != nil {
		return empty, err
	}
	hash := EndpointHash(in.Endpoint)
	count, err := queries.CountPushDevices(ctx, q.CountPushDevicesParams{WorkspaceID: w, Subject: subject})
	if err != nil {
		return empty, err
	}
	if count >= maxDevices {
		known := false
		rows, err := queries.ListPushDevices(ctx, q.ListPushDevicesParams{WorkspaceID: w, Subject: subject})
		if err != nil {
			return empty, err
		}
		for _, r := range rows {
			known = known || r.EndpointHash == hash
		}
		if !known {
			return empty, router.Errorf(409, "too many devices; remove one first")
		}
	}
	if _, err = queries.SavePushSubscription(ctx, q.SavePushSubscriptionParams{WorkspaceID: w, Subject: subject, Endpoint: in.Endpoint, EndpointHash: hash, P256dh: strings.TrimRight(in.P256dh, "="), Auth: strings.TrimRight(in.Auth, "="), Label: cleanLabel(in.Label)}); err != nil {
		return empty, err
	}
	// Subscribed again: the reason it was removed no longer applies.
	if err = queries.ClearPushRemovedDevice(ctx, q.ClearPushRemovedDeviceParams{WorkspaceID: w, Subject: subject, EndpointHash: hash}); err != nil {
		return empty, err
	}
	if err = tx.Commit(ctx); err != nil {
		return empty, err
	}
	return settings(ctx, q.New(db.From(ctx)), w, subject)
}

// Unsubscribe removes one of the member's devices.
func Unsubscribe(ctx context.Context, w, id string) (schema.NotificationSettings, error) {
	empty := emptySettings()
	subject, err := member(ctx, w)
	if err != nil {
		return empty, err
	}
	if !workspace.ValidID(id) {
		return empty, router.Errorf(404, "device not found")
	}
	queries := q.New(db.From(ctx))
	n, err := queries.DeletePushDevice(ctx, q.DeletePushDeviceParams{WorkspaceID: w, Subject: subject, ID: id})
	if err != nil {
		return empty, err
	}
	if n == 0 {
		return empty, router.Errorf(404, "device not found")
	}
	return settings(ctx, queries, w, subject)
}

// Test sends a test notification to the member's devices in this workspace.
func Test(ctx context.Context, w string) (schema.PushTestResult, error) {
	subject, err := member(ctx, w)
	if err != nil {
		return schema.PushTestResult{}, err
	}
	if PublicKey(ctx) == "" {
		return schema.PushTestResult{}, router.Errorf(503, "%s", ErrUnavailable.Error())
	}
	n, err := Notify(ctx, nil, w, []string{subject}, testEvent(w))
	if err != nil {
		return schema.PushTestResult{}, err
	}
	if n == 0 {
		return schema.PushTestResult{}, router.Errorf(409, "turn on notifications on a device first")
	}
	return schema.PushTestResult{Queued: n}, nil
}

// TestDevice sends a test notification to one of the member's devices now,
// in the request, and answers with what the push service said: delivered
// (and by which service) or the exact failure, recorded on the device and
// in the member's notification log like any delivery.
func TestDevice(ctx context.Context, w, id string) (schema.PushDeviceTestResult, error) {
	out := schema.PushDeviceTestResult{Settings: emptySettings()}
	subject, err := member(ctx, w)
	if err != nil {
		return out, err
	}
	if !workspace.ValidID(id) {
		return out, router.Errorf(404, "device not found")
	}
	if PublicKey(ctx) == "" {
		return out, router.Errorf(503, "%s", ErrUnavailable.Error())
	}
	queries := q.New(db.From(ctx))
	sub, err := queries.GetPushSubscription(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (sub.WorkspaceID != w || sub.Subject != subject) {
		return out, router.Errorf(404, "device not found")
	}
	if err != nil {
		return out, err
	}
	e := testEvent(w)
	logID := logEntry(ctx, queries, w, subject, e, StatusQueued, "", 1)
	body, err := encode(e, w, logID)
	if err != nil {
		return out, err
	}
	job := schema.PushJob{SubscriptionID: sub.ID, Kind: KindTest, Payload: string(body), Ttl: int((10 * time.Minute) / time.Second), Urgency: "high", Log: logID}
	o, err := deliver(ctx, queries, sub, job)
	if err != nil {
		return out, err
	}
	o.retry = nil // a test is not retried: the member sees the answer now
	o.message = strings.Replace(strings.Replace(o.message, "; Thura tries again.", ".", 1), "; it tries again.", ".", 1)
	record(ctx, queries, sub, jobInfo{kind: KindTest, log: logID}, o, true)
	out.Delivered, out.Status, out.ErrorKind, out.Error, out.Service = o.delivered, o.status, o.kind, o.message, serviceName(sub.Endpoint)
	if out.Settings, err = settings(ctx, queries, w, subject); err != nil {
		return out, err
	}
	return out, nil
}

func testEvent(w string) Event {
	return Event{Kind: KindTest, Title: "Thura notifications are on", Body: "This device will show notifications you choose in Settings.", URL: "/app/settings/notifications?workspace=" + w, Tag: "thura-test", TTL: 10 * time.Minute}
}

// MarkOpen records that a notification showed nothing because a focused
// tab of the member already showed its view. Another member's entry is a 404.
func MarkOpen(ctx context.Context, w, id string) (schema.NotificationSettings, error) {
	subject, err := member(ctx, w)
	if err != nil {
		return emptySettings(), err
	}
	if !workspace.ValidID(id) {
		return emptySettings(), router.Errorf(404, "notification not found")
	}
	queries := q.New(db.From(ctx))
	n, err := queries.NotificationLogOpen(ctx, q.NotificationLogOpenParams{WorkspaceID: w, Subject: subject, ID: id})
	if err != nil {
		return emptySettings(), err
	}
	if n == 0 {
		return emptySettings(), router.Errorf(404, "notification not found")
	}
	return settings(ctx, queries, w, subject)
}
