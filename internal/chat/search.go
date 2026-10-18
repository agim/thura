package chat

import (
	"context"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/pkg/router"
	"net/url"
	"strings"
	provider "thura/internal/providers/matrix"
	"thura/schema"
	"unicode/utf8"
)

// Search never joins a room or uses the bot's history. Matrix applies history
// visibility for the requesting virtual user, after Thura checks room access.
func Search(ctx context.Context, w, id, term, cursor string) (schema.ChatTimeline, error) {
	out := schema.ChatTimeline{Items: []schema.ChatMessage{}}
	term = strings.TrimSpace(term)
	if term == "" || !utf8.ValidString(term) || utf8.RuneCountInString(term) > 200 || len(cursor) > 2000 {
		return out, router.Errorf(422, "invalid search or cursor")
	}
	room, err := Access(ctx, w, id)
	if err != nil {
		return out, err
	}
	cfg, err := provider.Load()
	if err != nil {
		return out, err
	}
	query := url.Values{}
	if cursor != "" {
		query.Set("next_batch", cursor)
	}
	var response struct {
		Categories struct {
			Events struct {
				Results []struct {
					Result struct {
						ID        string `json:"event_id"`
						RoomID    string `json:"room_id"`
						Type      string `json:"type"`
						Sender    string `json:"sender"`
						Timestamp int64  `json:"origin_server_ts"`
						Content   struct {
							Body string `json:"body"`
						} `json:"content"`
					}
				} `json:"results"`
				Next string `json:"next_batch"`
			} `json:"room_events"`
		} `json:"search_categories"`
	}
	body := map[string]any{"search_categories": map[string]any{"room_events": map[string]any{"search_term": term, "keys": []string{"content.body"}, "order_by": "recent", "filter": map[string]any{"rooms": []string{room.MatrixRoomID}, "types": []string{"m.room.message"}, "limit": 20}}}}
	err = cfg.Call(ctx, cfg.User(auth.CurrentUser(ctx).ID), "POST", "/_matrix/client/v3/search?"+query.Encode(), body, &response)
	if err != nil {
		return out, err
	}
	if _, err = Access(ctx, w, id); err != nil {
		return out, err
	}
	for _, hit := range response.Categories.Events.Results {
		e := hit.Result
		if e.RoomID != room.MatrixRoomID || e.Type != "m.room.message" || e.Content.Body == "" {
			continue
		}
		out.Items = append(out.Items, schema.ChatMessage{ID: e.ID, Sender: e.Sender, Body: e.Content.Body, Timestamp: int(e.Timestamp)})
	}
	if response.Categories.Events.Next != "" {
		out.Next = &response.Categories.Events.Next
	}
	return out, nil
}
