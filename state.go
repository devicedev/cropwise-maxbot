package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type State struct {
	LastSync *time.Time        `json:"last_sync,omitempty"`
	Sent     map[string]string `json:"sent,omitempty"`

	db *sql.DB
}

func LoadState(path string) (State, error) {
	state := State{Sent: map[string]string{}}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return state, nil
		}
		return State{}, err
	}
	if len(b) == 0 {
		return state, nil
	}
	if err := json.Unmarshal(b, &state); err != nil {
		return State{}, err
	}
	if state.Sent == nil {
		state.Sent = map[string]string{}
	}
	return state, nil
}

func LoadStateDB(path string) (State, error) {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return State{}, err
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return State{}, err
	}

	state := State{Sent: map[string]string{}, db: db}
	if err := state.initSQLite(); err != nil {
		_ = db.Close()
		return State{}, err
	}
	if err := state.loadSQLite(); err != nil {
		_ = db.Close()
		return State{}, err
	}
	return state, nil
}

func (s *State) LastSyncOr(def time.Time) time.Time {
	if s.LastSync == nil || s.LastSync.IsZero() {
		return def
	}
	return *s.LastSync
}

func (s *State) SetLastSync(t time.Time) {
	utc := t.UTC()
	s.LastSync = &utc
}

func (s *State) IsSent(id, updatedAt string) bool {
	if s.Sent == nil || id == "" {
		return false
	}
	return s.Sent[id] == updatedAt
}

func (s *State) MarkSent(id, updatedAt string) {
	if s.Sent == nil {
		s.Sent = map[string]string{}
	}
	if id != "" {
		s.Sent[id] = updatedAt
	}
}

func (s *State) Save(path string) error {
	if s.db != nil {
		return s.saveSQLite()
	}
	if s.Sent == nil {
		s.Sent = map[string]string{}
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

func (s *State) Close() error {
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}

func (s *State) initSQLite() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS state_meta (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS sent_reports (
	report_id TEXT PRIMARY KEY,
	updated_at TEXT NOT NULL
);`)
	return err
}

func (s *State) loadSQLite() error {
	var rawLastSync string
	err := s.db.QueryRow(`SELECT value FROM state_meta WHERE key = 'last_sync'`).Scan(&rawLastSync)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if rawLastSync != "" {
		t, err := time.Parse(time.RFC3339Nano, rawLastSync)
		if err != nil {
			return err
		}
		s.SetLastSync(t)
	}

	rows, err := s.db.Query(`SELECT report_id, updated_at FROM sent_reports`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var id, updatedAt string
		if err := rows.Scan(&id, &updatedAt); err != nil {
			return err
		}
		s.Sent[id] = updatedAt
	}
	return rows.Err()
}

func (s *State) saveSQLite() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if s.LastSync != nil && !s.LastSync.IsZero() {
		_, err = tx.Exec(
			`INSERT INTO state_meta(key, value) VALUES('last_sync', ?)
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
			s.LastSync.UTC().Format(time.RFC3339Nano),
		)
		if err != nil {
			return err
		}
	}

	for id, updatedAt := range s.Sent {
		_, err = tx.Exec(
			`INSERT INTO sent_reports(report_id, updated_at) VALUES(?, ?)
			 ON CONFLICT(report_id) DO UPDATE SET updated_at = excluded.updated_at`,
			id,
			updatedAt,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}
