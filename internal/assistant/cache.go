package assistant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"runtime/debug"
	"time"

	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/llm"
	"github.com/agim/lidza/pkg/router"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	q "thura/db/queries/gen"
	"thura/internal/workspace"
	"thura/schema"
)

var frameworkCacheVersion = func() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, dependency := range info.Deps {
			if dependency.Path == "github.com/agim/lidza" {
				if dependency.Replace != nil {
					return dependency.Replace.Path + "@" + dependency.Replace.Version
				}
				return dependency.Version
			}
		}
	}
	return "unknown"
}()

// Hash the complete request and effective model configuration. Neither source
// text, instructions nor provider credentials are persisted as cache metadata.
// A framework/prompt/config/content change automatically selects a fresh entry.
func responseKey(config llm.Config, provider, model string, request llm.Request, contextVersion string) (string, error) {
	encoded, err := json.Marshal(struct {
		Version         string
		ContextVersion  string
		Config          llm.Config
		Provider, Model string
		Request         llm.Request
	}{"thura-ai-cache-v1:" + frameworkCacheVersion, contextVersion, config, provider, model, request})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func cachedResponse(ctx context.Context, workspaceID, key string, generate func() (schema.AssistantResult, error)) (schema.AssistantResult, error) {
	queries := q.New(db.From(ctx))
	token := uuid.NewString()
	for {
		if err := workspace.RequireMember(ctx, workspaceID); err != nil {
			return schema.AssistantResult{}, err
		}
		saved, err := queries.GetAssistantCache(ctx, q.GetAssistantCacheParams{WorkspaceID: workspaceID, CacheKey: key})
		if err == nil {
			return schema.AssistantResult{Text: saved.Text, Model: saved.Model, Cached: true}, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return schema.AssistantResult{}, err
		}
		_, err = queries.ClaimAssistantCache(ctx, q.ClaimAssistantCacheParams{WorkspaceID: workspaceID, CacheKey: key, LeaseToken: token})
		if err == nil {
			break
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return schema.AssistantResult{}, err
		}
		// Wait without holding a transaction or pool connection during generation.
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return schema.AssistantResult{}, router.Errorf(503, "This response is still being prepared. Please try again")
		case <-timer.C:
		}
	}
	// Only this token can finalize or remove its lease. A crashed process leaves
	// an expiring lease rather than a permanently locked file summary.
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		_ = queries.ReleaseAssistantCache(cleanup, q.ReleaseAssistantCacheParams{WorkspaceID: workspaceID, CacheKey: key, LeaseToken: token})
	}()
	result, err := generate()
	if err != nil {
		return schema.AssistantResult{}, err
	}
	if err = workspace.RequireMember(ctx, workspaceID); err != nil {
		return schema.AssistantResult{}, err
	}
	if err = ctx.Err(); err != nil {
		return schema.AssistantResult{}, err
	}
	// Failed, empty, cancelled and incomplete responses never become shared hits.
	if result.Truncated {
		return result, nil
	}
	rows, err := queries.StoreAssistantCache(ctx, q.StoreAssistantCacheParams{WorkspaceID: workspaceID, CacheKey: key, Text: result.Text, Model: result.Model, LeaseToken: token})
	if err != nil {
		return schema.AssistantResult{}, err
	}
	if rows != 1 {
		return schema.AssistantResult{}, router.Errorf(503, "The response expired before it could be saved. Please try again")
	}
	if err = queries.PruneAssistantCache(ctx, workspaceID); err != nil {
		return schema.AssistantResult{}, err
	}
	return result, nil
}
