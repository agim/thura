package push

import (
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/SherClockHolmes/webpush-go"
	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/jobs"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	q "thura/db/queries/gen"
	"thura/schema"
)

// SendJob delivers one notification to one subscription.
const SendJob = "thura.push.send"

// Event kinds; each but KindTest is a member's checkbox in Settings →
// Notifications.
const (
	KindMail        = "mail"
	KindChat        = "chat"
	KindChatRequest = "chat_request"
	KindCalendar    = "calendar"
	KindMeet        = "meet"
	KindTest        = "test"
)

// Payload limits: a notification is a short title and preview, never a
// message body. Browsers cap the encrypted record at about 4 KB.
const (
	maxTitle = 80
	maxBody  = 140
	maxURL   = 512
)

// View names the page an event belongs to, so the service worker can skip
// the system notification while a focused tab already shows it: the path,
// parameters that must match (a missing one reads as its default) and
// parameters that match when equal or absent (the workspace and mailbox a
// tab shows by default are not in its address).
type View struct {
	Path     string            `json:"path"`
	Params   map[string]string `json:"params,omitempty"`
	Defaults map[string]string `json:"defaults,omitempty"`
	Loose    map[string]string `json:"loose,omitempty"`
}

// Event is one thing that happened, for the members it concerns.
type Event struct {
	Kind  string
	Title string
	Body  string
	URL   string
	Tag   string
	View  *View
	// Actor is the member who caused it; they are never notified.
	Actor string
	// Person is set on mail the Real person rules recognised.
	Person bool
	// TTL is how long a push service keeps it for an offline device.
	TTL time.Duration
}

type payload struct {
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
	URL   string `json:"url"`
	Tag   string `json:"tag,omitempty"`
	View  *View  `json:"view,omitempty"`
	// Workspace and Log name the member's log entry, so a tab that showed
	// the view instead can report it ("Skipped: you had it open").
	Workspace string `json:"workspace,omitempty"`
	Log       string `json:"log,omitempty"`
}

func clip(s string, n int) string {
	s = strings.Join(strings.Fields(strings.ToValidUTF8(s, "")), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

// Encode is the bounded payload sent for an event.
func Encode(e Event) ([]byte, error) {
	return encode(e, "", "")
}

func encode(e Event, workspaceID, logID string) ([]byte, error) {
	title := clip(e.Title, maxTitle)
	if title == "" {
		title = "Thura"
	}
	return json.Marshal(payload{Kind: e.Kind, Title: title, Body: clip(e.Body, maxBody), URL: safeLink(e.URL), Tag: clip(e.Tag, 64), View: e.View, Workspace: workspaceID, Log: logID})
}

func defaults() q.NotificationPreference {
	return q.NotificationPreference{Mail: true, MailPeopleOnly: true, Chat: true, ChatRequests: true, Calendar: true, Meet: true}
}

// Wants reports whether a member's preferences let an event notify them.
func Wants(p q.NotificationPreference, e Event) bool {
	return declined(p, e) == ""
}

// declined is why a member's preferences decline an event ("" when they
// want it): mail not from a real person while "Only mail from real people"
// is chosen, or a kind turned off.
func declined(p q.NotificationPreference, e Event) string {
	on := false
	switch e.Kind {
	case KindMail:
		if p.Mail && p.MailPeopleOnly && !e.Person {
			return SkipPeopleOnly
		}
		on = p.Mail
	case KindChat:
		on = p.Chat
	case KindChatRequest:
		on = p.ChatRequests
	case KindCalendar:
		on = p.Calendar
	case KindMeet:
		on = p.Meet
	case KindTest:
		on = true
	}
	if !on {
		return SkipOff
	}
	return ""
}

// Notify queues the event for each recipient's devices, within tx when one
// is given (so it commits with the change that caused it). It skips the
// actor, members who left, members whose preferences decline the event and
// members without a device; it never fails because nobody is subscribed.
// Each member with a device gets a log entry: queued, or skipped with the
// reason. It returns the number of deliveries queued.
func Notify(ctx context.Context, tx pgx.Tx, workspaceID string, recipients []string, e Event) (int, error) {
	return notify(ctx, tx, workspaceID, recipients, e, "")
}

// Skip records, for each recipient with a device, that the event did not
// notify them and why (mail held as spam), without queueing anything.
func Skip(ctx context.Context, tx pgx.Tx, workspaceID string, recipients []string, e Event, reason string) error {
	_, err := notify(ctx, tx, workspaceID, recipients, e, reason)
	return err
}

func notify(ctx context.Context, tx pgx.Tx, workspaceID string, recipients []string, e Event, skip string) (int, error) {
	seen := map[string]bool{}
	subjects := []string{}
	for _, s := range recipients {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		subjects = append(subjects, s)
	}
	if len(subjects) == 0 {
		return 0, nil
	}
	var queries *q.Queries
	if tx != nil {
		queries = q.New(tx)
	} else {
		queries = q.New(db.From(ctx))
	}
	subs, err := queries.ListPushRecipients(ctx, q.ListPushRecipientsParams{WorkspaceID: workspaceID, Subjects: subjects})
	if err != nil || len(subs) == 0 {
		return 0, err
	}
	rows, err := queries.ListNotificationPreferences(ctx, q.ListNotificationPreferencesParams{WorkspaceID: workspaceID, Subjects: subjects})
	if err != nil {
		return 0, err
	}
	prefs := map[string]q.NotificationPreference{}
	for _, p := range rows {
		prefs[p.Subject] = p
	}
	devices := map[string][]q.PushSubscription{}
	order := []string{}
	for _, s := range subs {
		if devices[s.Subject] == nil {
			order = append(order, s.Subject)
		}
		devices[s.Subject] = append(devices[s.Subject], s)
	}
	ttl := e.TTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	urgency := string(webpush.UrgencyNormal)
	if e.Kind == KindChat || e.Kind == KindMeet || e.Kind == KindCalendar {
		urgency = string(webpush.UrgencyHigh)
	}
	queued := 0
	for _, subject := range order {
		p, ok := prefs[subject]
		if !ok {
			p = defaults()
		}
		reason := declined(p, e)
		switch {
		case subject == e.Actor:
			reason = SkipOwn
		case skip != "" && reason != SkipOff:
			reason = skip
		}
		if reason != "" {
			logEntry(ctx, queries, workspaceID, subject, e, StatusSkipped, reason, 0)
			continue
		}
		logID := logEntry(ctx, queries, workspaceID, subject, e, StatusQueued, "", len(devices[subject]))
		body, err := encode(e, workspaceID, logID)
		if err != nil {
			return queued, err
		}
		for _, s := range devices[subject] {
			job := schema.PushJob{SubscriptionID: s.ID, Kind: e.Kind, Payload: string(body), Ttl: int(ttl / time.Second), Urgency: urgency, Delivery: uuid.NewString(), Log: logID}
			if tx != nil {
				_, err = jobs.From(ctx).EnqueueTx(ctx, tx, SendJob, job, jobs.MaxAttempts(5))
			} else {
				_, err = jobs.From(ctx).Enqueue(ctx, SendJob, job, jobs.MaxAttempts(5))
			}
			if err != nil {
				return queued, err
			}
			queued++
		}
	}
	return queued, nil
}

// Safe queues an event outside the change that caused it; a failure to
// queue is logged, never returned, so a notification cannot fail the action.
func Safe(ctx context.Context, workspaceID string, recipients []string, e Event) {
	if _, err := Notify(ctx, nil, workspaceID, recipients, e); err != nil {
		lidza.Log(ctx).Warn("push notification not queued", "error", err, "kind", e.Kind, "workspace", workspaceID)
	}
}

// pushHosts are the push services of the browsers Thura supports: Chrome,
// Edge, Opera and Samsung Internet (FCM), Firefox (Mozilla autopush),
// Safari (Apple) and legacy Edge (WNS). An endpoint elsewhere is refused, so
// a subscription cannot make the server send requests to arbitrary hosts.
func pushHosts() []string {
	return []string{"fcm.googleapis.com", "android.googleapis.com", "push.services.mozilla.com", "push.apple.com", "notify.windows.com"}
}

func endpointPolicy(u *url.URL) error {
	// The browser suite (LIDZA_MODE=test) runs its own push service on the
	// loopback interface.
	if os.Getenv("LIDZA_MODE") == "test" && u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost") {
		return nil
	}
	if u.Scheme != "https" || u.User != nil || u.Port() != "" && u.Port() != "443" {
		return errors.New("push endpoint must be an https URL of a browser push service")
	}
	host := strings.ToLower(u.Hostname())
	for _, h := range pushHosts() {
		if host == h || strings.HasSuffix(host, "."+h) {
			return nil
		}
	}
	return errors.New("push endpoint is not a known browser push service")
}

// CheckEndpoint validates a subscription endpoint.
func CheckEndpoint(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return errors.New("invalid push endpoint")
	}
	return endpointPolicy(u)
}

// publicOnly refuses connections to loopback, private and link-local
// addresses outside test mode, so DNS cannot point a known push host at the
// server's own network.
func publicOnly(_, address string, _ syscall.RawConn) error {
	if os.Getenv("LIDZA_MODE") == "test" {
		return nil
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return errors.New("push endpoint resolves to a non-public address")
	}
	return nil
}

var client = &http.Client{
	Timeout: 15 * time.Second,
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, Control: publicOnly}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		MaxIdleConnsPerHost:   4,
	},
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// Send is the SendJob handler. A subscription the push service no longer
// knows (404, 410) is deleted; a rejected request (other 4xx) is dropped;
// a busy or failing service (429, 5xx, network) is retried by the queue.
// Every attempt is recorded on the subscription and, once final, in the
// member's notification log.
func Send(ctx context.Context, raw json.RawMessage) error {
	var job schema.PushJob
	if err := json.Unmarshal(raw, &job); err != nil {
		lidza.Log(ctx).Error("push notification failed", "error_kind", ErrJob, "error", "malformed job")
		return nil // a malformed job cannot succeed on retry
	}
	queries := q.New(db.From(ctx))
	sub, err := queries.GetPushSubscription(ctx, job.SubscriptionID)
	if errors.Is(err, pgx.ErrNoRows) {
		if job.Log != "" {
			if err = queries.NotificationLogFailed(ctx, q.NotificationLogFailedParams{ID: job.Log, Error: "The device was removed before it could be sent."}); err != nil {
				lidza.Log(ctx).Warn("notification log not updated", "error", err, "kind", job.Kind)
			}
		}
		return nil
	}
	if err != nil {
		return gaveUp(ctx, queries, nil, job, err)
	}
	o, err := deliver(ctx, queries, sub, job)
	if err != nil {
		return gaveUp(ctx, queries, &sub, job, err)
	}
	final := o.retry == nil || finalAttempt(ctx, queries, job.Delivery)
	if o.retry != nil && final {
		o.message = "Gave up after 5 attempts. " + strings.Replace(o.message, "; Thura tries again.", ".", 1)
		o.message = strings.Replace(o.message, "; it tries again.", ".", 1)
	}
	record(ctx, queries, sub, jobInfo{kind: job.Kind, log: job.Log}, o, final)
	return o.retry
}

// gaveUp handles an error outside the push service's answer (the database,
// the server's keys): the queue retries it, and its last attempt is
// recorded as a job failure.
func gaveUp(ctx context.Context, queries *q.Queries, sub *q.PushSubscription, job schema.PushJob, err error) error {
	if !finalAttempt(ctx, queries, job.Delivery) {
		lidza.Log(ctx).Warn("push notification failed", "error_kind", ErrJob, "error", err, "kind", job.Kind, "subscription", job.SubscriptionID)
		return err
	}
	o := outcome{kind: ErrJob, message: "Delivery failed after 5 attempts: the server could not prepare it."}
	if sub != nil {
		record(ctx, queries, *sub, jobInfo{kind: job.Kind, log: job.Log}, o, true)
	} else {
		lidza.Log(ctx).Error("push notification failed", "error_kind", ErrJob, "error", err, "kind", job.Kind, "subscription", job.SubscriptionID)
		if job.Log != "" {
			if logErr := queries.NotificationLogFailed(ctx, q.NotificationLogFailedParams{ID: job.Log, Error: o.message}); logErr != nil {
				lidza.Log(ctx).Warn("notification log not updated", "error", logErr, "kind", job.Kind)
			}
		}
	}
	return err
}

// deliver makes one attempt. Its error is one the server caused (reading
// membership, the keys); the outcome describes everything else.
func deliver(ctx context.Context, queries *q.Queries, sub q.PushSubscription, job schema.PushJob) (outcome, error) {
	member, err := queries.IsWorkspaceMember(ctx, q.IsWorkspaceMemberParams{Scope: sub.WorkspaceID, Subject: sub.Subject})
	if err != nil {
		return outcome{}, err
	}
	if !member {
		return outcome{kind: ErrMember, message: "The device's member is no longer in this workspace, so it was removed.", remove: true}, nil
	}
	if CheckEndpoint(sub.Endpoint) != nil {
		return outcome{kind: ErrPolicy, message: "The device's push address is not a known browser push service, so Thura removed it. Turn notifications on again here.", remove: true}, nil
	}
	if !validKeys(sub.P256dh, sub.Auth) {
		return outcome{kind: ErrEncryption, message: "Thura could not encrypt the notification for this device: its subscription keys are invalid. Turn notifications off and on again on this device."}, nil
	}
	public, private, err := Keys(ctx)
	if err != nil {
		return outcome{}, err
	}
	ttl := job.Ttl
	if ttl <= 0 {
		ttl = 86400
	}
	response, err := webpush.SendNotificationWithContext(ctx, []byte(job.Payload), &webpush.Subscription{Endpoint: sub.Endpoint, Keys: webpush.Keys{P256dh: sub.P256dh, Auth: sub.Auth}}, &webpush.Options{
		HTTPClient: client, Subscriber: subscriber(), TTL: ttl, Urgency: webpush.Urgency(job.Urgency),
		VAPIDPublicKey: public, VAPIDPrivateKey: private,
	})
	if err != nil {
		return sendError(err), nil
	}
	defer response.Body.Close()
	detail := ""
	if response.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		detail = string(raw)
	}
	return statusOutcome(response.StatusCode, detail), nil
}

// validKeys reports whether a subscription's keys can encrypt a payload: a
// P-256 point and a 16-byte secret.
func validKeys(p256dh, secret string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(p256dh, "="))
	if err != nil {
		return false
	}
	if _, err = ecdh.P256().NewPublicKey(raw); err != nil {
		return false
	}
	return keyLength(secret, 16)
}
