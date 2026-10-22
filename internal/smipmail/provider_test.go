package smipmail

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/agim/lidza/packs/mail"
)

func TestProviderPostRedirectCannotReplaySubmission(t *testing.T) {
	var sourceCalls, targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls.Add(1); w.WriteHeader(http.StatusAccepted) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sourceCalls.Add(1)
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	transport, err := mail.New(mail.Config{Provider: "mailgun", Domain: "sender.example", APIKey: "PUBLIC-fixture-key", BaseURL: source.URL}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = transport.Send(ProviderContext(context.Background()), mail.Message{From: "team@sender.example", To: "team@recipient.example", Subject: "No redirect replay", Text: "One original submission"})
	if err == nil || sourceCalls.Load() != 1 || targetCalls.Load() != 0 {
		t.Fatal("provider replayed a redirected submission", err, sourceCalls.Load(), targetCalls.Load())
	}
}
