package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMoveThreadWorkspaceUpdatesRolloutAndSQLite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sessionID := "019a61dd-9748-7743-9ce9-92b8663a935b"
	rolloutPath := filepath.Join(home, ".codex", "sessions", "2026", "05", "28", "rollout-"+sessionID+".jsonl")
	writeTestFile(t, rolloutPath, testSessionRolloutLine(sessionID, "/old/project", "Move me")+"\n{\"type\":\"user_message\"}\n")
	createTestThreadsTable(t, filepath.Join(home, ".codex", "state_5.sqlite"), sessionID, rolloutPath, "/old/project", "Move me")
	writeTestGlobalState(t, home, map[string]any{
		"projectless-thread-ids":      []any{sessionID, "keep-me"},
		"thread-workspace-root-hints": map[string]any{sessionID: "/old/project", "keep-me": "/keep"},
	})

	result := handleSessionDataRoute("/move-thread-workspace", map[string]any{"session_id": "local:" + sessionID, "target_cwd": "/new/project"})

	if result["status"] != "moved" {
		t.Fatalf("move should succeed: %#v", result)
	}
	data, _ := os.ReadFile(rolloutPath)
	firstLine, _ := splitFirstLine(string(data))
	var record map[string]any
	if err := json.Unmarshal([]byte(firstLine), &record); err != nil {
		t.Fatalf("rollout first line should stay json: %v", err)
	}
	payload := record["payload"].(map[string]any)
	if got := stringFromAny(payload["cwd"]); got != "/new/project" {
		t.Fatalf("rollout cwd mismatch: %q", got)
	}
	if got := testThreadCWD(t, filepath.Join(home, ".codex", "state_5.sqlite"), sessionID); got != "/new/project" {
		t.Fatalf("sqlite cwd mismatch: %q", got)
	}
	state := readTestGlobalState(t, home)
	if containsAnyString(state["projectless-thread-ids"], sessionID) {
		t.Fatalf("projectless ids should remove moved session: %#v", state["projectless-thread-ids"])
	}
	hints := state["thread-workspace-root-hints"].(map[string]any)
	if got := stringFromAny(hints[sessionID]); got != "/new/project" {
		t.Fatalf("workspace hint mismatch: %q", got)
	}
	if got := stringFromAny(hints["keep-me"]); got != "/keep" {
		t.Fatalf("unrelated workspace hint should remain: %q", got)
	}
}

func TestMoveThreadProjectlessUpdatesGlobalState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sessionID := "019a61dd-9748-7743-9ce9-92b8663a935b"
	rolloutPath := filepath.Join(home, ".codex", "sessions", "2026", "05", "28", "rollout-"+sessionID+".jsonl")
	writeTestFile(t, rolloutPath, testSessionRolloutLine(sessionID, "/project", "Move to chats")+"\n")
	createTestThreadsTable(t, filepath.Join(home, ".codex", "state_5.sqlite"), sessionID, rolloutPath, "/project", "Move to chats")
	writeTestGlobalState(t, home, map[string]any{
		"projectless-thread-ids":      []any{"keep-me"},
		"thread-workspace-root-hints": map[string]any{sessionID: "/project", "keep-me": "/keep"},
	})

	result := handleSessionDataRoute("/move-thread-projectless", map[string]any{"session_id": "local:" + sessionID})

	if result["status"] != "moved" {
		t.Fatalf("projectless move should succeed: %#v", result)
	}
	state := readTestGlobalState(t, home)
	if !containsAnyString(state["projectless-thread-ids"], sessionID) {
		t.Fatalf("projectless ids should include moved session: %#v", state["projectless-thread-ids"])
	}
	hints := state["thread-workspace-root-hints"].(map[string]any)
	if _, ok := hints[sessionID]; ok {
		t.Fatalf("workspace hint should be removed for projectless session: %#v", hints)
	}
	if got := stringFromAny(hints["keep-me"]); got != "/keep" {
		t.Fatalf("unrelated workspace hint should remain: %q", got)
	}
}

func TestExportMarkdownFromRollout(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sessionID := "019a61dd-9748-7743-9ce9-92b8663a935b"
	rolloutPath := filepath.Join(home, ".codex", "sessions", "2026", "05", "28", "rollout-"+sessionID+".jsonl")
	lines := []string{
		testSessionRolloutLine(sessionID, "/project", "Export Me"),
		testRolloutResponseMessage("user", "请总结这个项目"),
		testRolloutResponseMessage("assistant", "项目已经整理完成。"),
		`{"type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":"secret tool output"}}`,
		`{"type":"response_item","payload":{"type":"reasoning","encrypted_content":"secret reasoning"}}`,
	}
	writeTestFile(t, rolloutPath, strings.Join(lines, "\n")+"\n")
	createTestThreadsTable(t, filepath.Join(home, ".codex", "state_5.sqlite"), sessionID, rolloutPath, "/project", "Export Me")

	result := handleSessionDataRoute("/export-markdown", map[string]any{"session_id": sessionID})

	if result["status"] != "exported" {
		t.Fatalf("export should succeed: %#v", result)
	}
	if filename := stringFromAny(result["filename"]); !strings.HasSuffix(filename, ".md") {
		t.Fatalf("filename should be markdown: %q", filename)
	}
	markdown := stringFromAny(result["markdown"])
	for _, expected := range []string{"# Export Me", "Session ID", "## User", "请总结这个项目", "## Assistant", "项目已经整理完成。"} {
		if !strings.Contains(markdown, expected) {
			t.Fatalf("markdown missing %q:\n%s", expected, markdown)
		}
	}
	for _, unexpected := range []string{"secret tool output", "secret reasoning"} {
		if strings.Contains(markdown, unexpected) {
			t.Fatalf("markdown should not include %q:\n%s", unexpected, markdown)
		}
	}
}

func TestDeleteThreadAndUndoRestoresRolloutAndSQLite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sessionID := "019a61dd-9748-7743-9ce9-92b8663a935b"
	dbPath := filepath.Join(home, ".codex", "state_5.sqlite")
	rolloutPath := filepath.Join(home, ".codex", "sessions", "2026", "05", "28", "rollout-"+sessionID+".jsonl")
	contents := testSessionRolloutLine(sessionID, "/project", "Delete me") + "\n{\"type\":\"user_message\"}\n"
	writeTestFile(t, rolloutPath, contents)
	createTestThreadsTable(t, dbPath, sessionID, rolloutPath, "/project", "Delete me")

	deleted := handleSessionDataRoute("/delete", map[string]any{"session_id": sessionID, "title": "Delete me"})

	if deleted["status"] != "local_deleted" {
		t.Fatalf("delete should succeed: %#v", deleted)
	}
	if fileExists(rolloutPath) {
		t.Fatal("rollout file should be removed after delete")
	}
	if count := testThreadCount(t, dbPath, sessionID); count != 0 {
		t.Fatalf("sqlite row should be removed, count=%d", count)
	}
	token := stringFromAny(deleted["undo_token"])
	if token == "" {
		t.Fatal("delete should return undo token")
	}

	restored := handleSessionDataRoute("/undo", map[string]any{"undo_token": token})

	if restored["status"] != "ok" {
		t.Fatalf("undo should succeed: %#v", restored)
	}
	restoredData, err := os.ReadFile(rolloutPath)
	if err != nil {
		t.Fatalf("rollout file should be restored: %v", err)
	}
	if string(restoredData) != contents {
		t.Fatalf("restored rollout mismatch:\n%s", string(restoredData))
	}
	if count := testThreadCount(t, dbPath, sessionID); count != 1 {
		t.Fatalf("sqlite row should be restored, count=%d", count)
	}
}

func testSessionRolloutLine(sessionID, cwd, title string) string {
	data, _ := json.Marshal(map[string]any{
		"type": "session_meta",
		"payload": map[string]any{
			"id":             sessionID,
			"cwd":            cwd,
			"title":          title,
			"model_provider": "CodexPlusPlus",
			"timestamp":      "2026-05-28T10:00:00Z",
		},
		"timestamp": "2026-05-28T10:00:00Z",
	})
	return string(data)
}

func testRolloutResponseMessage(role, text string) string {
	data, _ := json.Marshal(map[string]any{
		"type":      "response_item",
		"timestamp": "2026-05-28T10:01:00Z",
		"payload": map[string]any{
			"type": "message",
			"role": role,
			"content": []any{
				map[string]any{"type": "output_text", "text": text},
			},
		},
	})
	return string(data)
}

func createTestThreadsTable(t *testing.T, dbPath, sessionID, rolloutPath, cwd, title string) {
	t.Helper()
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to open test sqlite db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE threads (
		id TEXT PRIMARY KEY,
		rollout_path TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		model_provider TEXT NOT NULL,
		cwd TEXT NOT NULL,
		title TEXT NOT NULL,
		archived INTEGER NOT NULL DEFAULT 0,
		created_at_ms INTEGER,
		updated_at_ms INTEGER
	)`); err != nil {
		t.Fatalf("failed to create threads table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO threads (id, rollout_path, created_at, updated_at, model_provider, cwd, title, archived, created_at_ms, updated_at_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, ?)`, sessionID, rolloutPath, 1779962400, 1779962500, "CodexPlusPlus", cwd, title, 1779962400000, 1779962500000); err != nil {
		t.Fatalf("failed to insert thread row: %v", err)
	}
}

func writeTestGlobalState(t *testing.T, home string, state map[string]any) {
	t.Helper()
	if err := atomicWriteJSON(filepath.Join(home, ".codex", ".codex-global-state.json"), state); err != nil {
		t.Fatalf("failed to write global state: %v", err)
	}
}

func readTestGlobalState(t *testing.T, home string) map[string]any {
	t.Helper()
	var state map[string]any
	if err := readJSON(filepath.Join(home, ".codex", ".codex-global-state.json"), &state); err != nil {
		t.Fatalf("failed to read global state: %v", err)
	}
	return state
}

func containsAnyString(value any, expected string) bool {
	for _, item := range value.([]any) {
		if stringFromAny(item) == expected {
			return true
		}
	}
	return false
}

func testThreadCWD(t *testing.T, dbPath, sessionID string) string {
	t.Helper()
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to open test sqlite db: %v", err)
	}
	defer db.Close()
	var cwd string
	if err := db.QueryRow(`SELECT cwd FROM threads WHERE id = ?`, sessionID).Scan(&cwd); err != nil {
		t.Fatalf("failed to read cwd: %v", err)
	}
	return cwd
}

func testThreadCount(t *testing.T, dbPath, sessionID string) int {
	t.Helper()
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to open test sqlite db: %v", err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM threads WHERE id = ?`, sessionID).Scan(&count); err != nil {
		t.Fatalf("failed to count thread rows: %v", err)
	}
	return count
}
