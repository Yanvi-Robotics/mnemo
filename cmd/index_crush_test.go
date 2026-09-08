package cmd

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pilan-AI/mnemo/internal/db"
)

// TestIndexCrush_TimestampsAreUnixSeconds pins that crush messages.created_at
// is Unix SECONDS, not milliseconds.
//
// Crush's initial migration comment claims milliseconds, but the stored
// values are seconds: the update_sessions_updated_at trigger writes
// strftime('%s','now') and the Crush CLI renders time.Unix(CreatedAt, 0)
// (see charmbracelet/crush internal/db/migrations). 1784269138 is a real
// captured value: 2026-07-17 as seconds, but 1970-01-21 if misread as
// milliseconds — the bug this test guards against (messages silently
// indexed into January 1970).
func TestIndexCrush_TimestampsAreUnixSeconds(t *testing.T) {
	// Redirect HOME/USERPROFILE so InitDB creates ~/.mnemo inside the
	// test sandbox instead of touching the developer's real index.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	if err := db.InitDB(); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer db.CloseDB()

	crushPath := filepath.Join(t.TempDir(), "crush.db")
	fixture, err := sql.Open("sqlite", crushPath)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer fixture.Close()

	schema := `
		CREATE TABLE sessions (
			id TEXT PRIMARY KEY,
			title TEXT,
			prompt_tokens INTEGER DEFAULT 0,
			completion_tokens INTEGER DEFAULT 0,
			cost REAL DEFAULT 0
		);
		CREATE TABLE messages (
			id TEXT PRIMARY KEY,
			session_id TEXT,
			role TEXT,
			parts TEXT,
			created_at INTEGER,
			model TEXT,
			provider TEXT
		);`
	if _, err := fixture.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	parts := `[{"type":"text","data":{"text":"hello crush"}}]`
	if _, err := fixture.Exec(
		`INSERT INTO sessions (id, title, prompt_tokens, completion_tokens, cost) VALUES ('sess-1', 'demo', 10, 20, 0.1)`,
	); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if _, err := fixture.Exec(
		`INSERT INTO messages (id, session_id, role, parts, created_at) VALUES ('msg-1', 'sess-1', 'user', ?, ?)`,
		parts, int64(1784269138),
	); err != nil {
		t.Fatalf("seed message: %v", err)
	}

	sessions, messages := indexCrush(crushPath)
	if sessions != 1 || messages != 1 {
		t.Fatalf("indexCrush = (%d sessions, %d messages), want (1, 1)", sessions, messages)
	}

	var stored string
	row := db.GetDB().QueryRow(`SELECT timestamp FROM messages WHERE session_id = 'sess-1' LIMIT 1`)
	if err := row.Scan(&stored); err != nil {
		t.Fatalf("read indexed timestamp: %v", err)
	}
	if !strings.Contains(stored, "2026") {
		t.Errorf("indexed timestamp = %q, want a 2026 date (1784269138 is 2026-07-17 as Unix seconds); a 1970 date means created_at was misread as milliseconds", stored)
	}
}
