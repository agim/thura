package tests

import (
	"github.com/agim/lidza"
	"github.com/agim/lidza/pkg/lidzatest"
	"testing"
	"thura/tests/testsupport"
)

func startConfigured(t *testing.T, application lidza.App) *lidzatest.Server {
	t.Helper()
	return testsupport.StartConfigured(t, application)
}
