package push

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/agim/lidza"
	q "thura/db/queries/gen"
)

// Delivery diagnostics: each subscription keeps its last attempt, its last
// delivery and its last failure in plain words; each member keeps a short
// log of the notification decisions that reached them (NotificationLog), so
// Settings → Notifications can answer "why didn't I get one". Neither keeps
// a title, a preview, an endpoint or a key.

// Log statuses.
const (
	StatusQueued    = "queued"
	StatusDelivered = "delivered"
	StatusFailed    = "failed"
	StatusSkipped   = "skipped"
)

// Skip reasons, recorded where Notify, Wants and MailArrived decide.
const (
	SkipPeopleOnly = "people_only" // mail not from a real person, "Only mail from real people" chosen
	SkipOpen       = "open"        // a focused tab showed the view (reported by the service worker)
	SkipSpam       = "spam"        // mail went to Spam or was held
	SkipFiltered   = "filtered"    // a mail filter took it out of the Inbox
	SkipOwn        = "own"         // the member's own action
	SkipOff        = "off"         // the member turned this kind off
)

// Error kinds of a failed delivery.
const (
	ErrStatus     = "status"     // the push service answered with a refusal or an error
	ErrNetwork    = "network"    // the push service could not be reached
	ErrVAPID      = "vapid"      // the server could not sign the request
	ErrEncryption = "encryption" // the payload could not be encrypted for the device's keys
	ErrPolicy     = "policy"     // the endpoint is not an allowed push service
	ErrMember     = "member"     // the device's member left the workspace
	ErrJob        = "job"        // the delivery job gave up
)

// logLimit bounds each member's notification log; recentLimit is what
// Settings shows.
const (
	logLimit    = 50
	recentLimit = 10
)

// outcome is the result of one delivery attempt.
type outcome struct {
	delivered bool
	status    int
	kind      string // error kind; "" when delivered
	message   string // plain words for Settings
	remove    bool   // the subscription is gone for good
	retry     error  // non-nil: the queue should try again
}

func (o outcome) failed() bool { return !o.delivered }

// statusOutcome turns a push service's answer into an outcome. 404 and 410
// mean the browser unsubscribed; 429 and 5xx are retried; any other
// refusal is final.
func statusOutcome(code int, detail string) outcome {
	reason := fmt.Sprintf("%d %s", code, http.StatusText(code))
	if strings.TrimSpace(http.StatusText(code)) == "" {
		reason = fmt.Sprintf("%d", code)
	}
	o := outcome{status: code, kind: ErrStatus}
	switch {
	case code >= 200 && code < 300:
		return outcome{delivered: true, status: code}
	case code == http.StatusNotFound || code == http.StatusGone:
		o.message, o.remove = "The push service refused it ("+reason+"): this device unsubscribed. Turn notifications on again here.", true
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		o.message = "The push service refused it (" + reason + "): it does not accept this server's key. Turn notifications off and on again on this device."
	case code == http.StatusRequestEntityTooLarge:
		o.message = "The push service refused it (" + reason + "): the notification was too large."
	case code == http.StatusTooManyRequests:
		o.message, o.retry = "The push service is busy ("+reason+"); Thura tries again.", fmt.Errorf("push service answered %d", code)
	case code >= 500:
		o.message, o.retry = "The push service had a problem ("+reason+"); Thura tries again.", fmt.Errorf("push service answered %d", code)
	default:
		o.message = "The push service refused it (" + reason + ")."
	}
	if detail = cleanDetail(detail); detail != "" && !o.remove {
		o.message = clip(strings.TrimSuffix(o.message, ".")+": "+detail, 300)
	}
	return o
}

// cleanDetail is the push service's own short explanation, without
// anything that could be an address.
func cleanDetail(s string) string {
	s = clip(s, 120)
	if s == "" || strings.Contains(s, "://") || strings.ContainsAny(s, "<>{}") {
		return ""
	}
	return s
}

// sendError classifies an error from building or sending the request: a
// network failure is retried; a signing or encryption failure is final.
func sendError(err error) outcome {
	var u *url.Error
	if errors.As(err, &u) || errors.Is(err, context.DeadlineExceeded) {
		return outcome{kind: ErrNetwork, message: "Thura could not reach the push service (network error); it tries again.", retry: err}
	}
	return outcome{kind: ErrVAPID, message: "Thura could not sign the notification (VAPID key problem). The server administrator should check the master key."}
}

// serviceName is the push service an endpoint belongs to, for the test result.
func serviceName(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "the push service"
	}
	host := strings.ToLower(u.Hostname())
	switch {
	case host == "fcm.googleapis.com" || host == "android.googleapis.com":
		return "Google's push service"
	case strings.HasSuffix(host, "push.services.mozilla.com"):
		return "Mozilla's push service"
	case strings.HasSuffix(host, "push.apple.com"):
		return "Apple's push service"
	case strings.HasSuffix(host, "notify.windows.com"):
		return "Microsoft's push service"
	case host == "127.0.0.1" || host == "localhost":
		return "the local test push service"
	}
	return "the push service"
}

// logEntry records one decision for a member and returns its id ("" when
// the log could not be written: diagnostics never fail a notification).
func logEntry(ctx context.Context, queries *q.Queries, w, subject string, e Event, status, reason string, devices int) string {
	id, err := queries.AddNotificationLog(ctx, q.AddNotificationLogParams{WorkspaceID: w, Subject: subject, Kind: e.Kind, Link: safeLink(e.URL), Status: status, Reason: reason, Devices: int32(devices)})
	if err != nil {
		lidza.Log(ctx).Warn("notification log not written", "error", err, "kind", e.Kind, "workspace", w)
		return ""
	}
	if err = queries.PruneNotificationLog(ctx, q.PruneNotificationLogParams{WorkspaceID: w, Subject: subject}); err != nil {
		lidza.Log(ctx).Warn("notification log not pruned", "error", err, "workspace", w)
	}
	return id
}

// safeLink is the item link Encode would send.
func safeLink(link string) string {
	if !strings.HasPrefix(link, "/app/") || len(link) > maxURL || strings.ContainsAny(link, "\r\n\\") {
		return "/app/mail"
	}
	return link
}

// record stores an attempt's outcome on the subscription and the log; final
// is false for an attempt the queue retries. A subscription that is gone
// is removed with its reason kept for Settings.
func record(ctx context.Context, queries *q.Queries, sub q.PushSubscription, job jobInfo, o outcome, final bool) {
	now := lidza.Now(ctx)
	if !o.remove {
		if err := queries.RecordPushAttempt(ctx, q.RecordPushAttemptParams{At: &now, Kind: job.kind, Delivered: o.delivered, ErrorKind: o.kind, Error: clip(o.message, 300), FinalFailure: o.failed() && final, ID: sub.ID}); err != nil {
			lidza.Log(ctx).Warn("push delivery status not recorded", "error", err, "kind", job.kind, "subscription", sub.ID)
		}
	}
	if job.log != "" && (o.delivered || final) {
		var err error
		if o.delivered {
			err = queries.NotificationLogDelivered(ctx, job.log)
		} else {
			err = queries.NotificationLogFailed(ctx, q.NotificationLogFailedParams{ID: job.log, Error: clip(o.message, 300)})
		}
		if err != nil {
			lidza.Log(ctx).Warn("notification log not updated", "error", err, "kind", job.kind, "subscription", sub.ID)
		}
	}
	if o.failed() {
		level := lidza.Log(ctx).Warn
		if final {
			level = lidza.Log(ctx).Error
		}
		level("push notification failed", "error_kind", o.kind, "status", o.status, "final", final, "kind", job.kind, "subscription", sub.ID)
	}
	if o.remove {
		removeDevice(ctx, queries, sub, o)
	}
}

// removeDevice deletes a subscription and keeps why, for Settings.
func removeDevice(ctx context.Context, queries *q.Queries, sub q.PushSubscription, o outcome) {
	if err := queries.DeletePushSubscription(ctx, sub.ID); err != nil {
		lidza.Log(ctx).Warn("push subscription not removed", "error", err, "subscription", sub.ID)
		return
	}
	if err := queries.AddPushRemovedDevice(ctx, q.AddPushRemovedDeviceParams{WorkspaceID: sub.WorkspaceID, Subject: sub.Subject, EndpointHash: sub.EndpointHash, Label: sub.Label, ErrorKind: o.kind, Error: clip(o.message, 300)}); err != nil {
		lidza.Log(ctx).Warn("removed push device not recorded", "error", err, "subscription", sub.ID)
		return
	}
	if err := queries.PrunePushRemovedDevices(ctx, q.PrunePushRemovedDevicesParams{WorkspaceID: sub.WorkspaceID, Subject: sub.Subject}); err != nil {
		lidza.Log(ctx).Warn("removed push devices not pruned", "error", err, "subscription", sub.ID)
	}
}

// jobInfo is what record needs of a delivery.
type jobInfo struct{ kind, log string }

// finalAttempt reports whether the running delivery job will not be retried.
func finalAttempt(ctx context.Context, queries *q.Queries, delivery string) bool {
	if delivery == "" {
		return false
	}
	final, err := queries.PushJobFinalAttempt(ctx, q.PushJobFinalAttemptParams{Kind: SendJob, Delivery: delivery})
	if err != nil {
		lidza.Log(ctx).Warn("push job attempt not read", "error", err)
		return false
	}
	return final
}
