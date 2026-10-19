// Package mailbench shares the isolated official-LLM-pack benchmark protocol.
package mailbench

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/agim/lidza/packs/llm"
	"io"
	"os"
	"strings"
	"time"
)

type Config struct {
	Prompt    string
	Parse     func(string) (string, error)
	MaxTokens int
	Schema    map[string]any
	Label     string
}
type input struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Source string `json:"source"`
	Text   string `json:"text"`
}
type result struct {
	ID           string  `json:"id"`
	Label        string  `json:"label"`
	Source       string  `json:"source"`
	Prediction   string  `json:"prediction"`
	Error        string  `json:"error,omitempty"`
	Millis       float64 `json:"millis"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
}

func evidence(row input) ([]byte, error) {
	return json.Marshal(struct {
		Text string `json:"text"`
	}{Text: row.Text})
}

func Run(config Config) error {
	var rows []input
	d := json.NewDecoder(io.LimitReader(os.Stdin, 4<<20))
	if d.Decode(&rows) != nil || d.Decode(new(any)) != io.EOF || len(rows) < 1 || len(rows) > 128 {
		return errors.New("supply 1–128 bounded benchmark rows")
	}
	var instruction struct{ Version, System string }
	if json.Unmarshal([]byte(config.Prompt), &instruction) != nil || instruction.System == "" {
		return errors.New("invalid fixed prompt")
	}
	client, err := llm.New(llm.Config{Provider: "compatible", BaseURL: "http://127.0.0.1:8080", Model: "thura-mail-llama1b", EmbedProvider: "none", Timeout: 120 * time.Second, MaxAttempts: 1, MaxTokens: config.MaxTokens})
	if err != nil {
		return err
	}
	zero := 0.0
	seen := map[string]bool{}
	results := []result{}
	for _, row := range rows {
		if seen[row.ID] || row.ID == "" || len(row.Text) > 12000 || strings.TrimSpace(row.Text) == "" {
			return errors.New("invalid or duplicate benchmark input")
		}
		seen[row.ID] = true
		if row.Label != "legitimate" && row.Label != "spam" && row.Label != "phishing" {
			return errors.New("invalid benchmark ground truth")
		}
		// Only email text enters the request. IDs, source and expected labels stay out.
		payload, err := evidence(row)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		start := time.Now()
		reply, callErr := client.Chat(ctx, llm.Request{System: instruction.System, Messages: []llm.Message{{Role: llm.User, Content: string(payload)}}, Schema: config.Schema, MaxTokens: config.MaxTokens, Temperature: &zero, Label: config.Label})
		cancel()
		item := result{ID: row.ID, Label: row.Label, Source: row.Source, Millis: float64(time.Since(start).Microseconds()) / 1000, InputTokens: reply.Usage.Input, OutputTokens: reply.Usage.Output}
		if callErr != nil {
			item.Error = "inference failed or timed out"
		} else if reply.Stop != "end" || len(reply.ToolCalls) != 0 || reply.Model != "thura-mail-llama1b" {
			item.Error = "incomplete or unexpected model output"
		} else {
			item.Prediction, err = config.Parse(reply.Text)
			if err != nil {
				item.Error = err.Error()
			}
		}
		results = append(results, item)
		fmt.Fprintf(os.Stderr, "completed %d/%d\n", len(results), len(rows))
	}
	files := []string{"memory.max", "memory.peak", "memory.swap.max", "memory.events", "cpu.max", "cpu.stat"}
	groups := map[string]string{}
	for _, name := range files {
		data, err := os.ReadFile("/sys/fs/cgroup/" + name)
		if err != nil {
			return err
		}
		groups[name] = strings.TrimSpace(string(data))
	}
	hash := sha256.Sum256([]byte(config.Prompt))
	var buffer bytes.Buffer
	if err := json.NewEncoder(&buffer).Encode(map[string]any{"results": results, "cgroup": groups, "prompt_sha256": hex.EncodeToString(hash[:]), "prompt_version": instruction.Version, "llm_pack": "github.com/agim/lidza/packs/llm@v0.1.123"}); err != nil {
		return err
	}
	_, err = os.Stdout.Write(buffer.Bytes())
	return err
}
