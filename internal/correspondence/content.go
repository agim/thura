package correspondence

import (
	"context"
	"strings"
	"thura/internal/chat"
	"thura/internal/drive"
	"time"
)

// contentMatch uses live sources, never a workspace-wide copy of private text.
func contentMatch(ctx context.Context, w, id, source, term string) (string, bool, string) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if source == "chat" {
		result, err := chat.Search(ctx, w, id, term, "")
		if err != nil {
			return "", false, "Some chat messages could not be searched. Open Chat or refresh to retry."
		}
		if len(result.Items) > 0 {
			return "Message: " + Excerpt(result.Items[0].Body, term), true, ""
		}
		if result.Next != nil {
			return "", false, "Some chat search results require another page. Open the conversation search to continue."
		}
		return "", false, ""
	}
	text, err := drive.SearchText(ctx, w, id)
	if err != nil {
		return "", false, "Some documents could not be searched (unsupported format, extraction limit, changed version, or unavailable content). Refresh to retry."
	}
	if strings.Contains(strings.ToLower(text), strings.ToLower(term)) {
		return "Document text: " + Excerpt(text, term), true, ""
	}
	return "", false, ""
}

// Excerpt centers a plain-text match without inserting HTML or splitting UTF-8.
func Excerpt(text, term string) string {
	text = strings.Join(strings.Fields(text), " ")
	lower := strings.ToLower(text)
	index := strings.Index(lower, strings.ToLower(strings.Join(strings.Fields(term), " ")))
	runes := []rune(text)
	start := 0
	if index >= 0 {
		start = max(0, len([]rune(lower[:index]))-70)
	}
	end := min(len(runes), start+220)
	result := string(runes[start:end])
	if start > 0 {
		result = "…" + result
	}
	if end < len(runes) {
		result += "…"
	}
	return result
}
