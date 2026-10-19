package tests

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"errors"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/llm"
	"github.com/agim/lidza/pkg/router"
	"thura/app"
	q "thura/db/queries/gen"
	"thura/internal/assistant"
	"thura/internal/workspace"
	"thura/schema"
)

func TestAssistantCacheSharesExactRequestsAndInvalidatesChangedText(t *testing.T) {
	t.Setenv("THURA_AI_ENABLED", "true")
	srv := startConfigured(t, app.New(nil))
	ctx := srv.Context()
	accounts := auth.From(ctx)
	owner, err := accounts.CreateUser(ctx, fmt.Sprintf("cache-owner-%d@example.com", time.Now().UnixNano()), "Owner", "forest maple waterfall lantern 7143")
	if err != nil {
		t.Fatal(err)
	}
	member, err := accounts.CreateUser(ctx, fmt.Sprintf("cache-member-%d@example.com", time.Now().UnixNano()), "Member", "forest maple waterfall lantern 7143")
	if err != nil {
		t.Fatal(err)
	}
	first, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Shared AI", OwnerEmail: owner.Email})
	if err != nil {
		t.Fatal(err)
	}
	second, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Other AI", OwnerEmail: owner.Email})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, w := range []string{first.ID, second.ID} {
			for _, sql := range []string{"DELETE FROM assistant_cache WHERE workspace_id=$1", "DELETE FROM auth_member WHERE scope=$1", "DELETE FROM workspace WHERE id=$1"} {
				if _, err := db.From(ctx).Exec(ctx, sql, w); err != nil {
					t.Error(err)
				}
			}
		}
		for _, subject := range []string{owner.Subject, member.Subject} {
			if err := accounts.DeleteUser(ctx, subject); err != nil {
				t.Error(err)
			}
		}
	})
	if err := q.New(db.From(ctx)).GrantWorkspaceMember(ctx, q.GrantWorkspaceMemberParams{Scope: first.ID, Subject: member.Subject, Role: "member"}); err != nil {
		t.Fatal(err)
	}
	request := func(w, subject string, input schema.AssistantInput, status int) schema.AssistantResult {
		t.Helper()
		out, err := assistant.Generate(auth.WithUser(ctx, &auth.User{ID: subject}), w, input)
		actual := 200
		if err != nil {
			var failure *router.HTTPError
			if !errors.As(err, &failure) {
				t.Fatal(err)
			}
			actual = failure.Status
		}
		if actual != status {
			t.Fatalf("assist: got %d, want %d", actual, status)
		}
		return out
	}
	fake := llm.From(ctx).Fake()
	input := schema.AssistantInput{Source: "drive", Action: "summarize", Content: "Version one of a shared file"}
	fake.Reply("Shared file summary")
	original := request(first.ID, owner.Subject, input, 200)
	shared := request(first.ID, member.Subject, input, 200)
	if original.Cached || !shared.Cached || shared.Text != original.Text || shared.Model != original.Model || len(fake.Calls()) != 1 {
		t.Fatalf("not shared between members: %+v %+v calls=%d", original, shared, len(fake.Calls()))
	}
	// A separate workspace cannot reuse even an otherwise identical request.
	separate := request(second.ID, owner.Subject, input, 200)
	if separate.Cached || len(fake.Calls()) != 2 {
		t.Fatal("cache crossed workspace boundary")
	}
	request(second.ID, member.Subject, input, 404)
	if len(fake.Calls()) != 2 {
		t.Fatal("unauthorized cache access reached provider")
	}
	// Changed text, instructions, action and resource each produce a new key.
	variants := []schema.AssistantInput{input, input, input, input, input}
	variants[0].Content = "Version two of the shared file"
	instruction := "Focus on decisions"
	variants[1].Instructions = &instruction
	variants[2].Action = "next-steps"
	variants[3].Source = "mail"
	revision := "file-version-2"
	variants[4].ContextVersion = &revision
	for _, variant := range variants {
		before := len(fake.Calls())
		if out := request(first.ID, member.Subject, variant, 200); out.Cached || len(fake.Calls()) != before+1 {
			t.Fatal("changed request reused stale result")
		}
		if out := request(first.ID, owner.Subject, variant, 200); !out.Cached || len(fake.Calls()) != before+1 {
			t.Fatal("new result was not retained")
		}
	}
	// Equal concurrent requests use the same database lock, across members.
	concurrent := input
	concurrent.Content = "Concurrent file revision"
	before := len(fake.Calls())
	type outcome struct {
		status int
		result schema.AssistantResult
		err    error
	}
	results := make(chan outcome, 8)
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			subject := owner.Subject
			if i%2 == 0 {
				subject = member.Subject
			}
			out, err := assistant.Generate(auth.WithUser(ctx, &auth.User{ID: subject}), first.ID, concurrent)
			status := 200
			if err != nil {
				status = 500
			}
			results <- outcome{status, out, err}
		}(i)
	}
	group.Wait()
	close(results)
	hits := 0
	for result := range results {
		if result.err != nil || result.status != 200 {
			t.Fatalf("concurrent assist: %+v", result)
		}
		if result.result.Cached {
			hits++
		}
	}
	if len(fake.Calls()) != before+1 || hits != 7 {
		t.Fatalf("duplicate generation: calls=%d hits=%d", len(fake.Calls())-before, hits)
	}
	// Expired entries regenerate; empty results never become reusable entries.
	if _, err := db.From(ctx).Exec(ctx, "UPDATE assistant_cache SET expires_at=now()-interval '1 second' WHERE workspace_id=$1", first.ID); err != nil {
		t.Fatal(err)
	}
	if out := request(first.ID, owner.Subject, input, 200); out.Cached {
		t.Fatal("expired response reused")
	}
	failed := input
	failed.Content = "File with temporarily failing generation"
	fake.Reply("")
	request(first.ID, owner.Subject, failed, 502)
	fake.Reply("Recovered summary")
	if out := request(first.ID, member.Subject, failed, 200); out.Cached || out.Text != "Recovered summary" {
		t.Fatal("failed response cached")
	}
	if out := request(first.ID, owner.Subject, failed, 200); !out.Cached {
		t.Fatal("successful retry not cached")
	}
	t.Setenv("THURA_AI_ENABLED", "false")
	request(first.ID, member.Subject, failed, 503)
	t.Setenv("THURA_AI_ENABLED", "true")
	if _, err := db.From(ctx).Exec(ctx, "DELETE FROM auth_member WHERE scope=$1 AND subject=$2", first.ID, member.Subject); err != nil {
		t.Fatal(err)
	}
	request(first.ID, member.Subject, failed, 404)
}
