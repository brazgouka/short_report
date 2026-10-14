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
	anthropicAPIURL       = "https://api.anthropic.com/v1/messages"
	anthropicVersion      = "2023-06-01"
	defaultAnthropicModel = "claude-sonnet-5"
)

// AnthropicProvider calls the hosted Anthropic Messages API.
type AnthropicProvider struct {
	APIKey    string
	ModelName string
	Client    *http.Client
}

func (p *AnthropicProvider) Name() string { return "Anthropic" }

func (p *AnthropicProvider) Model() string {
	if strings.TrimSpace(p.ModelName) == "" {
		return defaultAnthropicModel
	}
	return p.ModelName
}

// briefingInputSchema forces Claude to return items matching BriefingItem
// exactly, via tool use, instead of free-form prose.
const briefingInputSchema = `{
	"type": "object",
	"properties": {
		"items": {
			"type": "array",
			"items": {
				"type": "object",
				"properties": {
					"headline": {"type": "string", "description": "An extremely short label for this story: a bare keyword or short phrase, never a full sentence."},
					"detail": {"type": "string", "description": "A few plain-text sentences explaining the story in more depth, with no markdown formatting."},
					"sources": {"type": "array", "items": {"type": "integer"}, "description": "1-based numbers of the source articles this item is drawn from, from the numbered list provided."}
				},
				"required": ["headline", "detail", "sources"]
			}
		}
	},
	"required": ["items"]
}`

type anthropicRequest struct {
	Model      string             `json:"model"`
	MaxTokens  int                `json:"max_tokens"`
	System     string             `json:"system,omitempty"`
	Messages   []anthropicMessage `json:"messages"`
	Tools      []anthropicTool    `json:"tools,omitempty"`
	ToolChoice map[string]string  `json:"tool_choice,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anthropicResponse struct {
	Content []struct {
		Type  string          `json:"type"`
		Text  string          `json:"text"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	} `json:"content"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// Summarize sends the news batch to Claude and returns structured
// briefing items, forcing the response through a tool call so it's
// always valid, schema-conformant JSON rather than free text.
func (p *AnthropicProvider) Summarize(ctx context.Context, req Request) ([]BriefingItem, error) {
	if strings.TrimSpace(p.APIKey) == "" {
		return nil, errors.New("no Anthropic API key configured")
	}

	body := anthropicRequest{
		Model:     p.Model(),
		MaxTokens: maxTokensFor(req.MaxItems),
		System:    systemPrompt,
		Messages:  []anthropicMessage{{Role: "user", Content: buildUserPrompt(req)}},
		Tools: []anthropicTool{{
			Name:        "submit_briefing",
			Description: "Submit the structured news briefing items.",
			InputSchema: json.RawMessage(briefingInputSchema),
		}},
		ToolChoice: map[string]string{"type": "tool", "name": "submit_briefing"},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encoding anthropic request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicAPIURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("building anthropic request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("x-api-key", p.APIKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)

	resp, err := p.Client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("calling Anthropic API: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("reading anthropic response: %w", err)
	}

	var ar anthropicResponse
	if err := json.Unmarshal(data, &ar); err != nil {
		return nil, fmt.Errorf("anthropic returned an unreadable response (status %d)", resp.StatusCode)
	}

	if resp.StatusCode != http.StatusOK {
		if ar.Error != nil && ar.Error.Message != "" {
			return nil, fmt.Errorf("anthropic: %s", ar.Error.Message)
		}
		return nil, fmt.Errorf("anthropic: unexpected status %d", resp.StatusCode)
	}

	for _, c := range ar.Content {
		if c.Type != "tool_use" || c.Name != "submit_briefing" {
			continue
		}
		var parsed struct {
			Items []BriefingItem `json:"items"`
		}
		if err := json.Unmarshal(c.Input, &parsed); err != nil {
			return nil, fmt.Errorf("anthropic returned malformed briefing data: %w", err)
		}
		if len(parsed.Items) == 0 {
			return nil, errors.New("anthropic returned no briefing items")
		}
		return parsed.Items, nil
	}

	return nil, errors.New("anthropic did not return a structured briefing")
}
