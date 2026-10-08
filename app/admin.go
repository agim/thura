package app

import (
	"embed"
	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/admin"
	"github.com/agim/lidza/pkg/router"
	"thura/internal/setup"
)

//go:embed admin
var adminFiles embed.FS

func mountAdmin(r *router.Router) {
	admin.Mount(r, admin.Options{Title: "Thura administration", Allow: setup.Allow, NoFirstUserAdmin: true, Templates: lidza.Sub(adminFiles, "admin"), Pages: []admin.Page{setup.Page()}})
}
