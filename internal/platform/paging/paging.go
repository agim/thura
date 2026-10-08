package paging

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/agim/lidza/pkg/router"
)

type Request struct {
	Limit  int32
	Search string
	Cursor string
}

func Read(r *http.Request, defaultLimit int32) (Request, error) {
	p := Request{Limit: defaultLimit, Search: strings.TrimSpace(r.URL.Query().Get("search")), Cursor: r.URL.Query().Get("cursor")}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 200 {
			return p, router.Errorf(422, "limit must be between 1 and 200")
		}
		p.Limit = int32(limit)
	}
	if !utf8.ValidString(p.Search) || utf8.RuneCountInString(p.Search) > 200 || len(p.Cursor) > 2048 {
		return p, router.Errorf(422, "search or cursor is too long")
	}
	return p, nil
}

func Decode(raw string, into any) error {
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || json.Unmarshal(data, into) != nil {
		return router.Errorf(422, "invalid cursor")
	}
	return nil
}

func Encode(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(data)
}
