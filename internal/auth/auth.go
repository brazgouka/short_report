// Package auth provides simple cookie-based session management for the
// multi-user account system. Sessions live only in memory: restarting the
// server logs everyone out, which is an acceptable tradeoff for a small
// self-hosted app and avoids persisting session tokens to disk.
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

const (
	cookieName = "short_report_session"
	sessionTTL = 30 * 24 * time.Hour
)

type session struct {
	username string
	expires  time.Time
}

// Manager issues, validates, and revokes login sessions.
type Manager struct {
	mu       sync.Mutex
	sessions map[string]session
}

// NewManager creates an empty session manager.
func NewManager() *Manager {
	return &Manager{sessions: make(map[string]session)}
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Create starts a new session for username and sets the session cookie
// on the response.
func (m *Manager) Create(w http.ResponseWriter, username string) error {
	token, err := newToken()
	if err != nil {
		return err
	}

	expires := time.Now().Add(sessionTTL)
	m.mu.Lock()
	m.sessions[token] = session{username: username, expires: expires}
	m.mu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  expires,
	})
	return nil
}

// Username returns the logged-in username for this request's session
// cookie, or "" if there isn't a valid one.
func (m *Manager) Username(r *http.Request) string {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return ""
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sessions[c.Value]
	if !ok {
		return ""
	}
	if time.Now().After(s.expires) {
		delete(m.sessions, c.Value)
		return ""
	}
	return s.username
}

// Destroy ends the session tied to this request's cookie, if any, and
// clears the cookie on the client.
func (m *Manager) Destroy(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		m.mu.Lock()
		delete(m.sessions, c.Value)
		m.mu.Unlock()
	}

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}
