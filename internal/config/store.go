package config

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

// UserStore is a SQLite-backed account store: one row per user in
// `users`, their scalar preferences in `user_settings`, and their
// selected topics / blocked sources as real one-to-many rows in
// `user_tags` / `user_banned_sources`.
type UserStore struct {
	db *sql.DB
}

// NewUserStore opens (creating if needed) the SQLite database under the
// OS user-config directory and ensures its schema exists.
func NewUserStore() (*UserStore, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	appDir := filepath.Join(dir, "short-report")
	if err := os.MkdirAll(appDir, 0o700); err != nil {
		return nil, err
	}

	dbPath := filepath.Join(appDir, "shortreport.db")
	dsn := fmt.Sprintf(
		"file:%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)",
		dbPath,
	)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}
	// SQLite only ever allows one writer; avoiding a connection pool
	// sidesteps "database is locked" errors without needing retry logic,
	// and keeps pragmas (set via the DSN above) consistent across every
	// query instead of only whichever connection happened to run first.
	db.SetMaxOpenConns(1)

	st := &UserStore{db: db}
	if err := st.migrate(); err != nil {
		return nil, fmt.Errorf("migrating database: %w", err)
	}
	return st, nil
}

func (st *UserStore) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			username      TEXT PRIMARY KEY COLLATE NOCASE,
			password_hash TEXT NOT NULL,
			created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS user_settings (
			username        TEXT PRIMARY KEY COLLATE NOCASE REFERENCES users(username) ON DELETE CASCADE,
			provider        TEXT NOT NULL DEFAULT 'anthropic',
			anthropic_key   TEXT NOT NULL DEFAULT '',
			anthropic_model TEXT NOT NULL DEFAULT '',
			openai_key      TEXT NOT NULL DEFAULT '',
			openai_model    TEXT NOT NULL DEFAULT '',
			gemini_key      TEXT NOT NULL DEFAULT '',
			gemini_model    TEXT NOT NULL DEFAULT '',
			ollama_host     TEXT NOT NULL DEFAULT '',
			ollama_model    TEXT NOT NULL DEFAULT '',
			word_stop_index INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS user_tags (
			username TEXT NOT NULL COLLATE NOCASE REFERENCES users(username) ON DELETE CASCADE,
			tag      TEXT NOT NULL,
			position INTEGER NOT NULL,
			PRIMARY KEY (username, tag)
		)`,
		`CREATE TABLE IF NOT EXISTS user_banned_sources (
			username TEXT NOT NULL COLLATE NOCASE REFERENCES users(username) ON DELETE CASCADE,
			source   TEXT NOT NULL,
			PRIMARY KEY (username, source)
		)`,
	}
	for _, stmt := range stmts {
		if _, err := st.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// querier is satisfied by both *sql.DB and *sql.Tx, so the read helpers
// below work whether or not they're called inside a transaction.
type querier interface {
	QueryRow(query string, args ...any) *sql.Row
	Query(query string, args ...any) (*sql.Rows, error)
	Exec(query string, args ...any) (sql.Result, error)
}

// Register creates a new account with default settings.
func (st *UserStore) Register(username, password string) (*User, error) {
	username = strings.TrimSpace(username)
	if !validUsername(username) {
		return nil, ErrInvalidUsername
	}
	if len(password) < 8 {
		return nil, ErrInvalidPassword
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	tx, err := st.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`INSERT INTO users (username, password_hash) VALUES (?, ?)`, username, string(hash)); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrUserExists
		}
		return nil, err
	}

	d := defaultSettings()
	if err := writeSettings(tx, username, d); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &User{Username: username, PasswordHash: string(hash), Settings: d}, nil
}

// Authenticate verifies a username/password pair and returns the user.
func (st *UserStore) Authenticate(username, password string) (*User, error) {
	var storedUsername, hash string
	err := st.db.QueryRow(
		`SELECT username, password_hash FROM users WHERE username = ?`, username,
	).Scan(&storedUsername, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		_ = bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte(password))
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return nil, ErrInvalidCredentials
	}

	settings, err := readSettings(st.db, storedUsername)
	if err != nil {
		return nil, err
	}
	return &User{Username: storedUsername, PasswordHash: hash, Settings: settings}, nil
}

// GetSettings returns the given user's current settings.
func (st *UserStore) GetSettings(username string) (Settings, error) {
	return readSettings(st.db, username)
}

// ApplySettings merges an Update into the user's settings and persists it.
func (st *UserStore) ApplySettings(username string, upd Update) (Settings, error) {
	tx, err := st.db.Begin()
	if err != nil {
		return Settings{}, err
	}
	defer tx.Rollback()

	current, err := readSettings(tx, username)
	if err != nil {
		return Settings{}, err
	}

	applyUpdate(&current, upd)

	if err := writeSettings(tx, username, current); err != nil {
		return Settings{}, err
	}

	if err := tx.Commit(); err != nil {
		return Settings{}, err
	}
	return current, nil
}

func readSettings(q querier, username string) (Settings, error) {
	var s Settings
	var provider string
	err := q.QueryRow(`
		SELECT provider, anthropic_key, anthropic_model, openai_key, openai_model,
		       gemini_key, gemini_model, ollama_host, ollama_model, word_stop_index
		FROM user_settings WHERE username = ?`, username,
	).Scan(&provider, &s.AnthropicKey, &s.AnthropicModel, &s.OpenAIKey, &s.OpenAIModel,
		&s.GeminiKey, &s.GeminiModel, &s.OllamaHost, &s.OllamaModel, &s.WordStopIndex)
	if errors.Is(err, sql.ErrNoRows) {
		return Settings{}, ErrUserNotFound
	}
	if err != nil {
		return Settings{}, err
	}
	s.Provider = Provider(provider)

	tags, err := readList(q, `SELECT tag FROM user_tags WHERE username = ? ORDER BY position`, username)
	if err != nil {
		return Settings{}, err
	}
	s.Tags = tags

	banned, err := readList(q, `SELECT source FROM user_banned_sources WHERE username = ?`, username)
	if err != nil {
		return Settings{}, err
	}
	s.BannedSources = banned

	return s, nil
}

func readList(q querier, query, username string) ([]string, error) {
	rows, err := q.Query(query, username)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// writeSettings replaces the scalar settings row and, wholesale, the tag
// and banned-source lists for username. Called both on registration
// (fresh insert) and on every ApplySettings (upsert-by-replace).
func writeSettings(q querier, username string, s Settings) error {
	_, err := q.Exec(`
		INSERT INTO user_settings (
			username, provider, anthropic_key, anthropic_model, openai_key, openai_model,
			gemini_key, gemini_model, ollama_host, ollama_model, word_stop_index
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(username) DO UPDATE SET
			provider = excluded.provider,
			anthropic_key = excluded.anthropic_key,
			anthropic_model = excluded.anthropic_model,
			openai_key = excluded.openai_key,
			openai_model = excluded.openai_model,
			gemini_key = excluded.gemini_key,
			gemini_model = excluded.gemini_model,
			ollama_host = excluded.ollama_host,
			ollama_model = excluded.ollama_model,
			word_stop_index = excluded.word_stop_index`,
		username, string(s.Provider), s.AnthropicKey, s.AnthropicModel, s.OpenAIKey, s.OpenAIModel,
		s.GeminiKey, s.GeminiModel, s.OllamaHost, s.OllamaModel, s.WordStopIndex,
	)
	if err != nil {
		return err
	}

	if err := replaceList(q, "user_tags", "tag", username, s.Tags, true); err != nil {
		return err
	}
	if err := replaceList(q, "user_banned_sources", "source", username, s.BannedSources, false); err != nil {
		return err
	}
	return nil
}

func replaceList(q querier, table, column, username string, values []string, ordered bool) error {
	if _, err := q.Exec(fmt.Sprintf(`DELETE FROM %s WHERE username = ?`, table), username); err != nil {
		return err
	}
	for i, v := range values {
		var err error
		if ordered {
			err = execInsert(q, fmt.Sprintf(`INSERT INTO %s (username, %s, position) VALUES (?, ?, ?)`, table, column),
				username, v, i)
		} else {
			err = execInsert(q, fmt.Sprintf(`INSERT INTO %s (username, %s) VALUES (?, ?)`, table, column),
				username, v)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func execInsert(q querier, query string, args ...any) error {
	_, err := q.Exec(query, args...)
	return err
}
