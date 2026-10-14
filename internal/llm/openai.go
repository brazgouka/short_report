package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	openAIAPIURL       = "https://api.openai.com/v1/chat/completions"
	defaultOpenAIModel = "gpt-4o"
)

// OpenAIProvider calls the OpenAI Chat Completions API.
type OpenAIProvider struct {
	APIKey    string
	ModelName string
	Client    *http.Client
}

func (p *OpenAIProvider) Name() string { return "OpenAI" }

func (p *OpenAIProvider) Model() string {
	if strings.TrimSpace(p.ModelName) == "" {
		return defaultOpenAIModel
	}
	return p.ModelName
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIRequest struct {
	Model          string            `json:"model"`
	Messages       []openAIMessage   `json:"messages"`
	MaxTokens      int               `json:"max_tokens"`
	ResponseFormat map[string]string `json:"response_format,omitempty"`
}

type openAIResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

const jsonShapeHint = ` Respond with a single JSON object of the shape {"items": [{"headline": "...", "detail": "...", "sources": [1, 2]}]} and nothing else.`

// Summarize sends the news batch to OpenAI and returns structured
// briefing items. response_format:"json_object" guarantees valid JSON
// syntax (not a specific shape), so the result is parsed defensively via
// parseBriefingJSON, same as the Ollama provider.
func (p *OpenAIProvider) Summarize(ctx context.Context, req Request) ([]BriefingItem, error) {
	if strings.TrimSpace(p.APIKey) == "" {
		return nil, errors.New("no OpenAI API key configured")
	}

	body := openAIRequest{
		Model:     p.Model(),
		MaxTokens: maxTokensFor(req.MaxItems),
		Messages: []openAIMessage{
			{Role: "system", Content: systemPrompt + jsonShapeHint},
			{Role: "user", Content: buildUserPrompt(req)},
		},
		ResponseFormat: map[string]string{"type": "json_object"},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encoding openai request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, openAIAPIURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("building openai request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.APIKey)

	resp, err := p.Client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("calling OpenAI API: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("reading openai response: %w", err)
	}

	var or openAIResponse
	if err := json.Unmarshal(data, &or); err != nil {
		return nil, fmt.Errorf("openai returned an unreadable response (status %d)", resp.StatusCode)
	}

	if resp.StatusCode != http.StatusOK {
		if or.Error != nil && or.Error.Message != "" {
			return nil, fmt.Errorf("openai: %s", or.Error.Message)
		}
		return nil, fmt.Errorf("openai: unexpected status %d", resp.StatusCode)
	}

	if len(or.Choices) == 0 {
		return nil, errors.New("openai returned no choices")
	}
	text := strings.TrimSpace(or.Choices[0].Message.Content)
	if text == "" {
		return nil, errors.New("openai returned an empty response")
	}

	items, err := parseBriefingJSON(text)
	if err != nil {
		return nil, fmt.Errorf("openai returned unparsable briefing data: %w", err)
	}
	return items, nil
}
