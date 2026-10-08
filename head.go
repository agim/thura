package main

import (
	_ "embed"
	"encoding/json"
	"net/http"

	"github.com/agim/lidza"
)

// Public route descriptions are shared with the browser. They never include
// account, workspace, file, invitation or token contents.
//
//go:embed src/page-metadata.json
var pageMetadataJSON string

type pageMetadata struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	NoIndex     bool   `json:"noIndex"`
}

var pageHeads = func() map[string]pageMetadata {
	var pages map[string]pageMetadata
	if err := json.Unmarshal([]byte(pageMetadataJSON), &pages); err != nil {
		panic(err)
	}
	return pages
}()

func head(r *http.Request) (lidza.Head, bool) {
	page, ok := pageHeads[r.URL.Path]
	if !ok {
		return lidza.Head{}, false
	}
	return lidza.Head{Title: page.Title, Description: page.Description, NoIndex: page.NoIndex, SiteName: "Thura"}, true
}
