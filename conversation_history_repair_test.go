package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRepairConversationHistoryJSONLineOnlyRemovesResponseItemPayloadNamespace(t *testing.T) {
	line := []byte("{\"type\":\"response_item\",\"namespace\":\"outer\",\"payload\":{\"type\":\"custom_tool_call\",\"namespace\":\"mcp__browser\",\"input\":{\"namespace\":\"nested\"},\"large\":9007199254740993}}\r\n")
	want := []byte("{\"type\":\"response_item\",\"namespace\":\"outer\",\"payload\":{\"type\":\"custom_tool_call\",\"input\":{\"namespace\":\"nested\"},\"large\":9007199254740993}}\r\n")

	got, changed, err := repairConversationHistoryJSONLine(line)
	if err != nil {
		t.Fatalf("repair line failed: %v", err)
	}
	if !changed {
		t.Fatal("response_item payload namespace should be removed")
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("repair should preserve every other raw JSON value:\n got: %s\nwant: %s", got, want)
	}

	nonResponseItem := []byte(`{"type":"event_msg","payload":{"namespace":"keep"}}`)
	got, changed, err = repairConversationHistoryJSONLine(nonResponseItem)
	if err != nil || changed || !bytes.Equal(got, nonResponseItem) {
		t.Fatalf("non-response_item record must stay unchanged: changed=%v err=%v got=%s", changed, err, got)
	}

	nestedOnly := []byte(`{"type":"response_item","payload":{"type":"custom_tool_call","input":{"namespace":"keep"}}}`)
	got, changed, err = repairConversationHistoryJSONLine(nestedOnly)
	if err != nil || changed || !bytes.Equal(got, nestedOnly) {
		t.Fatalf("nested namespace must stay unchanged: changed=%v err=%v got=%s", changed, err, got)
	}

	messageItem := []byte(`{"type":"response_item","payload":{"type":"message","namespace":"keep-message","content":[]}}`)
	got, changed, err = repairConversationHistoryJSONLine(messageItem)
	if err != nil || changed || !bytes.Equal(got, messageItem) {
		t.Fatalf("non-tool response_item payload must stay unchanged: changed=%v err=%v got=%s", changed, err, got)
	}
}

func TestRepairConversationHistoryJSONLineRemovesDuplicatePayloadNamespaceKeys(t *testing.T) {
	line := []byte(`{"type":"response_item","payload":{ "namespace":"one", "namespace":"two", "type":"custom_tool_call", "content":[{"namespace":"nested"}] }}`)
	updated, changed, err := repairConversationHistoryJSONLine(line)
	if err != nil {
		t.Fatalf("repair line failed: %v", err)
	}
	if !changed {
		t.Fatal("duplicate namespace keys should be removed")
	}
	var record map[string]json.RawMessage
	if err := json.Unmarshal(updated, &record); err != nil {
		t.Fatalf("updated line is not valid JSON: %v\n%s", err, updated)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(record["payload"], &payload); err != nil {
		t.Fatalf("updated payload is not an object: %v", err)
	}
	if _, exists := payload["namespace"]; exists {
		t.Fatalf("top-level payload namespace remains: %s", updated)
	}
	if !bytes.Contains(updated, []byte(`{"namespace":"nested"}`)) {
		t.Fatalf("nested namespace should remain: %s", updated)
	}
}

func TestRepairConversationHistoryNamespacesBacksUpBothSessionTreesAndIsIdempotent(t *testing.T) {
	allowConversationHistoryRepairProcessesForTest(t)
	home := t.TempDir()
	sessionPath := filepath.Join(home, "sessions", "2026", "07", "rollout-active.jsonl")
	archivedPath := filepath.Join(home, "archived_sessions", "2026", "06", "rollout-archived.jsonl")
	unchangedPath := filepath.Join(home, "sessions", "rollout-clean.jsonl")
	ignoredPath := filepath.Join(home, "sessions", "not-a-rollout.jsonl")
	for _, path := range []string{sessionPath, archivedPath, unchangedPath, ignoredPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir failed: %v", err)
		}
	}
	sessionOriginal := strings.Join([]string{
		`{"timestamp":"2026-07-10T00:00:00Z","type":"response_item","namespace":"outer","payload":{"type":"custom_tool_call","namespace":"mcp__browser","input":{"namespace":"nested"},"large":9007199254740993}}`,
		`{"type":"event_msg","payload":{"namespace":"keep-event"}}`,
		`{"type":"response_item","payload":{"input":{"namespace":"keep-nested"}}}`,
		`{"type":"response_item","payload":{"type":"message","namespace":"keep-message","content":[]}}`,
		`{"type":"session_meta","payload":{"dynamic_tools":[{"name":"tool","namespace":"keep-dynamic-tool"}]}}`,
		`{not-json}`,
	}, "\r\n") + "\r\n"
	archivedOriginal := `{"type":"response_item","payload":{ "namespace":"one", "namespace":"two", "type":"function_call", "arguments":{"namespace":"nested"} }}`
	unchangedOriginal := `{"type":"response_item","payload":{"type":"message","content":[]}}` + "\n"
	ignoredOriginal := `{"type":"response_item","payload":{"type":"custom_tool_call","namespace":"keep-ignored"}}` + "\n"
	if err := os.WriteFile(sessionPath, []byte(sessionOriginal), 0o600); err != nil {
		t.Fatalf("write session failed: %v", err)
	}
	if err := os.WriteFile(archivedPath, []byte(archivedOriginal), 0o640); err != nil {
		t.Fatalf("write archived session failed: %v", err)
	}
	if err := os.WriteFile(unchangedPath, []byte(unchangedOriginal), 0o644); err != nil {
		t.Fatalf("write clean session failed: %v", err)
	}
	if err := os.WriteFile(ignoredPath, []byte(ignoredOriginal), 0o644); err != nil {
		t.Fatalf("write ignored JSONL failed: %v", err)
	}

	result, err := repairConversationHistoryNamespaces(home)
	if err != nil {
		t.Fatalf("repair failed: %v", err)
	}
	if result.ScannedFiles != 3 || result.ScannedRecords != 8 || result.InvalidRecords != 1 {
		t.Fatalf("unexpected scan stats: %#v", result)
	}
	if result.ChangedFiles != 2 || result.ChangedRecords != 2 {
		t.Fatalf("unexpected change stats: %#v", result)
	}
	if result.ChangedBytes != int64(len(sessionOriginal)+len(archivedOriginal)) || result.MaxChangedBytes != int64(len(sessionOriginal)) || result.RequiredSpace == 0 || result.FreeSpace < result.RequiredSpace {
		t.Fatalf("unexpected disk preflight stats: %#v", result)
	}
	if result.BackupDir == nil || !isDir(*result.BackupDir) {
		t.Fatalf("repair should return an existing backup directory: %#v", result)
	}

	sessionUpdated, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("read updated session failed: %v", err)
	}
	if bytes.Contains(sessionUpdated, []byte(`"namespace":"mcp__browser"`)) {
		t.Fatalf("response_item payload namespace was not removed:\n%s", sessionUpdated)
	}
	for _, preserved := range []string{
		`"namespace":"outer"`,
		`"input":{"namespace":"nested"}`,
		`"payload":{"namespace":"keep-event"}`,
		`"input":{"namespace":"keep-nested"}`,
		`"namespace":"keep-message"`,
		`"namespace":"keep-dynamic-tool"`,
		`"large":9007199254740993`,
		`{not-json}`,
	} {
		if !bytes.Contains(sessionUpdated, []byte(preserved)) {
			t.Fatalf("repair removed or rewrote unrelated content %q:\n%s", preserved, sessionUpdated)
		}
	}
	if info, err := os.Stat(sessionPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("session mode should be preserved: info=%v err=%v", info, err)
	}

	archivedUpdated, err := os.ReadFile(archivedPath)
	if err != nil {
		t.Fatalf("read updated archived session failed: %v", err)
	}
	var archivedRecord struct {
		Payload map[string]json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(archivedUpdated, &archivedRecord); err != nil {
		t.Fatalf("updated archived record invalid: %v\n%s", err, archivedUpdated)
	}
	if _, exists := archivedRecord.Payload["namespace"]; exists {
		t.Fatalf("archived payload namespace remains: %s", archivedUpdated)
	}
	if !bytes.Contains(archivedUpdated, []byte(`"arguments":{"namespace":"nested"}`)) {
		t.Fatalf("archived nested namespace should remain: %s", archivedUpdated)
	}
	ignoredUpdated, err := os.ReadFile(ignoredPath)
	if err != nil || string(ignoredUpdated) != ignoredOriginal {
		t.Fatalf("non-rollout JSONL must not be scanned or modified: err=%v data=%s", err, ignoredUpdated)
	}

	for path, want := range map[string]string{sessionPath: sessionOriginal, archivedPath: archivedOriginal} {
		relative, _ := filepath.Rel(home, path)
		backup, readErr := os.ReadFile(filepath.Join(*result.BackupDir, relative))
		if readErr != nil {
			t.Fatalf("read backup for %s failed: %v", path, readErr)
		}
		if string(backup) != want {
			t.Fatalf("backup for %s is not the complete original file", path)
		}
	}
	metadata, err := os.ReadFile(filepath.Join(*result.BackupDir, "metadata.json"))
	if err != nil || !bytes.Contains(metadata, []byte(`"status": "completed"`)) {
		t.Fatalf("completed backup metadata missing: err=%v data=%s", err, metadata)
	}

	beforeSecondRun := append([]byte(nil), sessionUpdated...)
	second, err := repairConversationHistoryNamespaces(home)
	if err != nil {
		t.Fatalf("second repair failed: %v", err)
	}
	if second.ChangedFiles != 0 || second.ChangedRecords != 0 || second.BackupDir != nil {
		t.Fatalf("second repair should be an idempotent no-op: %#v", second)
	}
	afterSecondRun, _ := os.ReadFile(sessionPath)
	if !bytes.Equal(beforeSecondRun, afterSecondRun) {
		t.Fatal("idempotent repair rewrote an already-clean file")
	}
	backupEntries, err := os.ReadDir(filepath.Join(home, "backups_state", conversationHistoryRepairBackupKind))
	if err != nil || len(backupEntries) != 1 {
		t.Fatalf("no-op repair must not create another backup: entries=%d err=%v", len(backupEntries), err)
	}
}

func TestRepairConversationHistoryCommandIsDispatched(t *testing.T) {
	allowConversationHistoryRepairProcessesForTest(t)
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	s := &server{}
	result := s.dispatch(context.Background(), "repair_conversation_history", map[string]any{})
	if stringFromAny(result["status"]) != "accepted" || stringFromAny(result["taskStatus"]) != "running" {
		t.Fatalf("repair command should be dispatched: %#v", result)
	}
	taskID := stringFromAny(result["taskId"])
	if taskID == "" {
		t.Fatalf("repair command should return a task ID: %#v", result)
	}
	terminal := waitForConversationHistoryRepairTask(t, s, taskID)
	if stringFromAny(terminal["taskStatus"]) != "ok" || stringFromAny(terminal["phase"]) != "completed" {
		t.Fatalf("repair task should finish successfully: %#v", terminal)
	}
	if _, exists := terminal["scannedFiles"]; !exists {
		t.Fatalf("repair command payload should include scan stats: %#v", result)
	}
	if commandTimeout("repair_conversation_history") != 45*time.Second {
		t.Fatalf("background task start should use the normal request timeout, got %s", commandTimeout("repair_conversation_history"))
	}
}

func TestConversationHistoryRepairTaskRejectsMismatchedCancellationAndReusesRunningTask(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	task := &conversationHistoryRepairTask{ID: "current-task", Status: "running", Phase: "scanning", cancel: cancel}
	s := &server{conversationHistoryRepairTask: task}

	duplicate := s.repairConversationHistory()
	if stringFromAny(duplicate["status"]) != "accepted" || stringFromAny(duplicate["taskId"]) != task.ID {
		t.Fatalf("duplicate start should return the running task: %#v", duplicate)
	}
	mismatch := s.cancelConversationHistoryRepair(map[string]any{"taskId": "stale-task"})
	if stringFromAny(mismatch["status"]) != "failed" || task.CancelRequested {
		t.Fatalf("mismatched task ID must not cancel the running task: %#v", mismatch)
	}

	accepted := s.cancelConversationHistoryRepair(map[string]any{"taskId": task.ID})
	if stringFromAny(accepted["status"]) != "accepted" || stringFromAny(accepted["taskStatus"]) != "cancelling" {
		t.Fatalf("matching task ID should request cancellation: %#v", accepted)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("matching cancellation did not cancel the task context")
	}
}

func TestRepairConversationHistoryCancellationDuringScanLeavesHistoryUntouched(t *testing.T) {
	allowConversationHistoryRepairProcessesForTest(t)
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "rollout-cancel-scan.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	line := `{"type":"response_item","payload":{"type":"custom_tool_call","namespace":"mcp__cancel_scan","input":{"value":"` + strings.Repeat("x", 512) + `"}}}` + "\n"
	original := []byte(strings.Repeat(line, 4096))
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatalf("write rollout failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	result, err := repairConversationHistoryNamespacesWithContext(ctx, home, func(progress conversationHistoryRepairProgress) {
		if progress.Phase == "scanning" && progress.ProcessedBytes > 0 {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("scan cancellation should return context.Canceled: result=%#v err=%v", result, err)
	}
	updated, readErr := os.ReadFile(path)
	if readErr != nil || !bytes.Equal(updated, original) {
		t.Fatalf("scan cancellation must leave the source untouched: err=%v", readErr)
	}
	if result.BackupDir != nil || fileExists(filepath.Join(home, "backups_state", conversationHistoryRepairBackupKind)) {
		t.Fatalf("scan cancellation must not create a backup: %#v", result)
	}
}

func TestRepairConversationHistoryCancellationKeepsCompletedFileAndCurrentFileUntouched(t *testing.T) {
	allowConversationHistoryRepairProcessesForTest(t)
	home := t.TempDir()
	firstPath := filepath.Join(home, "sessions", "rollout-1.jsonl")
	secondPath := filepath.Join(home, "sessions", "rollout-2.jsonl")
	if err := os.MkdirAll(filepath.Dir(firstPath), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	firstOriginal := `{"type":"response_item","payload":{"type":"function_call","namespace":"mcp__first_cancel"}}`
	secondOriginal := `{"type":"response_item","payload":{"type":"function_call","namespace":"mcp__second_cancel"}}`
	for path, contents := range map[string]string{firstPath: firstOriginal, secondPath: secondOriginal} {
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("write rollout failed: %v", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	result, err := repairConversationHistoryNamespacesWithContext(ctx, home, func(progress conversationHistoryRepairProgress) {
		if progress.Phase == "repairing" && progress.Result.RepairedFiles == 1 {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("repair cancellation should return context.Canceled: result=%#v err=%v", result, err)
	}
	if result.RepairedFiles != 1 || result.RepairedRecords != 1 || result.BackupDir == nil {
		t.Fatalf("cancellation should retain exactly one completed file: %#v", result)
	}
	firstUpdated, _ := os.ReadFile(firstPath)
	if bytes.Contains(firstUpdated, []byte(`"namespace":"mcp__first_cancel"`)) {
		t.Fatal("the completed first file should remain repaired")
	}
	secondUpdated, _ := os.ReadFile(secondPath)
	if string(secondUpdated) != secondOriginal {
		t.Fatal("the next file should remain untouched after cancellation")
	}
	metadata, readErr := os.ReadFile(filepath.Join(*result.BackupDir, "metadata.json"))
	if readErr != nil || !bytes.Contains(metadata, []byte(`"status": "cancelled"`)) || !bytes.Contains(metadata, []byte(`"appliedFiles": 1`)) {
		t.Fatalf("cancelled backup metadata missing: err=%v data=%s", readErr, metadata)
	}

	continued, continueErr := repairConversationHistoryNamespaces(home)
	if continueErr != nil || continued.ChangedFiles != 1 || continued.RepairedFiles != 1 {
		t.Fatalf("rerun should safely finish the remaining file: result=%#v err=%v", continued, continueErr)
	}
	secondUpdated, _ = os.ReadFile(secondPath)
	if bytes.Contains(secondUpdated, []byte(`"namespace":"mcp__second_cancel"`)) {
		t.Fatal("rerun did not repair the remaining file")
	}
}

func TestRepairConversationHistoryCancellationDuringCurrentFileLeavesSourceUntouched(t *testing.T) {
	allowConversationHistoryRepairProcessesForTest(t)
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "rollout-cancel-current.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	line := `{"type":"response_item","payload":{"type":"custom_tool_call","namespace":"mcp__cancel_current","input":{"value":"` + strings.Repeat("x", 512) + `"}}}` + "\n"
	original := []byte(strings.Repeat(line, 4096))
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatalf("write rollout failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	result, err := repairConversationHistoryNamespacesWithContext(ctx, home, func(progress conversationHistoryRepairProgress) {
		if progress.Phase == "repairing" && progress.ProcessedBytes > 0 {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("current-file cancellation should return context.Canceled: result=%#v err=%v", result, err)
	}
	if result.RepairedFiles != 0 || result.BackupDir == nil {
		t.Fatalf("current-file cancellation must not count an atomic replacement: %#v", result)
	}
	updated, readErr := os.ReadFile(path)
	if readErr != nil || !bytes.Equal(updated, original) {
		t.Fatalf("current-file cancellation must leave source bytes untouched: err=%v", readErr)
	}
	metadata, readErr := os.ReadFile(filepath.Join(*result.BackupDir, "metadata.json"))
	if readErr != nil || !bytes.Contains(metadata, []byte(`"status": "cancelled"`)) || !bytes.Contains(metadata, []byte(`"appliedFiles": 0`)) {
		t.Fatalf("current-file cancellation metadata missing: err=%v data=%s", readErr, metadata)
	}
}

func TestShutdownConversationHistoryRepairCancelsAndWaitsForTask(t *testing.T) {
	taskCtx, cancelTask := context.WithCancel(context.Background())
	done := make(chan struct{})
	task := &conversationHistoryRepairTask{
		ID:     "shutdown-task",
		Status: "running",
		Phase:  "repairing",
		cancel: cancelTask,
		done:   done,
	}
	s := &server{conversationHistoryRepairTask: task}
	go func() {
		<-taskCtx.Done()
		s.conversationHistoryRepairMu.Lock()
		task.Status = "cancelled"
		s.conversationHistoryRepairMu.Unlock()
		close(done)
	}()

	ctx, cancelWait := context.WithTimeout(context.Background(), time.Second)
	defer cancelWait()
	if err := s.shutdownConversationHistoryRepair(ctx); err != nil {
		t.Fatalf("shutdown should wait for safe task cancellation: %v", err)
	}
	s.conversationHistoryRepairMu.Lock()
	defer s.conversationHistoryRepairMu.Unlock()
	if task.Status != "cancelled" || !task.CancelRequested {
		t.Fatalf("shutdown did not cancel the task safely: %#v", task)
	}
}

func waitForConversationHistoryRepairTask(t *testing.T, s *server, taskID string) commandResult {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		result := s.dispatch(context.Background(), "conversation_history_repair_status", map[string]any{"taskId": taskID})
		switch stringFromAny(result["taskStatus"]) {
		case "ok", "failed", "cancelled":
			return result
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for conversation history repair task %s", taskID)
	return nil
}

func TestRepairConversationHistoryNamespacesHandlesLongJSONLRecords(t *testing.T) {
	allowConversationHistoryRepairProcessesForTest(t)
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "rollout-long.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	longArgument := strings.Repeat("x", 256*1024)
	original := `{"type":"response_item","payload":{"type":"function_call","namespace":"mcp__long","arguments":"` + longArgument + `"}}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("write long rollout failed: %v", err)
	}

	result, err := repairConversationHistoryNamespaces(home)
	if err != nil {
		t.Fatalf("repair long rollout failed: %v", err)
	}
	if result.ChangedFiles != 1 || result.ChangedRecords != 1 {
		t.Fatalf("long rollout was not repaired: %#v", result)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read long rollout failed: %v", err)
	}
	if bytes.Contains(updated, []byte(`"namespace":"mcp__long"`)) || !bytes.Contains(updated, []byte(longArgument)) {
		t.Fatal("long rollout repair removed the wrong content")
	}
}

func TestRepairConversationHistoryNamespacesBlocksActiveChatGPT(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "rollout-active.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	original := `{"type":"response_item","payload":{"type":"custom_tool_call","namespace":"mcp__active"}}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("write rollout failed: %v", err)
	}
	previous := detectConversationHistoryActiveProcesses
	detectConversationHistoryActiveProcesses = func() ([]string, error) { return []string{"ChatGPT", "Codex"}, nil }
	t.Cleanup(func() { detectConversationHistoryActiveProcesses = previous })

	result, err := repairConversationHistoryNamespaces(home)
	if err == nil || !strings.Contains(err.Error(), "完全退出") {
		t.Fatalf("active ChatGPT should block repair: result=%#v err=%v", result, err)
	}
	if result.ScannedFiles != 0 || result.BackupDir != nil {
		t.Fatalf("blocked repair must not scan or create backups: %#v", result)
	}
	updated, _ := os.ReadFile(path)
	if string(updated) != original {
		t.Fatal("blocked repair modified the rollout")
	}
	if fileExists(filepath.Join(home, "tmp", "provider-sync.lock")) {
		t.Fatal("blocked repair should release the shared maintenance lock")
	}
}

func TestRepairConversationHistoryNamespacesFailsClosedWhenProcessCheckFails(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "rollout-process-check.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	original := `{"type":"response_item","payload":{"type":"function_call","namespace":"mcp__check"}}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("write rollout failed: %v", err)
	}
	previous := detectConversationHistoryActiveProcesses
	detectConversationHistoryActiveProcesses = func() ([]string, error) {
		return nil, errors.New("process query unavailable")
	}
	t.Cleanup(func() { detectConversationHistoryActiveProcesses = previous })

	result, err := repairConversationHistoryNamespaces(home)
	if err == nil || !strings.Contains(err.Error(), "运行状态失败") {
		t.Fatalf("process detection failure must stop repair: result=%#v err=%v", result, err)
	}
	if result.ScannedFiles != 0 || result.BackupDir != nil {
		t.Fatalf("failed process check must stop before scanning or backup: %#v", result)
	}
	updated, _ := os.ReadFile(path)
	if string(updated) != original {
		t.Fatal("process-check failure modified the rollout")
	}
}

func TestRepairConversationHistoryNamespacesRejectsInsufficientDiskSpace(t *testing.T) {
	allowConversationHistoryRepairProcessesForTest(t)
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "rollout-disk.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	original := `{"type":"response_item","payload":{"type":"function_call","namespace":"mcp__disk"}}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("write rollout failed: %v", err)
	}
	previous := conversationHistoryAvailableDiskBytes
	conversationHistoryAvailableDiskBytes = func(string) (uint64, error) { return 1, nil }
	t.Cleanup(func() { conversationHistoryAvailableDiskBytes = previous })

	result, err := repairConversationHistoryNamespaces(home)
	if err == nil || !strings.Contains(err.Error(), "磁盘空间不足") {
		t.Fatalf("insufficient disk should block repair: result=%#v err=%v", result, err)
	}
	if result.RequiredSpace <= result.FreeSpace || result.BackupDir != nil {
		t.Fatalf("disk preflight should happen before backup creation: %#v", result)
	}
	updated, _ := os.ReadFile(path)
	if string(updated) != original {
		t.Fatal("disk-preflight failure modified the rollout")
	}
}

func TestRepairConversationHistoryNamespacesStopsWhenLauncherGuardIsBusy(t *testing.T) {
	allowConversationHistoryRepairProcessesForTest(t)
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "rollout-launch-race.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	original := `{"type":"response_item","payload":{"type":"function_call","namespace":"mcp__race"}}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("write rollout failed: %v", err)
	}
	previous := acquireConversationHistoryLauncherGuard
	acquireConversationHistoryLauncherGuard = func() (func(), error) {
		return nil, errors.New("launcher busy")
	}
	t.Cleanup(func() { acquireConversationHistoryLauncherGuard = previous })

	result, err := repairConversationHistoryNamespaces(home)
	if err == nil || !strings.Contains(err.Error(), "launcher busy") {
		t.Fatalf("busy launcher guard should stop repair: result=%#v err=%v", result, err)
	}
	if result.ScannedFiles != 1 || result.ChangedFiles != 1 || result.BackupDir != nil {
		t.Fatalf("launcher race must stop after scan and before backup: %#v", result)
	}
	updated, _ := os.ReadFile(path)
	if string(updated) != original {
		t.Fatal("launcher-guard failure modified the rollout")
	}
}

func TestRepairConversationHistoryNamespacesStopsBeforeReplaceWhenChatGPTRestarts(t *testing.T) {
	allowConversationHistoryRepairProcessesForTest(t)
	home := t.TempDir()
	firstPath := filepath.Join(home, "sessions", "rollout-1.jsonl")
	secondPath := filepath.Join(home, "sessions", "rollout-2.jsonl")
	if err := os.MkdirAll(filepath.Dir(firstPath), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	firstOriginal := `{"type":"response_item","payload":{"type":"function_call","namespace":"mcp__first"}}`
	secondOriginal := `{"type":"response_item","payload":{"type":"function_call","namespace":"mcp__restart"}}`
	for path, contents := range map[string]string{firstPath: firstOriginal, secondPath: secondOriginal} {
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("write rollout failed: %v", err)
		}
	}
	directChecks := 0
	detectConversationHistoryDirectProcesses = func() ([]string, error) {
		directChecks++
		if directChecks == 1 {
			return nil, nil
		}
		return []string{"ChatGPT"}, nil
	}

	result, err := repairConversationHistoryNamespaces(home)
	if err == nil || !strings.Contains(err.Error(), "未自动回滚") {
		t.Fatalf("ChatGPT restart should stop before replacement: result=%#v err=%v", result, err)
	}
	if result.BackupDir == nil {
		t.Fatal("the complete original should already be backed up before the final process check")
	}
	firstUpdated, _ := os.ReadFile(firstPath)
	if bytes.Contains(firstUpdated, []byte(`"namespace":"mcp__first"`)) {
		t.Fatal("the first rollout should remain safely repaired instead of being rolled back")
	}
	secondUpdated, _ := os.ReadFile(secondPath)
	if string(secondUpdated) != secondOriginal {
		t.Fatal("restart detected before replace must leave the current rollout untouched")
	}
	metadata, readErr := os.ReadFile(filepath.Join(*result.BackupDir, "metadata.json"))
	if readErr != nil || !bytes.Contains(metadata, []byte(`"status": "interrupted"`)) || !bytes.Contains(metadata, []byte(`"appliedFiles": 1`)) {
		t.Fatalf("interrupted backup metadata missing: err=%v data=%s", readErr, metadata)
	}
}

func allowConversationHistoryRepairProcessesForTest(t *testing.T) {
	t.Helper()
	previousProcesses := detectConversationHistoryActiveProcesses
	previousDirectProcesses := detectConversationHistoryDirectProcesses
	previousGuard := acquireConversationHistoryLauncherGuard
	detectConversationHistoryActiveProcesses = func() ([]string, error) { return nil, nil }
	detectConversationHistoryDirectProcesses = func() ([]string, error) { return nil, nil }
	acquireConversationHistoryLauncherGuard = func() (func(), error) { return func() {}, nil }
	t.Cleanup(func() {
		detectConversationHistoryActiveProcesses = previousProcesses
		detectConversationHistoryDirectProcesses = previousDirectProcesses
		acquireConversationHistoryLauncherGuard = previousGuard
	})
}

func TestConversationHistoryRequiredSpaceUsesBackupPeakAndSafetyMargin(t *testing.T) {
	const gib = int64(1024 * 1024 * 1024)
	got := conversationHistoryRequiredSpace(3*gib, gib/2)
	peak := uint64(3*gib + gib/2)
	want := peak + peak/10
	if got != want {
		t.Fatalf("required space should be sum + largest file + 10%% margin: got=%d want=%d", got, want)
	}
	got = conversationHistoryRequiredSpace(10*1024, 10*1024)
	want = 20*1024 + conversationHistoryRepairMinDiskMargin
	if got != want {
		t.Fatalf("small repairs should retain the minimum margin: got=%d want=%d", got, want)
	}
}

func TestProviderSyncLockWithLiveOwnerDoesNotExpireByAge(t *testing.T) {
	lockDir := filepath.Join(t.TempDir(), "provider-sync.lock")
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	owner := fmt.Sprintf(`{"pid":%d,"startedAt":%d}`, os.Getpid(), time.Now().Add(-2*providerSyncLockTTL).Unix())
	if err := os.WriteFile(filepath.Join(lockDir, "owner.json"), []byte(owner), 0o644); err != nil {
		t.Fatalf("write owner failed: %v", err)
	}
	if stale, reason := providerSyncLockStale(lockDir, time.Now()); stale || reason != "owner_active" {
		t.Fatalf("live maintenance owner must not expire by age: stale=%v reason=%s", stale, reason)
	}
}

func TestProcessIDRunningDetectsCurrentProcess(t *testing.T) {
	running, err := processIDRunning(os.Getpid())
	if err != nil || !running {
		t.Fatalf("current process should be reported as running: running=%v err=%v", running, err)
	}
}
