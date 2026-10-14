package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

const defaultOllamaHost = "http://localhost:11434"

// OllamaProvider calls a locally running Ollama server, keeping news data
// (and any API cost) entirely on the user's machine.
type OllamaProvider struct {
	Host      string
	ModelName string
	Client    *http.Client
}

func (p *OllamaProvider) Name() string  { return "Ollama (local)" }
func (p *OllamaProvider) Model() string { return p.ModelName }

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Format   string          `json:"format,omitempty"`
}

type ollamaChatResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Error string `json:"error"`
}

// Summarize sends the news batch to a local Ollama chat model and returns
// structured briefing items. Ollama's "format":"json" guarantees valid
// JSON syntax (not a specific shape), so the result is parsed defensively
// via parseBriefingJSON.
func (p *OllamaProvider) Summarize(ctx context.Context, req Request) ([]BriefingItem, error) {
	if strings.TrimSpace(p.ModelName) == "" {
		return nil, errors.New("no Ollama model configured")
	}

	host := strings.TrimRight(strings.TrimSpace(p.Host), "/")
	if host == "" {
		host = defaultOllamaHost
	}

	body := ollamaChatRequest{
		Model:  p.ModelName,
		Stream: false,
		Format: "json",
		Messages: []ollamaMessage{
			{Role: "system", Content: systemPrompt + ` Respond with a single JSON object of the shape {"items": [{"headline": "...", "detail": "...", "sources": [1, 2]}]} and nothing else.`},
			{Role: "user", Content: buildUserPrompt(req)},
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encoding ollama request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, host+"/api/chat", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("building ollama request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")

	resp, err := p.Client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("could not reach local Ollama server at %s (is `ollama serve` running?): %w", host, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("reading ollama response: %w", err)
	}

	var or ollamaChatResponse
	if err := json.Unmarshal(data, &or); err != nil {
		return nil, fmt.Errorf("ollama returned an unreadable response (status %d)", resp.StatusCode)
	}

	if resp.StatusCode != http.StatusOK {
		if or.Error != "" {
			return nil, fmt.Errorf("ollama: %s", or.Error)
		}
		return nil, fmt.Errorf("ollama: unexpected status %d", resp.StatusCode)
	}

	text := strings.TrimSpace(or.Message.Content)
	if text == "" {
		return nil, errors.New("ollama returned an empty response")
	}

	items, err := parseBriefingJSON(text)
	if err != nil {
		return nil, fmt.Errorf("ollama returned unparsable briefing data: %w", err)
	}
	return items, nil
}

type ollamaTagsResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

// ListModels queries a local Ollama server for the models it currently has
// pulled, so callers can offer a selection list instead of free-text entry.
func ListModels(ctx context.Context, client *http.Client, host string) ([]string, error) {
	host = strings.TrimRight(strings.TrimSpace(host), "/")
	if host == "" {
		host = defaultOllamaHost
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, host+"/api/tags", nil)
	if err != nil {
		return nil, fmt.Errorf("building ollama request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach local Ollama server at %s (is `ollama serve` running?): %w", host, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama: unexpected status %d", resp.StatusCode)
	}

	var tags ollamaTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return nil, fmt.Errorf("ollama returned an unreadable response")
	}

	names := make([]string, 0, len(tags.Models))
	for _, m := range tags.Models {
		names = append(names, m.Name)
	}
	sort.Strings(names)
	return names, nil
}
