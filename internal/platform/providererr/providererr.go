// Package providererr is the one place that words a provider failure for the
// people using the app. A provider (Matrix, LiveKit, Collabora, the model
// service) explains its failures in operator terms: setting names, vendor
// names, status codes. Those go to the log; the person using the app gets
// one plain sentence and a 503. Server administrators find the detail in the
// log and on the setup pages.
package providererr

import (
	"context"
	"errors"
	"net/http"

	"github.com/agim/lidza"
	"github.com/agim/lidza/pkg/router"
	"github.com/agim/lidza/pkg/validate"
)

// The member-facing sentences, one per service.
const (
	Chat      = "Chat isn't available right now. Try again later."
	Meet      = "Meetings aren't available right now. Try again later."
	Editor    = "The document editor is unavailable right now."
	Assistant = "The assistant couldn't respond. Try again later."
)

// Hide returns err unchanged when it is the app's own answer to the request
// (a 4xx: not found, forbidden, a conflict, a validation error) and otherwise
// logs it and returns a 503 carrying message. A nil err stays nil.
func Hide(ctx context.Context, service, message string, err error) error {
	if err == nil {
		return nil
	}
	var httpErr *router.HTTPError
	if errors.As(err, &httpErr) && httpErr.Status < 500 {
		return err
	}
	var invalid *validate.Errors
	var tooLarge *http.MaxBytesError
	if errors.As(err, &invalid) || errors.As(err, &tooLarge) || errors.Is(err, context.Canceled) {
		return err
	}
	lidza.Log(ctx).Warn("provider failure", "service", service, "error", err)
	return router.Errorf(http.StatusServiceUnavailable, "%s", message)
}
