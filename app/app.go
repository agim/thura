package app

import (
	"context"
	"github.com/agim/lidza"
	"github.com/agim/lidza/pkg/middleware"
	"io/fs"
	"os"
	"thura/internal/platform/ingress"
)

// New constructs the application for the executable and integration tests.
func New(dist fs.FS) lidza.App {
	configuration, configurationErr := ingress.LoadDeployment(os.Getenv)
	chain := appMiddleware()
	if configurationErr == nil && configuration != nil {
		protect, err := ingress.Protect(configuration.Policy)
		if err != nil {
			configurationErr = err
		} else {
			chain = append([]middleware.Middleware{protect}, chain...)
		}
	}
	application := lidza.App{
		Name:   "thura",
		Dist:   dist,
		Routes: routes,
		Packs:  packs(),
		Tools:  tools(),
		OnStart: func(ctx context.Context, services *lidza.Services) error {
			if configurationErr != nil {
				return configurationErr
			}
			return onStart(ctx, services)
		},
		Middleware: chain,
		Head:       head,
	}
	if configurationErr == nil && configuration != nil {
		application.PrivateTLS = configuration.TLS
		application.PrivateTLSAddr = configuration.Address
	}
	return application
}
