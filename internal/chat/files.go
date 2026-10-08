package chat

import (
	"context"
	"encoding/base64"
	"github.com/agim/lidza/pkg/router"
	"github.com/google/uuid"
	"net/url"
	"strings"
	"thura/internal/drive"
	"thura/schema"
)

// Outbound files must already belong to this workspace's private Drive.
func SendFile(ctx context.Context, w, id string, in schema.ChatFileInput) (schema.ChatEventResult, error) {
	out := schema.ChatEventResult{}
	if _, err := uuid.Parse(in.TransactionID); err != nil {
		return out, router.Errorf(422, "transaction ID required")
	}
	r, cfg, user, err := connected(ctx, w, id)
	if err != nil {
		return out, err
	}
	file, err := drive.Get(ctx, w, in.FileID)
	if err != nil {
		return out, err
	}
	content, err := drive.Content(ctx, file, 0)
	if err != nil {
		return out, err
	}
	data, err := base64.StdEncoding.DecodeString(content.Data)
	if err != nil {
		return out, err
	}
	media, err := cfg.Upload(ctx, user, content.Name, data, "application/octet-stream")
	if err != nil {
		return out, err
	}
	var response struct {
		ID string `json:"event_id"`
	}
	err = cfg.Call(ctx, user, "PUT", "/_matrix/client/v3/rooms/"+url.PathEscape(r.MatrixRoomID)+"/send/m.room.message/"+url.PathEscape(in.TransactionID), map[string]any{"msgtype": "m.file", "body": content.Name, "url": media, "info": map[string]any{"size": len(data), "mimetype": "application/octet-stream"}}, &response)
	return schema.ChatEventResult{EventID: response.ID}, err
}

// The event authorizes this media reference. Callers cannot pass a media URL or
// use the app token as an arbitrary download proxy. Always force a download.
func DownloadFile(ctx context.Context, w, id, event string) (schema.FileContent, error) {
	out := schema.FileContent{}
	r, cfg, user, err := connected(ctx, w, id)
	if err != nil {
		return out, err
	}
	if err = checkEvent(ctx, cfg, user, r.MatrixRoomID, event); err != nil {
		return out, err
	}
	var e matrixEvent
	err = cfg.Call(ctx, user, "GET", "/_matrix/client/v3/rooms/"+url.PathEscape(r.MatrixRoomID)+"/event/"+url.PathEscape(event), nil, &e)
	if err != nil {
		return out, err
	}
	if e.Content.Msgtype != "m.file" && e.Content.Msgtype != "m.image" && e.Content.Msgtype != "m.audio" && e.Content.Msgtype != "m.video" {
		return out, router.Errorf(404, "attachment not found")
	}
	u, err := url.Parse(e.Content.URL)
	if err != nil || u.Scheme != "mxc" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(u.Host, " /?#") || len(strings.Split(strings.TrimPrefix(u.Path, "/"), "/")) != 1 || len(u.Path) < 2 {
		return out, router.Errorf(422, "invalid attachment reference")
	}
	data, err := cfg.Media(ctx, user, "GET", "/_matrix/client/v1/media/download/"+url.PathEscape(u.Host)+"/"+url.PathEscape(strings.TrimPrefix(u.Path, "/")), nil, "")
	if err != nil {
		return out, err
	}
	return schema.FileContent{Name: e.Content.Body, ContentType: "application/octet-stream", Data: base64.StdEncoding.EncodeToString(data)}, nil
}
