// Package llm defines a small provider-agnostic interface for turning a
// batch of raw news items into a structured, word-budgeted briefing, with
// implementations for the Anthropic API and a local Ollama server.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Request carries everything a provider needs to write one briefing.
type Request struct {
	Date          string // human-readable, e.g. "Wednesday, July 15, 2026"
	Tags          []string
	WordTarget    int    // exact word count, or the lower bound of a range
	WordTargetMax int    // upper bound of a range; equal to WordTarget for an exact ask
	MaxItems      int    // hard cap on number of briefing items, enforced server-side too
	Articles      string // preformatted, numbered "source articles" block
}

// BriefingItem is one distinct story in a structured briefing: a short
// always-visible headline, a longer explanation shown on expand, and the
// 1-based numbers (matching Request.Articles) of the source articles it
// was drawn from.
type BriefingItem struct {
	Headline string `json:"headline"`
	Detail   string `json:"detail"`
	Sources  []int  `json:"sources"`
}

// Provider turns news into a briefing of roughly Request.WordTarget words
// (measured across item headlines), written by an LLM (hosted or local).
type Provider interface {
	Name() string  // human-readable backend name, e.g. "Anthropic"
	Model() string // model identifier in use
	Summarize(ctx context.Context, req Request) ([]BriefingItem, error)
}

const systemPrompt = `You are a careful, neutral news editor. You will be given real headlines and short descriptions gathered moments ago from live RSS feeds, each numbered and tagged by topic. Break the most important stories into short, distinct briefing items — never invent details, names, numbers, or sources beyond what's given. Prioritize by real-world significance. Never use markdown formatting anywhere in your output (no **, -, #, or similar) — plain text only.`

func buildUserPrompt(req Request) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Today's date: %s\n", req.Date)
	fmt.Fprintf(&b, "Topics selected by the user: %s\n\n", strings.Join(req.Tags, ", "))
	b.WriteString("Numbered source articles:\n\n")
	b.WriteString(req.Articles)
	b.WriteString("\n\n")

	isRange := req.WordTargetMax > req.WordTarget
	fmt.Fprintf(&b, "Produce AT MOST %d briefing item(s) — this is a strict limit, never exceed it, though fewer is fine if there isn't enough genuinely distinct news. Cover only the most important stories, most significant first.\n\n", req.MaxItems)

	switch {
	case !isRange && req.WordTarget <= 13:
		fmt.Fprintf(&b, "Each headline must be an extremely short label — a bare keyword or a 2-4 word phrase, never a full sentence (for example \"Wildfires\" or \"Ceasefire talks\", not \"Wildfires continue to spread\"). Still give every item a short plain-text detail of 1-3 sentences, and list which source article numbers it draws from.")
	case isRange:
		fmt.Fprintf(&b, "Each headline should be a short, punchy phrase rather than a full sentence, with headlines summing to roughly %d-%d words combined. Give every item a plain-text detail of 2-4 sentences, and list which source article numbers it draws from.", req.WordTarget, req.WordTargetMax)
	default:
		fmt.Fprintf(&b, "Each headline should be a short phrase, with headlines summing to roughly %d words combined. Give every item a plain-text detail of 2-4 sentences, and list which source article numbers it draws from.", req.WordTarget)
	}
	return b.String()
}

// maxTokensFor sizes the response budget mainly off the item count, since
// each item's JSON envelope + detail sentence costs roughly the same
// regardless of how short its headline is asked to be.
func maxTokensFor(maxItems int) int {
	t := maxItems*120 + 400
	if t < 600 {
		t = 600
	}
	if t > 6000 {
		t = 6000
	}
	return t
}

// stripCodeFence removes a ```json ... ``` (or bare ```) wrapper some local
// models add around JSON output despite being told not to.
func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

// parseBriefingJSON accepts either {"items": [...]} or a bare [...] array,
// since not every model wraps its output the way it's asked to.
func parseBriefingJSON(text string) ([]BriefingItem, error) {
	text = stripCodeFence(text)

	var wrapped struct {
		Items []BriefingItem `json:"items"`
	}
	if err := json.Unmarshal([]byte(text), &wrapped); err == nil && len(wrapped.Items) > 0 {
		return wrapped.Items, nil
	}

	var bare []BriefingItem
	if err := json.Unmarshal([]byte(text), &bare); err == nil && len(bare) > 0 {
		return bare, nil
	}

	return nil, fmt.Errorf("no valid briefing items found in model output")
}
