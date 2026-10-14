package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const defaultGeminiModel = "gemini-2.0-flash"

// GeminiProvider calls Google's Generative Language API.
type GeminiProvider struct {
	APIKey    string
	ModelName string
	Client    *http.Client
}

func (p *GeminiProvider) Name() string { return "Gemini" }

func (p *GeminiProvider) Model() string {
	if strings.TrimSpace(p.ModelName) == "" {
		return defaultGeminiModel
	}
	return p.ModelName
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiRequest struct {
	Contents          []geminiContent `json:"contents"`
	SystemInstruction *geminiContent  `json:"systemInstruction,omitempty"`
	GenerationConfig  struct {
		ResponseMimeType string `json:"responseMimeType,omitempty"`
		MaxOutputTokens  int    `json:"maxOutputTokens,omitempty"`
	} `json:"generationConfig"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Summarize sends the news batch to Gemini and returns structured
// briefing items. responseMimeType:"application/json" guarantees valid
// JSON syntax (not a specific shape), so the result is parsed defensively
// via parseBriefingJSON, same as the Ollama and OpenAI providers.
func (p *GeminiProvider) Summarize(ctx context.Context, req Request) ([]BriefingItem, error) {
	if strings.TrimSpace(p.APIKey) == "" {
		return nil, errors.New("no Gemini API key configured")
	}

	body := geminiRequest{
		Contents:          []geminiContent{{Parts: []geminiPart{{Text: buildUserPrompt(req)}}}},
		SystemInstruction: &geminiContent{Parts: []geminiPart{{Text: systemPrompt + jsonShapeHint}}},
	}
	body.GenerationConfig.ResponseMimeType = "application/json"
	body.GenerationConfig.MaxOutputTokens = maxTokensFor(req.MaxItems)

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encoding gemini request: %w", err)
	}

	apiURL := fmt.Sprintf(
		"https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s",
		url.PathEscape(p.Model()), url.QueryEscape(p.APIKey),
	)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("building gemini request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")

	resp, err := p.Client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("calling Gemini API: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("reading gemini response: %w", err)
	}

	var gr geminiResponse
	if err := json.Unmarshal(data, &gr); err != nil {
		return nil, fmt.Errorf("gemini returned an unreadable response (status %d)", resp.StatusCode)
	}

	if resp.StatusCode != http.StatusOK {
		if gr.Error != nil && gr.Error.Message != "" {
			return nil, fmt.Errorf("gemini: %s", gr.Error.Message)
		}
		return nil, fmt.Errorf("gemini: unexpected status %d", resp.StatusCode)
	}

	if len(gr.Candidates) == 0 || len(gr.Candidates[0].Content.Parts) == 0 {
		return nil, errors.New("gemini returned no content")
	}
	text := strings.TrimSpace(gr.Candidates[0].Content.Parts[0].Text)
	if text == "" {
		return nil, errors.New("gemini returned an empty response")
	}

	items, err := parseBriefingJSON(text)
	if err != nil {
		return nil, fmt.Errorf("gemini returned unparsable briefing data: %w", err)
	}
	return items, nil
}
