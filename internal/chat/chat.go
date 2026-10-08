package chat

import (
	"context"
	"errors"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/router"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"net/url"
	"strings"
	q "thura/db/queries/gen"
	provider "thura/internal/providers/matrix"
	"thura/internal/workspace"
	"thura/schema"
)

func View(r q.ChatRoom) schema.ChatRoom {
	return schema.ChatRoom{ID: r.ID, WorkspaceID: r.WorkspaceID, MatrixRoomID: r.MatrixRoomID, RequestID: r.RequestID, Name: r.Name, Direct: r.Direct, CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt}
}
func List(ctx context.Context, w string) (schema.ChatRoomList, error) {
	out := schema.ChatRoomList{Items: []schema.ChatRoom{}}
	if err := workspace.RequireMember(ctx, w); err != nil {
		return out, err
	}
	_, err := provider.Load()
	out.Configured = err == nil
	rows, err := q.New(db.From(ctx)).ListChatRooms(ctx, q.ListChatRoomsParams{WorkspaceID: w, Subject: auth.CurrentUser(ctx).ID})
	for _, r := range rows {
		out.Items = append(out.Items, View(r))
	}
	return out, err
}
func Access(ctx context.Context, w, id string) (q.ChatRoom, error) {
	if err := workspace.RequireMember(ctx, w); err != nil {
		return q.ChatRoom{}, err
	}
	if !workspace.ValidID(id) {
		return q.ChatRoom{}, router.Errorf(404, "room not found")
	}
	r, err := q.New(db.From(ctx)).GetChatRoom(ctx, q.GetChatRoomParams{WorkspaceID: w, ID: id, Subject: auth.CurrentUser(ctx).ID})
	if errors.Is(err, pgx.ErrNoRows) {
		err = router.Errorf(404, "room not found")
	}
	return r, err
}
func Create(ctx context.Context, w string, in schema.ChatRoomInput) (schema.ChatRoom, error) {
	if err := workspace.RequireMember(ctx, w); err != nil {
		return schema.ChatRoom{}, err
	}
	in.Name = strings.TrimSpace(in.Name)
	if err := in.Validate(); err != nil {
		return schema.ChatRoom{}, err
	}
	if _, err := uuid.Parse(in.RequestID); err != nil {
		return schema.ChatRoom{}, router.Errorf(422, "request ID required")
	}
	subject := auth.CurrentUser(ctx).ID
	cfg, err := provider.Load()
	if err != nil {
		return schema.ChatRoom{}, err
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return schema.ChatRoom{}, err
	}
	defer tx.Rollback(ctx)
	queries := q.New(tx)
	if _, err = queries.LockWorkspace(ctx, w); err != nil {
		return schema.ChatRoom{}, err
	}
	// Membership may have changed while waiting for the workspace lock.
	member, err := queries.IsWorkspaceMember(ctx, q.IsWorkspaceMemberParams{Scope: w, Subject: subject})
	if err != nil {
		return schema.ChatRoom{}, err
	}
	if !member {
		return schema.ChatRoom{}, router.Errorf(404, "workspace not found")
	}
	direct := in.Participant != nil && *in.Participant != ""
	if direct {
		if *in.Participant == subject {
			return schema.ChatRoom{}, router.Errorf(422, "choose another member")
		}
		member, err = queries.IsWorkspaceMember(ctx, q.IsWorkspaceMemberParams{Scope: w, Subject: *in.Participant})
		if err != nil {
			return schema.ChatRoom{}, err
		}
		if !member {
			return schema.ChatRoom{}, router.Errorf(422, "recipient must be a workspace member")
		}
	}
	existing, err := queries.FindChatRequest(ctx, q.FindChatRequestParams{WorkspaceID: w, RequestID: in.RequestID})
	if err == nil {
		if existing.CreatedBy != subject {
			return schema.ChatRoom{}, router.Errorf(409, "request ID already used")
		}
		return View(existing), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return schema.ChatRoom{}, err
	}
	count, err := queries.CountWorkspaceRooms(ctx, w)
	if err != nil {
		return schema.ChatRoom{}, err
	}
	if count >= 100 {
		return schema.ChatRoom{}, router.Errorf(409, "workspace room limit reached")
	}
	if err = cfg.EnsureUser(ctx, cfg.Bot()); err != nil {
		return schema.ChatRoom{}, err
	}
	aliasLocal := "thura_room_" + strings.ReplaceAll(w, "-", "") + "_" + strings.ReplaceAll(in.RequestID, "-", "")
	alias := "#" + aliasLocal + ":" + cfg.ServerName
	var matrixRoom struct {
		ID string `json:"room_id"`
	}
	err = cfg.Call(ctx, cfg.Bot(), "GET", "/_matrix/client/v3/directory/room/"+url.PathEscape(alias), nil, &matrixRoom)
	if e, ok := err.(*provider.Error); ok && e.Status == 404 {
		err = cfg.Call(ctx, cfg.Bot(), "POST", "/_matrix/client/v3/createRoom", map[string]any{"name": in.Name, "room_alias_name": aliasLocal, "visibility": "private", "preset": "private_chat", "creation_content": map[string]any{"m.federate": true}, "initial_state": []any{map[string]any{"type": "m.room.history_visibility", "state_key": "", "content": map[string]string{"history_visibility": "joined"}}}, "power_level_content_override": map[string]any{"invite": 100, "kick": 100, "ban": 100, "redact": 100, "state_default": 100, "events_default": 0, "users": map[string]int{cfg.Bot(): 100}}}, &matrixRoom)
	}
	if err != nil {
		return schema.ChatRoom{}, err
	}
	if matrixRoom.ID == "" {
		return schema.ChatRoom{}, router.Errorf(502, "Matrix did not return a room")
	}
	if err = cfg.Join(ctx, matrixRoom.ID, cfg.User(subject)); err != nil {
		return schema.ChatRoom{}, err
	}
	if direct {
		if err = cfg.Join(ctx, matrixRoom.ID, cfg.User(*in.Participant)); err != nil {
			return schema.ChatRoom{}, err
		}
	}
	r, err := queries.AddChatRoom(ctx, q.AddChatRoomParams{WorkspaceID: w, MatrixRoomID: matrixRoom.ID, RequestID: in.RequestID, Name: in.Name, Direct: direct, CreatedBy: subject})
	if err != nil {
		return schema.ChatRoom{}, err
	}
	if direct {
		for _, s := range []string{subject, *in.Participant} {
			if err = queries.AddChatParticipant(ctx, q.AddChatParticipantParams{RoomID: r.ID, Subject: s}); err != nil {
				return schema.ChatRoom{}, err
			}
		}
	}
	return View(r), tx.Commit(ctx)
}
func connected(ctx context.Context, w, id string) (q.ChatRoom, provider.Config, string, error) {
	r, err := Access(ctx, w, id)
	if err != nil {
		return r, provider.Config{}, "", err
	}
	cfg, err := provider.Load()
	if err != nil {
		return r, cfg, "", err
	}
	user := cfg.User(auth.CurrentUser(ctx).ID)
	err = cfg.Join(ctx, r.MatrixRoomID, user)
	if err == nil {
		err = q.New(db.From(ctx)).AddChatParticipant(ctx, q.AddChatParticipantParams{RoomID: r.ID, Subject: auth.CurrentUser(ctx).ID})
	}

	return r, cfg, user, err
}

type matrixEvent struct {
	ID        string `json:"event_id"`
	Sender    string `json:"sender"`
	Type      string `json:"type"`
	Timestamp int64  `json:"origin_server_ts"`
	Content   struct {
		Body    string `json:"body"`
		Msgtype string `json:"msgtype"`
		URL     string `json:"url"`
		Info    struct {
			Size int `json:"size"`
		} `json:"info"`
		Relation struct {
			Reply struct {
				EventID string `json:"event_id"`
			} `json:"m.in_reply_to"`
		} `json:"m.relates_to"`
	} `json:"content"`
}

func Timeline(ctx context.Context, w, id, from string) (schema.ChatTimeline, error) {
	out := schema.ChatTimeline{Items: []schema.ChatMessage{}}
	if len(from) > 2000 {
		return out, router.Errorf(422, "invalid page cursor")
	}
	r, cfg, user, err := connected(ctx, w, id)
	if err != nil {
		return out, err
	}
	query := url.Values{"dir": {"b"}, "limit": {"50"}}
	if from != "" {
		query.Set("from", from)
	}
	var response struct {
		Chunk []matrixEvent `json:"chunk"`
		End   string        `json:"end"`
	}
	err = cfg.Call(ctx, user, "GET", "/_matrix/client/v3/rooms/"+url.PathEscape(r.MatrixRoomID)+"/messages?"+query.Encode(), nil, &response)
	if err != nil {
		return out, err
	}
	for _, e := range response.Chunk {
		if e.Type != "m.room.message" || e.Content.Body == "" {
			continue
		}
		var reply *string
		if e.Content.Relation.Reply.EventID != "" {
			v := e.Content.Relation.Reply.EventID
			reply = &v
		}
		var file *schema.ChatMessageFile
		if e.Content.URL != "" {
			file = &schema.ChatMessageFile{Name: e.Content.Body, Size: e.Content.Info.Size}
		}
		out.Items = append(out.Items, schema.ChatMessage{ID: e.ID, Sender: e.Sender, Body: e.Content.Body, Timestamp: int(e.Timestamp), File: file, ReplyTo: reply})
	}
	if response.End != "" {
		out.Next = &response.End
	}
	return out, nil
}
func checkEvent(ctx context.Context, cfg provider.Config, user, room, event string) error {
	if len(event) > 300 || !strings.HasPrefix(event, "$") {
		return router.Errorf(422, "invalid event ID")
	}
	var e matrixEvent
	if err := cfg.Call(ctx, user, "GET", "/_matrix/client/v3/rooms/"+url.PathEscape(room)+"/event/"+url.PathEscape(event), nil, &e); err != nil {
		return err
	}
	if e.ID != event || e.Type != "m.room.message" {
		return router.Errorf(404, "message not found")
	}
	return nil
}
func Send(ctx context.Context, w, id string, in schema.ChatSendInput) (schema.ChatEventResult, error) {
	out := schema.ChatEventResult{}
	if err := in.Validate(); err != nil {
		return out, err
	}
	if strings.TrimSpace(in.Body) == "" {
		return out, router.Errorf(422, "message required")
	}
	if _, err := uuid.Parse(in.TransactionID); err != nil {
		return out, router.Errorf(422, "transaction ID required")
	}
	r, cfg, user, err := connected(ctx, w, id)
	if err != nil {
		return out, err
	}
	content := map[string]any{"msgtype": "m.text", "body": in.Body}
	if in.ReplyTo != nil && *in.ReplyTo != "" {
		if err = checkEvent(ctx, cfg, user, r.MatrixRoomID, *in.ReplyTo); err != nil {
			return out, err
		}
		content["m.relates_to"] = map[string]any{"m.in_reply_to": map[string]string{"event_id": *in.ReplyTo}}
	}
	var response struct {
		ID string `json:"event_id"`
	}
	err = cfg.Call(ctx, user, "PUT", "/_matrix/client/v3/rooms/"+url.PathEscape(r.MatrixRoomID)+"/send/m.room.message/"+url.PathEscape(in.TransactionID), content, &response)
	return schema.ChatEventResult{EventID: response.ID}, err
}
func Remote(ctx context.Context, w, id string, in schema.ChatRemoteInput, kick bool) error {
	if err := workspace.RequireManager(ctx, w); err != nil {
		return err
	}
	r, err := Access(ctx, w, id)
	if err != nil {
		return err
	}
	if r.Direct {
		return router.Errorf(422, "external invitations require a shared channel")
	}
	cfg, err := provider.Load()
	if err != nil {
		return err
	}
	parts := strings.SplitN(strings.TrimPrefix(in.UserID, "@"), ":", 2)
	if !strings.HasPrefix(in.UserID, "@") || len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.ContainsAny(in.UserID, " /?#\r\n") || strings.EqualFold(parts[1], cfg.ServerName) {
		return router.Errorf(422, "provide a remote Matrix user ID")
	}
	action := "invite"
	if kick {
		action = "ban"
	}
	return cfg.Call(ctx, cfg.Bot(), "POST", "/_matrix/client/v3/rooms/"+url.PathEscape(r.MatrixRoomID)+"/"+action, map[string]string{"user_id": in.UserID}, nil)
}
func Receipt(ctx context.Context, w, id, event string) error {
	r, cfg, user, err := connected(ctx, w, id)
	if err != nil {
		return err
	}
	if err = checkEvent(ctx, cfg, user, r.MatrixRoomID, event); err != nil {
		return err
	}
	return cfg.Call(ctx, user, "POST", "/_matrix/client/v3/rooms/"+url.PathEscape(r.MatrixRoomID)+"/read_markers", map[string]string{"m.fully_read": event, "m.read": event}, nil)
}
