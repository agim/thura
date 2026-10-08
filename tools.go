package main

import (
	"context"
	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/auth"
	"strings"
	"thura/internal/mailbox"
	"thura/internal/workspace"
	"thura/schema"
)

// tools lists the app's MCP tools: functions an agent can call through
// `lidza mcp` (as app_<name>) and, with LIDZA_MCP_TOKEN set, through the
// running binary at /mcp. They run inside the app with its packs, so a
// tool can query the database or enqueue a job. Mark a lookup
// ReadOnly (the agent's client runs it without asking) and one that
// deletes or overwrites Destructive. Example:
//
//	lidza.ToolFunc("count_posts", "Number of posts.", func(ctx context.Context, _ struct{}) (int, error) {
//		var n int
//		err := db.From(ctx).QueryRow(ctx, "SELECT count(*) FROM post").Scan(&n)
//		return n, err
//	}),
func tools() []lidza.Tool {
	return []lidza.Tool{
		lidza.ToolFunc("provision_mailbox", "Operator only: assign a sending address and credential prefix to a workspace mailbox.", mailbox.Provision),
		lidza.ToolFunc("provision_account", "Operator only: provision an invited local account. Password input must not be saved in source or logs.", func(ctx context.Context, in schema.ProvisionAccountInput) (auth.Profile, error) {
			return auth.From(ctx).CreateUser(ctx, strings.ToLower(strings.TrimSpace(in.Email)), strings.TrimSpace(in.Name), in.Password)
		}),
		lidza.ToolFunc("create_workspace", "Operator only: create a workspace and grant ownership to an already provisioned account.", workspace.Create),
	}
}
