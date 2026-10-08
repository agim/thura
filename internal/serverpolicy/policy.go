package serverpolicy

import (
	"github.com/agim/lidza/pkg/env"
	"github.com/agim/lidza/pkg/router"
)

type Config struct {
	MaxMembers   int  `env:"THURA_MAX_MEMBERS" default:"1000"`
	AdminInvites bool `env:"THURA_ADMIN_INVITES" default:"true"`
	Shares       bool `env:"THURA_ALLOW_SHARES" default:"true"`
	Anonymous    bool `env:"THURA_ALLOW_ANONYMOUS_SHARES" default:"true"`
}

func Load() (Config, error) {
	var c Config
	e := env.Load(".", &c)
	if e == nil && (c.MaxMembers < 1 || c.MaxMembers > 1000) {
		e = router.Errorf(503, "invalid server member limit")
	}
	return c, e
}
func Share(anonymous bool) error {
	c, e := Load()
	if e != nil {
		return e
	}
	if !c.Shares || (anonymous && !c.Anonymous) {
		return router.Errorf(403, "external sharing is restricted by the server administrator")
	}
	return nil
}
