package app

import (
	"github.com/agim/lidza"
	"io/fs"
)

// New constructs the application for the executable and integration tests.
func New(dist fs.FS) lidza.App {
	return lidza.App{
		Name:       "thura",
		Dist:       dist,
		Routes:     routes,
		Packs:      packs(),
		Tools:      tools(),
		OnStart:    onStart,
		Middleware: appMiddleware(),
		Head:       head,
	}
}
