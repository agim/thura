package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/agim/lidza"
	"io"
	"net/http"
	"os"
	"strings"
)

const managedModelURL = "http://127.0.0.1:11434"
const minimumModel = "qwen2.5:0.5b"

func localModelDefaults(v map[string]string) bool {
	if os.Getenv("THURA_MANAGED_OLLAMA") != "true" || v["LLM_PROVIDER"] != "ollama" {
		return false
	}
	changed := false
	if v["LLM_BASE_URL"] == "" {
		v["LLM_BASE_URL"] = managedModelURL
		changed = true
	}
	if v["LLM_BASE_URL"] == managedModelURL && v["LLM_MODEL"] == "" {
		v["LLM_MODEL"] = minimumModel
		changed = true
	}
	return changed
}

// Model installation occurs only on an explicit saved-provider check. Hosted
// provider selections never contact Ollama or download/load a local model.
func ensureLocalModel(ctx context.Context, v map[string]string) error {
	if os.Getenv("THURA_MANAGED_OLLAMA") != "true" || v["LLM_PROVIDER"] != "ollama" || strings.TrimRight(v["LLM_BASE_URL"], "/") != managedModelURL {
		return nil
	}
	if v["LLM_MODEL"] != minimumModel {
		return errors.New("the minimum local runtime supports qwen2.5:0.5b; use an external Ollama service or a measured larger profile for other models")
	}
	client := *lidza.HTTPClient(ctx)
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, "GET", managedModelURL+"/api/tags", nil)
	if err != nil {
		return err
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	decodeErr := json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&tags)
	closeErr := res.Body.Close()
	if res.StatusCode != 200 || decodeErr != nil || closeErr != nil {
		return errors.New("local model inventory is unavailable")
	}
	for _, model := range tags.Models {
		if model.Name == minimumModel {
			return nil
		}
	}
	raw, err := json.Marshal(map[string]any{"model": minimumModel, "stream": false})
	if err != nil {
		return err
	}
	req, err = http.NewRequestWithContext(ctx, "POST", managedModelURL+"/api/pull", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	var result struct {
		Status string `json:"status"`
	}
	if res.StatusCode != 200 || json.NewDecoder(io.LimitReader(res.Body, 8192)).Decode(&result) != nil || result.Status != "success" {
		return errors.New("local model installation did not complete")
	}
	return nil
}
