package smipmail

import (
	"context"
	"errors"
	"net/http"

	"github.com/agim/lidza"
)

// The mail pack owns provider serialization. Reject redirects in its transport
// before net/http can replay a submitted POST to another endpoint.
type noRedirect struct{ base http.RoundTripper }

func (t noRedirect) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(r)
	if err == nil && response.StatusCode >= 300 && response.StatusCode < 400 {
		response.Body.Close()
		return nil, errors.New("mail provider redirect refused; submission outcome requires reconciliation")
	}
	return response, err
}

func ProviderContext(ctx context.Context) context.Context {
	base := lidza.HTTPClient(ctx).Transport
	if base == nil {
		base = http.DefaultTransport
	}
	services := lidza.NewServices()
	lidza.Provide[http.RoundTripper](services, noRedirect{base})
	return lidza.WithServices(ctx, services)
}
