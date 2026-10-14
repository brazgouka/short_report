// Package config manages per-user accounts: password-authenticated
// registration/login and each user's persisted preferences (LLM provider
// choice, credentials, selected news tags, banned sources, and the
// last-used briefing length). Storage lives in store.go, backed by
// SQLite; this file holds the domain types and pure settings logic.
package config

import (
	"errors"
	"os"
	"regexp"
	"strings"
)

type Provider string

const (
	ProviderAnthropic Provider = "anthropic"
	ProviderOllama    Provider = "ollama"
	ProviderOpenAI    Provider = "openai"
	ProviderGemini    Provider = "gemini"
)

func isValidProvider(p Provider) bool {
	switch p {
	case ProviderAnthropic, ProviderOllama, ProviderOpenAI, ProviderGemini:
		return true
	}
	return false
}

// AvailableTags is the default catalog offered in the UI. Users can add
// arbitrary custom tags too; anything not in the curated feed map falls
// back to a live news search.
var AvailableTags = []string{
	"World", "USA", "New York", "Business", "Technology",
	"Science", "Health", "Soccer", "Politics", "Entertainment", "War",
}

// WordStop is one selectable position on the briefing-length slider. Small
// stops ask for an exact word count; larger ones ask for a range, since
// asking an LLM to hit an exact count of e.g. 350 words is meaningless —
// only the ballpark matters once the briefing is more than a sentence or
// two. The final stop (Infinite) means "no summarization at all: show
// every fetched headline."
type WordStop struct {
	Label    string `json:"label"`
	Min      int    `json:"min"`
	Max      int    `json:"max"`
	MaxItems int    `json:"maxItems"`
	Infinite bool   `json:"infinite"`
}

// WordStops is the canonical, server-owned set of slider positions.
// MaxItems is enforced server-side after the LLM responds, since asking a
// (especially small/local) model to hit a total word budget is unreliable —
// an explicit item-count cap is something models follow far more reliably.
var WordStops = []WordStop{
	{Label: "1", Min: 1, Max: 1, MaxItems: 1},
	{Label: "2", Min: 2, Max: 2, MaxItems: 1},
	{Label: "3", Min: 3, Max: 3, MaxItems: 1},
	{Label: "5", Min: 5, Max: 5, MaxItems: 2},
	{Label: "8", Min: 8, Max: 8, MaxItems: 2},
	{Label: "13", Min: 13, Max: 13, MaxItems: 3},
	{Label: "20-30", Min: 20, Max: 30, MaxItems: 5},
	{Label: "30-50", Min: 30, Max: 50, MaxItems: 6},
	{Label: "50-100", Min: 50, Max: 100, MaxItems: 8},
	{Label: "100-200", Min: 100, Max: 200, MaxItems: 10},
	{Label: "200-300", Min: 200, Max: 300, MaxItems: 12},
	{Label: "300-500", Min: 300, Max: 500, MaxItems: 15},
	{Label: "500-1000", Min: 500, Max: 1000, MaxItems: 18},
	{Label: "1000-5000", Min: 1000, Max: 5000, MaxItems: 25},
	{Label: "∞", Infinite: true},
}

// DefaultWordStopIndex points at a moderate "50-100 words" briefing.
const DefaultWordStopIndex = 8

// Settings is one user's persisted preferences.
type Settings struct {
	Provider       Provider `json:"provider"`
	AnthropicKey   string   `json:"anthropicKey,omitempty"`
	AnthropicModel string   `json:"anthropicModel"`
	OpenAIKey      string   `json:"openaiKey,omitempty"`
	OpenAIModel    string   `json:"openaiModel"`
	GeminiKey      string   `json:"geminiKey,omitempty"`
	GeminiModel    string   `json:"geminiModel"`
	OllamaHost     string   `json:"ollamaHost"`
	OllamaModel    string   `json:"ollamaModel"`
	Tags           []string `json:"tags"`
	WordStopIndex  int      `json:"wordStopIndex"`
	BannedSources  []string `json:"bannedSources"`
}

func defaultSettings() Settings {
	return Settings{
		Provider:       ProviderAnthropic,
		AnthropicModel: "claude-sonnet-5",
		OpenAIModel:    "gpt-4o",
		GeminiModel:    "gemini-2.0-flash",
		OllamaHost:     "http://localhost:11434",
		OllamaModel:    "llama3.2",
		Tags:           []string{"World", "USA"},
		WordStopIndex:  DefaultWordStopIndex,
		BannedSources:  []string{},
	}
}

// effectiveKey lets a user's own saved key override a shared default the
// server operator may provide via environment variable; if the user
// hasn't set one, the env var (if any) is used instead.
func effectiveKey(userKey, envVar string) string {
	if v := strings.TrimSpace(userKey); v != "" {
		return v
	}
	return strings.TrimSpace(os.Getenv(envVar))
}

func hasEnvDefault(userKey, envVar string) bool {
	return strings.TrimSpace(userKey) == "" && strings.TrimSpace(os.Getenv(envVar)) != ""
}

func (s Settings) EffectiveAnthropicKey() string {
	return effectiveKey(s.AnthropicKey, "ANTHROPIC_API_KEY")
}
func (s Settings) EffectiveOpenAIKey() string { return effectiveKey(s.OpenAIKey, "OPENAI_API_KEY") }
func (s Settings) EffectiveGeminiKey() string { return effectiveKey(s.GeminiKey, "GEMINI_API_KEY") }

func (s Settings) EffectiveAnthropicModel() string {
	if v := strings.TrimSpace(os.Getenv("ANTHROPIC_MODEL")); v != "" {
		return v
	}
	return s.AnthropicModel
}

func (s Settings) AnthropicKeyHasEnvDefault() bool {
	return hasEnvDefault(s.AnthropicKey, "ANTHROPIC_API_KEY")
}
func (s Settings) OpenAIKeyHasEnvDefault() bool { return hasEnvDefault(s.OpenAIKey, "OPENAI_API_KEY") }
func (s Settings) GeminiKeyHasEnvDefault() bool { return hasEnvDefault(s.GeminiKey, "GEMINI_API_KEY") }

// Update carries partial changes; nil fields are left untouched.
type Update struct {
	Provider       *Provider
	AnthropicKey   *string
	AnthropicModel *string
	OpenAIKey      *string
	OpenAIModel    *string
	GeminiKey      *string
	GeminiModel    *string
	OllamaHost     *string
	OllamaModel    *string
	Tags           *[]string
	WordStopIndex  *int
	BannedSources  *[]string
}

func applyUpdate(s *Settings, u Update) {
	if u.Provider != nil && isValidProvider(*u.Provider) {
		s.Provider = *u.Provider
	}
	if u.AnthropicKey != nil {
		s.AnthropicKey = strings.TrimSpace(*u.AnthropicKey)
	}
	if u.AnthropicModel != nil && strings.TrimSpace(*u.AnthropicModel) != "" {
		s.AnthropicModel = strings.TrimSpace(*u.AnthropicModel)
	}
	if u.OpenAIKey != nil {
		s.OpenAIKey = strings.TrimSpace(*u.OpenAIKey)
	}
	if u.OpenAIModel != nil && strings.TrimSpace(*u.OpenAIModel) != "" {
		s.OpenAIModel = strings.TrimSpace(*u.OpenAIModel)
	}
	if u.GeminiKey != nil {
		s.GeminiKey = strings.TrimSpace(*u.GeminiKey)
	}
	if u.GeminiModel != nil && strings.TrimSpace(*u.GeminiModel) != "" {
		s.GeminiModel = strings.TrimSpace(*u.GeminiModel)
	}
	if u.OllamaHost != nil && strings.TrimSpace(*u.OllamaHost) != "" {
		s.OllamaHost = strings.TrimSpace(*u.OllamaHost)
	}
	if u.OllamaModel != nil && strings.TrimSpace(*u.OllamaModel) != "" {
		s.OllamaModel = strings.TrimSpace(*u.OllamaModel)
	}
	if u.Tags != nil {
		s.Tags = *u.Tags
	}
	if u.WordStopIndex != nil {
		s.WordStopIndex = *u.WordStopIndex
	}
	if u.BannedSources != nil {
		s.BannedSources = *u.BannedSources
	}
}

// User is one registered account: a username, a bcrypt password hash
// (never the plaintext password), and that account's Settings.
type User struct {
	Username     string
	PasswordHash string
	Settings     Settings
}

var (
	ErrUserExists         = errors.New("that username is already taken")
	ErrUserNotFound       = errors.New("user not found")
	ErrInvalidUsername    = errors.New("username must be 3-32 characters: letters, numbers, underscore, or hyphen")
	ErrInvalidPassword    = errors.New("password must be at least 8 characters")
	ErrInvalidCredentials = errors.New("invalid username or password")
)

var usernameRE = regexp.MustCompile(`^[a-zA-Z0-9_-]{3,32}$`)

func validUsername(u string) bool { return usernameRE.MatchString(u) }

// dummyHash is compared against on a login attempt for a username that
// doesn't exist, so failing because "no such user" and failing because
// "wrong password" take about the same amount of time either way.
const dummyHash = "$2a$10$C6UzMDM.H6dfI/f8IDXNfePwHIftPYh3TqvcJ6nCz1uW7cUuAX9SC"
