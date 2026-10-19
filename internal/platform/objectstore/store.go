// Package objectstore retains Thura's fail-closed local-storage behavior when
// the framework pins an os.Root handle across directory moves.
package objectstore

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/storage"
	"github.com/agim/lidza/pkg/env"
)

type rootIdentity struct {
	path string
	info os.FileInfo
}

func Configure(ctx context.Context, services *lidza.Services) error {
	identity := &rootIdentity{}
	if storage.From(ctx).Provider() == "local" {
		var config storage.Config
		if err := env.Load(".", &config); err != nil {
			return err
		}
		path, err := filepath.Abs(config.Dir)
		if err != nil {
			return err
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("local storage directory is unavailable")
		}
		identity.path, identity.info = path, info
	}
	lidza.Provide(services, identity)
	return nil
}

func Put(ctx context.Context, key string, reader io.Reader, options storage.PutOptions) (storage.Object, error) {
	if identity, ok := lidza.Optional[*rootIdentity](ctx); ok && identity.path != "" && storage.From(ctx).Provider() == "local" {
		current, err := os.Stat(identity.path)
		if err != nil || !current.IsDir() || !os.SameFile(current, identity.info) {
			return storage.Object{}, fmt.Errorf("local storage directory is unavailable or changed; restore it before retrying")
		}
	}
	return storage.From(ctx).Put(ctx, key, reader, options)
}
