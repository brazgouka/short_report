// Package server wires accounts, config, news fetching, and LLM
// summarization together behind a small JSON API plus the embedded
// static frontend.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"shortreport/internal/auth"
	"shortreport/internal/config"
	"shortreport/internal/llm"
	"shortreport/internal/news"
)

// Bounds on user input so a huge or malformed request can't blow up the
// LLM prompt or hammer upstream feeds.
const (
	maxTags          = 12
	maxTagLen        = 40
	maxTotalItems    = 60
	maxBannedSources = 50
	maxBannedLen     = 100
)

type Server struct {
	users      *config.UserStore
	sessions   *auth.Manager
	cache      *news.Cache
	assets     http.Handler
	httpClient *http.Client
}

// New builds a Server. assets is the filesystem root containing
// index.html and friends (typically an embed.FS subtree).
func New(users *config.UserStore, sessions *auth.Manager, cache *news.Cache, assets fs.FS) *Server {
	return &Server{
		users:    users,
		sessions: sessions,
		cache:    cache,
		assets:   http.FileServer(http.FS(assets)),
		// 120s (not the 30s a hosted API would need) because local
		// Ollama inference is much slower — especially on modest
		// hardware, with a larger model, or on a cold start where
		// Ollama has to load the model into memory before it can
		// even begin generating.
		httpClient: &http.Client{Timeout: 120 * time.Second},
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/auth/register", s.handleRegister)
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /api/auth/me", s.handleMe)

	mux.HandleFunc("GET /api/config", s.requireAuth(s.handleGetConfig))
	mux.HandleFunc("POST /api/config", s.requireAuth(s.handleUpdateConfig))
	mux.HandleFunc("POST /api/aggregate", s.requireAuth(s.handleAggregate))
	mux.HandleFunc("GET /api/ollama/models", s.requireAuth(s.handleOllamaModels))

	mux.Handle("/", s.assets)
	return withLogging(mux)
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s (%s)", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}

// requireAuth wraps a handler so it only runs with a valid session,
// passing the authenticated username through.
func (s *Server) requireAuth(next func(w http.ResponseWriter, r *http.Request, username string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := s.sessions.Username(r)
		if username == "" {
			writeError(w, http.StatusUnauthorized, "not logged in")
			return
		}
		next(w, r, username)
	}
}

// --- /api/auth ------------------------------------------------------------

type authRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req authRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	u, err := s.users.Register(req.Username, req.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.sessions.Create(w, u.Username); err != nil {
		writeError(w, http.StatusInternalServerError, "could not start session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"username": u.Username})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req authRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	u, err := s.users.Authenticate(req.Username, req.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	if err := s.sessions.Create(w, u.Username); err != nil {
		writeError(w, http.StatusInternalServerError, "could not start session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"username": u.Username})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.sessions.Destroy(w, r)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	username := s.sessions.Username(r)
	if username == "" {
		writeError(w, http.StatusUnauthorized, "not logged in")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"username": username})
}

// --- /api/config ---------------------------------------------------------

type publicSettings struct {
	Username               string            `json:"username"`
	Provider               string            `json:"provider"`
	AnthropicKeyConfigured bool              `json:"anthropicKeyConfigured"`
	AnthropicKeyFromEnv    bool              `json:"anthropicKeyFromEnv"`
	AnthropicModel         string            `json:"anthropicModel"`
	OpenAIKeyConfigured    bool              `json:"openaiKeyConfigured"`
	OpenAIKeyFromEnv       bool              `json:"openaiKeyFromEnv"`
	OpenAIModel            string            `json:"openaiModel"`
	GeminiKeyConfigured    bool              `json:"geminiKeyConfigured"`
	GeminiKeyFromEnv       bool              `json:"geminiKeyFromEnv"`
	GeminiModel            string            `json:"geminiModel"`
	OllamaHost             string            `json:"ollamaHost"`
	OllamaModel            string            `json:"ollamaModel"`
	Tags                   []string          `json:"tags"`
	AvailableTags          []string          `json:"availableTags"`
	WordStopIndex          int               `json:"wordStopIndex"`
	WordStops              []config.WordStop `json:"wordStops"`
	BannedSources          []string          `json:"bannedSources"`
}

func publicSettingsFor(username string, set config.Settings) publicSettings {
	return publicSettings{
		Username:               username,
		Provider:               string(set.Provider),
		AnthropicKeyConfigured: strings.TrimSpace(set.AnthropicKey) != "",
		AnthropicKeyFromEnv:    set.AnthropicKeyHasEnvDefault(),
		AnthropicModel:         set.AnthropicModel,
		OpenAIKeyConfigured:    strings.TrimSpace(set.OpenAIKey) != "",
		OpenAIKeyFromEnv:       set.OpenAIKeyHasEnvDefault(),
		OpenAIModel:            set.OpenAIModel,
		GeminiKeyConfigured:    strings.TrimSpace(set.GeminiKey) != "",
		GeminiKeyFromEnv:       set.GeminiKeyHasEnvDefault(),
		GeminiModel:            set.GeminiModel,
		OllamaHost:             set.OllamaHost,
		OllamaModel:            set.OllamaModel,
		Tags:                   set.Tags,
		AvailableTags:          config.AvailableTags,
		WordStopIndex:          set.WordStopIndex,
		WordStops:              config.WordStops,
		BannedSources:          set.BannedSources,
	}
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request, username string) {
	set, err := s.users.GetSettings(username)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load settings")
		return
	}
	writeJSON(w, http.StatusOK, publicSettingsFor(username, set))
}

type configUpdateRequest struct {
	Provider       *string   `json:"provider"`
	AnthropicKey   *string   `json:"anthropicKey"`
	AnthropicModel *string   `json:"anthropicModel"`
	OpenAIKey      *string   `json:"openaiKey"`
	OpenAIModel    *string   `json:"openaiModel"`
	GeminiKey      *string   `json:"geminiKey"`
	GeminiModel    *string   `json:"geminiModel"`
	OllamaHost     *string   `json:"ollamaHost"`
	OllamaModel    *string   `json:"ollamaModel"`
	Tags           *[]string `json:"tags"`
	WordStopIndex  *int      `json:"wordStopIndex"`
	BannedSources  *[]string `json:"bannedSources"`
}

func (s *Server) handleUpdateConfig(w http.ResponseWriter, r *http.Request, username string) {
	var req configUpdateRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	update := config.Update{
		AnthropicKey:   req.AnthropicKey,
		AnthropicModel: req.AnthropicModel,
		OpenAIKey:      req.OpenAIKey,
		OpenAIModel:    req.OpenAIModel,
		GeminiKey:      req.GeminiKey,
		GeminiModel:    req.GeminiModel,
		OllamaHost:     req.OllamaHost,
		OllamaModel:    req.OllamaModel,
		WordStopIndex:  req.WordStopIndex,
	}
	if req.Provider != nil {
		p := config.Provider(*req.Provider)
		update.Provider = &p
	}
	if req.Tags != nil {
		tags := sanitizeTags(*req.Tags)
		update.Tags = &tags
	}
	if req.BannedSources != nil {
		banned := sanitizeBannedSources(*req.BannedSources)
		update.BannedSources = &banned
	}

	set, err := s.users.ApplySettings(username, update)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save settings")
		return
	}
	writeJSON(w, http.StatusOK, publicSettingsFor(username, set))
}

type ollamaModelsResponse struct {
	Models []string `json:"models"`
	Error  string   `json:"error,omitempty"`
}

// handleOllamaModels lists models a local Ollama server has pulled, so the
// UI can offer a picker instead of free-text entry. It always answers 200:
// an unreachable server is a normal, expected state, not a server error.
func (s *Server) handleOllamaModels(w http.ResponseWriter, r *http.Request, username string) {
	host := strings.TrimSpace(r.URL.Query().Get("host"))
	if host == "" {
		if set, err := s.users.GetSettings(username); err == nil {
			host = set.OllamaHost
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	models, err := llm.ListModels(ctx, s.httpClient, host)
	if err != nil {
		writeJSON(w, http.StatusOK, ollamaModelsResponse{Models: []string{}, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, ollamaModelsResponse{Models: models})
}

// --- /api/aggregate --------------------------------------------------------

type aggregateRequest struct {
	Tags      []string `json:"tags"`
	StopIndex int      `json:"stopIndex"`
}

type aggregateResponse struct {
	Items       []responseItem `json:"items,omitempty"`
	Unlimited   bool           `json:"unlimited"`
	Articles    []news.Article `json:"articles,omitempty"`
	Provider    string         `json:"provider"`
	Model       string         `json:"model,omitempty"`
	StopIndex   int            `json:"stopIndex"`
	GeneratedAt time.Time      `json:"generatedAt"`
	Warning     string         `json:"warning,omitempty"`
	FailedTags  []string       `json:"failedTags,omitempty"`
}

func (s *Server) handleAggregate(w http.ResponseWriter, r *http.Request, username string) {
	var req aggregateRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	tags := sanitizeTags(req.Tags)
	if len(tags) == 0 {
		writeError(w, http.StatusBadRequest, "select at least one tag")
		return
	}
	if req.StopIndex < 0 || req.StopIndex >= len(config.WordStops) {
		writeError(w, http.StatusBadRequest, "invalid stopIndex")
		return
	}
	stop := config.WordStops[req.StopIndex]

	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	settings, err := s.users.GetSettings(username)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load settings")
		return
	}

	articles, failed := s.cache.Get(ctx, tags)
	articles = filterBannedSources(articles, settings.BannedSources)
	if len(articles) > maxTotalItems {
		articles = articles[:maxTotalItems]
	}

	// Remember the user's last choices so a restart resumes where they left off.
	stopIndex := req.StopIndex
	settings, err = s.users.ApplySettings(username, config.Update{
		Tags:          &tags,
		WordStopIndex: &stopIndex,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save preferences")
		return
	}

	resp := aggregateResponse{
		Provider:    string(settings.Provider),
		StopIndex:   stopIndex,
		GeneratedAt: time.Now(),
		FailedTags:  failed,
	}

	if len(articles) == 0 {
		resp.Warning = "No articles were found for the selected tags right now — try again shortly."
		writeJSON(w, http.StatusOK, resp)
		return
	}

	// The "∞" stop means "no limit": show everything raw, no LLM call needed.
	if stop.Infinite {
		resp.Unlimited = true
		resp.Articles = articles
		writeJSON(w, http.StatusOK, resp)
		return
	}

	provider, err := s.providerFor(settings)
	if err != nil {
		resp.Warning = err.Error() + " — showing raw headlines instead. Open Settings to add a provider."
		resp.Unlimited = true
		resp.Articles = articles
		writeJSON(w, http.StatusOK, resp)
		return
	}
	resp.Model = provider.Model()

	items, err := provider.Summarize(ctx, llm.Request{
		Date:          time.Now().Format("Monday, January 2, 2006"),
		Tags:          tags,
		WordTarget:    stop.Min,
		WordTargetMax: stop.Max,
		MaxItems:      stop.MaxItems,
		Articles:      formatArticlesForPrompt(articles),
	})
	if err != nil {
		resp.Warning = "AI aggregation failed (" + err.Error() + ") — showing raw headlines instead."
		resp.Unlimited = true
		resp.Articles = articles
		writeJSON(w, http.StatusOK, resp)
		return
	}

	// Enforce the item cap server-side too — models, especially small
	// local ones, don't always respect it on their own.
	if len(items) > stop.MaxItems {
		items = items[:stop.MaxItems]
	}

	resp.Items = resolveBriefingItems(items, articles)
	if len(resp.Items) == 0 {
		resp.Warning = "AI response was empty — showing raw headlines instead."
		resp.Unlimited = true
		resp.Articles = articles
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) providerFor(settings config.Settings) (llm.Provider, error) {
	switch settings.Provider {
	case config.ProviderOllama:
		if strings.TrimSpace(settings.OllamaModel) == "" {
			return nil, errors.New("no Ollama model configured")
		}
		return &llm.OllamaProvider{Host: settings.OllamaHost, ModelName: settings.OllamaModel, Client: s.httpClient}, nil
	case config.ProviderOpenAI:
		key := settings.EffectiveOpenAIKey()
		if key == "" {
			return nil, errors.New("no OpenAI API key configured")
		}
		return &llm.OpenAIProvider{APIKey: key, ModelName: settings.OpenAIModel, Client: s.httpClient}, nil
	case config.ProviderGemini:
		key := settings.EffectiveGeminiKey()
		if key == "" {
			return nil, errors.New("no Gemini API key configured")
		}
		return &llm.GeminiProvider{APIKey: key, ModelName: settings.GeminiModel, Client: s.httpClient}, nil
	default:
		key := settings.EffectiveAnthropicKey()
		if key == "" {
			return nil, errors.New("no Anthropic API key configured")
		}
		return &llm.AnthropicProvider{APIKey: key, ModelName: settings.EffectiveAnthropicModel(), Client: s.httpClient}, nil
	}
}

// --- helpers ---------------------------------------------------------------

func sanitizeTags(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, t := range in {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if len(t) > maxTagLen {
			t = t[:maxTagLen]
		}
		key := strings.ToLower(t)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, t)
		if len(out) >= maxTags {
			break
		}
	}
	return out
}

func sanitizeBannedSources(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.ToLower(strings.TrimSpace(s))
		s = strings.TrimPrefix(s, "https://")
		s = strings.TrimPrefix(s, "http://")
		s = strings.TrimPrefix(s, "www.")
		s = strings.TrimSuffix(s, "/")
		if s == "" || len(s) > maxBannedLen {
			continue
		}
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
		if len(out) >= maxBannedSources {
			break
		}
	}
	return out
}

func hostOf(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}

// isBanned checks the host itself and each of its parent domains, so
// banning "bbc.co.uk" also covers "feeds.bbc.co.uk".
func isBanned(host string, banned map[string]bool) bool {
	if host == "" {
		return false
	}
	parts := strings.Split(host, ".")
	for i := 0; i < len(parts)-1; i++ {
		if banned[strings.Join(parts[i:], ".")] {
			return true
		}
	}
	return false
}

func filterBannedSources(articles []news.Article, banned []string) []news.Article {
	if len(banned) == 0 {
		return articles
	}
	bannedSet := make(map[string]bool, len(banned))
	for _, b := range banned {
		bannedSet[b] = true
	}
	kept := make([]news.Article, 0, len(articles))
	for _, a := range articles {
		if isBanned(hostOf(a.Link), bannedSet) {
			continue
		}
		kept = append(kept, a)
	}
	return kept
}

// formatArticlesForPrompt numbers articles globally (1-based) so the LLM
// can cite "source 3" etc.; that number is later resolved back to the real
// news.Article at the same index in the articles slice passed in.
func formatArticlesForPrompt(articles []news.Article) string {
	var b strings.Builder
	for i, a := range articles {
		desc := truncateRunes(a.Description, 220)
		fmt.Fprintf(&b, "%d. [%s] %s — %s (%s)\n", i+1, a.Tag, a.Title, desc, sourceOrHost(a))
	}
	return b.String()
}

type responseSource struct {
	Title string `json:"title"`
	Link  string `json:"link"`
}

type responseItem struct {
	Headline string           `json:"headline"`
	Detail   string           `json:"detail"`
	Tag      string           `json:"tag,omitempty"`
	Sources  []responseSource `json:"sources,omitempty"`
}

// resolveBriefingItems maps each item's LLM-supplied 1-based source numbers
// back to the real fetched articles, so links shown to the user always
// come from data we fetched ourselves — never from something the model
// might have invented.
func resolveBriefingItems(items []llm.BriefingItem, articles []news.Article) []responseItem {
	out := make([]responseItem, 0, len(items))
	for _, it := range items {
		headline := strings.TrimSpace(it.Headline)
		if headline == "" {
			continue
		}
		ri := responseItem{Headline: headline, Detail: strings.TrimSpace(it.Detail)}
		for _, n := range it.Sources {
			pos := n - 1
			if pos < 0 || pos >= len(articles) {
				continue
			}
			a := articles[pos]
			ri.Sources = append(ri.Sources, responseSource{Title: a.Title, Link: a.Link})
			if ri.Tag == "" {
				ri.Tag = a.Tag
			}
		}
		out = append(out, ri)
	}
	return out
}

func sourceOrHost(a news.Article) string {
	if strings.TrimSpace(a.Source) != "" {
		return a.Source
	}
	if u, err := url.Parse(a.Link); err == nil && u.Host != "" {
		return u.Host
	}
	return "unknown source"
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
