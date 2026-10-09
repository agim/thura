package tests

import (
	"context"
	"testing"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/lidzatest"
	q "thura/db/queries/gen"
)

// Existing feature tests use explicitly configured .env.test providers,
// without introducing real wizard credentials or replacing those providers.
// The publication marker represents that configured fixture installation.
// Fresh-install and publication tests use setupServer instead.
func startConfigured(t *testing.T, application lidza.App) *lidzatest.Server {
	t.Helper()
	start := application.OnStart
	application.OnStart = func(ctx context.Context, services *lidza.Services) error {
		ctx = lidza.WithServices(ctx, services)
		if err := q.New(db.From(ctx)).EnsureServerSetup(ctx); err != nil {
			return err
		}
		if _, err := db.From(ctx).Exec(ctx, "INSERT INTO auth_user(subject,email,name) VALUES('configured-test-operator','configured-operator@fixture.example','Configured test operator') ON CONFLICT(subject) DO NOTHING"); err != nil {
			t.Fatal(err)
			return err
		}
		if _, err := db.From(ctx).Exec(ctx, "UPDATE server_setup SET subject='thura-fixture:externally-configured',published_revision=1 WHERE id='00000000-0000-4000-8000-000000000001'"); err != nil {
			t.Fatal(err)
			return err
		}
		return start(ctx, services)
	}
	return lidzatest.Start(t, application)
}
