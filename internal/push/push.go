package push

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	link := e.URL
	if !strings.HasPrefix(link, "/app/") || len(link) > maxURL || strings.ContainsAny(link, "\r\n\\") {
		link = "/app/mail"
	}
	title := clip(e.Title, maxTitle)
	if title == "" {
		title = "Thura"
	}
	return json.Marshal(payload{Kind: e.Kind, Title: title, Body: clip(e.Body, maxBody), URL: link, Tag: clip(e.Tag, 64), View: e.View})
}

func defaults() q.NotificationPreference {
	return q.NotificationPreference{Mail: true, MailPeopleOnly: true, Chat: true, ChatRequests: true, Calendar: true, Meet: true}
}

// Wants reports whether a member's preferences let an event notify them.
func Wants(p q.NotificationPreference, e Event) bool {
	switch e.Kind {
	case KindMail:
		return p.Mail && (!p.MailPeopleOnly || e.Person)
	case KindChat:
		return p.Chat
	case KindChatRequest:
		return p.ChatRequests
	case KindCalendar:
		return p.Calendar
	case KindMeet:
		return p.Meet
	case KindTest:
		return true
	}
	return false
}

// Notify queues the event for each recipient's devices, within tx when one
// is given (so it commits with the change that caused it). It skips the
// actor, members who left, members whose preferences decline the event and
// members without a device; it never fails because nobody is subscribed.
// It returns the number of deliveries queued.
func Notify(ctx context.Context, tx pgx.Tx, workspaceID string, recipients []string, e Event) (int, error) {
	seen := map[string]bool{}
	subjects := []string{}
	for _, s := range recipients {
		if s == "" || s == e.Actor || seen[s] {
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
	body, err := Encode(e)
	if err != nil {
		return 0, err
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
	for _, s := range subs {
		p, ok := prefs[s.Subject]
		if !ok {
			p = defaults()
		}
		if !Wants(p, e) {
			continue
		}
		job := schema.PushJob{SubscriptionID: s.ID, Kind: e.Kind, Payload: string(body), Ttl: int(ttl / time.Second), Urgency: urgency}
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
func Send(ctx context.Context, raw json.RawMessage) error {
	var job schema.PushJob
	if err := json.Unmarshal(raw, &job); err != nil {
		return nil // a malformed job cannot succeed on retry
	}
	queries := q.New(db.From(ctx))
	sub, err := queries.GetPushSubscription(ctx, job.SubscriptionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	member, err := queries.IsWorkspaceMember(ctx, q.IsWorkspaceMemberParams{Scope: sub.WorkspaceID, Subject: sub.Subject})
	if err != nil {
		return err
	}
	if !member {
		return queries.DeletePushSubscription(ctx, sub.ID)
	}
	if CheckEndpoint(sub.Endpoint) != nil {
		return queries.DeletePushSubscription(ctx, sub.ID)
	}
	public, private, err := Keys(ctx)
	if err != nil {
		return err
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
		return err
	}
	defer response.Body.Close()
	switch {
	case response.StatusCode >= 200 && response.StatusCode < 300:
		return queries.TouchPushSubscription(ctx, q.TouchPushSubscriptionParams{ID: sub.ID, LastUsedAt: ptr(lidza.Now(ctx))})
	case response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone:
		return queries.DeletePushSubscription(ctx, sub.ID)
	case response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500:
		return fmt.Errorf("push service answered %d", response.StatusCode)
	default:
		lidza.Log(ctx).Warn("push notification refused", "status", response.StatusCode, "kind", job.Kind, "subscription", sub.ID)
		return nil
	}
}

func ptr[T any](v T) *T { return &v }
