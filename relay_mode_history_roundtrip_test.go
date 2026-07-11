package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRelayModeHistorySyncRoundTripPreservesSessions(t *testing.T) {
	testCases := []struct {
		name      string
		relayID   string
		relayMode string
		pure      bool
	}{
		{name: "mixed_api", relayID: "mixed", relayMode: "mixedApi", pure: false},
		{name: "pure_api", relayID: "pure", relayMode: "pureApi", pure: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			codexHome := filepath.Join(home, ".codex")
			if err := os.MkdirAll(codexHome, 0o755); err != nil {
				t.Fatalf("create Codex home failed: %v", err)
			}
			t.Setenv("CODEX_HOME", codexHome)
			stubCodexPluginCommands(t, nil)

			officialAuth := fakeChatGPTAuthJSON(t, "roundtrip@example.com")
			writeTestFile(t, filepath.Join(codexHome, "config.toml"), "model_provider = \"openai\"\n")
			writeTestFile(t, filepath.Join(codexHome, "auth.json"), officialAuth)

			dbPath := filepath.Join(codexHome, "state_5.sqlite")
			createProviderSyncThreadsTable(t, dbPath, true)
			fixtures := []relayHistoryRoundTripFixture{
				{
					id:       "019a61dd-9748-7743-9ce9-92b8663a935b",
					path:     filepath.Join(codexHome, "sessions", "2026", "07", "11", "rollout-019a61dd-9748-7743-9ce9-92b8663a935b.jsonl"),
					cwd:      filepath.Join(home, "active-project"),
					title:    "活动会话",
					archived: false,
					mode:     0o600,
				},
				{
					id:       "019a61dd-9748-7743-9ce9-92b8663a935c",
					path:     filepath.Join(codexHome, "archived_sessions", "2026", "07", "10", "rollout-019a61dd-9748-7743-9ce9-92b8663a935c.jsonl"),
					cwd:      filepath.Join(home, "archived-project"),
					title:    "归档会话",
					archived: true,
					mode:     0o640,
				},
			}

			for index := range fixtures {
				fixture := &fixtures[index]
				fixture.body = relayHistoryRoundTripBody(fixture.title)
				firstLine := strings.Replace(testSessionRolloutLine(fixture.id, fixture.cwd, fixture.title), "CodexPlusPlus", "openai", 1)
				if err := os.MkdirAll(filepath.Dir(fixture.path), 0o755); err != nil {
					t.Fatalf("create rollout directory failed: %v", err)
				}
				if err := os.WriteFile(fixture.path, []byte(firstLine+"\n"+fixture.body), fixture.mode); err != nil {
					t.Fatalf("write rollout fixture failed: %v", err)
				}
				insertProviderSyncThread(t, dbPath, map[string]any{
					"id":                 fixture.id,
					"rollout_path":       fixture.path,
					"created_at":         1779962400 + index,
					"updated_at":         1779962500 + index,
					"source":             "vscode",
					"model_provider":     "openai",
					"cwd":                fixture.cwd,
					"title":              fixture.title,
					"sandbox_policy":     `{"type":"danger-full-access"}`,
					"approval_mode":      "never",
					"tokens_used":        0,
					"has_user_event":     1,
					"archived":           boolInt(fixture.archived),
					"cli_version":        "",
					"first_user_message": fixture.title + "正文",
					"memory_mode":        "enabled",
					"created_at_ms":      1779962400000 + index,
					"updated_at_ms":      1779962500000 + index,
					"thread_source":      "user",
					"preview":            fixture.title + "正文",
				})
			}

			settings := defaultSettings()
			settings.RelayProfiles = []relayProfile{
				{
					ID:                   "official",
					Name:                 "Official",
					RelayMode:            "official",
					Protocol:             "responses",
					ConfigContents:       "model_provider = \"openai\"\n",
					AuthContents:         officialAuth,
					OfficialAuthContents: officialAuth,
					OfficialAccountLabel: "roundtrip@example.com",
				},
				{
					ID:                   testCase.relayID,
					Name:                 testCase.name,
					BaseURL:              "https://relay.example.test/v1",
					APIKey:               "roundtrip-key",
					RelayMode:            testCase.relayMode,
					Protocol:             "responses",
					ConfigContents:       buildTestRelayConfig("https://relay.example.test/v1", "roundtrip-key"),
					AuthContents:         officialAuth,
					OfficialAuthContents: officialAuth,
					OfficialAccountLabel: "roundtrip@example.com",
				},
			}
			settings.ActiveRelayID = "official"
			if err := saveSettings(settings); err != nil {
				t.Fatalf("save initial official settings failed: %v", err)
			}
			assertRelayHistoryRoundTripState(t, dbPath, fixtures, "openai")

			settings.ActiveRelayID = testCase.relayID
			if err := saveSettings(settings); err != nil {
				t.Fatalf("save API mode settings failed: %v", err)
			}
			apiResult := (&server{}).applyRelayInjection(testCase.pure)
			assertRelayHistorySyncResult(t, apiResult, "CodexPlusPlus")
			assertRelayHistoryRoundTripState(t, dbPath, fixtures, "CodexPlusPlus")

			settings = loadSettings()
			settings.ActiveRelayID = "official"
			if err := saveSettings(settings); err != nil {
				t.Fatalf("save restored official settings failed: %v", err)
			}
			officialResult := (&server{}).clearRelayInjection()
			assertRelayHistorySyncResult(t, officialResult, "openai")
			assertRelayHistoryRoundTripState(t, dbPath, fixtures, "openai")
		})
	}
}

type relayHistoryRoundTripFixture struct {
	id       string
	path     string
	cwd      string
	title    string
	body     string
	archived bool
	mode     os.FileMode
}

func relayHistoryRoundTripBody(title string) string {
	return fmt.Sprintf(
		"{\"timestamp\":\"2026-07-11T10:01:00Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"user_message\",\"message\":%q,\"images\":[\"data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAAB\"]}}\n"+
			"{\"timestamp\":\"2026-07-11T10:02:00Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call_output\",\"call_id\":\"call-history\",\"output\":\"工具输出：保留\\n第二行\"}}\n"+
			"{\"timestamp\":\"2026-07-11T10:03:00Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"附件说明\"},{\"type\":\"input_image\",\"image_url\":\"file:///tmp/history-attachment.png\"}]}}\n",
		title+"正文",
	)
}

func assertRelayHistorySyncResult(t *testing.T, result commandResult, targetProvider string) {
	t.Helper()
	if stringFromAny(result["status"]) != "ok" {
		t.Fatalf("mode switch failed for %s: %#v", targetProvider, result)
	}
	syncPayload, ok := result["providerSync"].(map[string]any)
	if !ok {
		t.Fatalf("mode switch did not return provider sync payload: %#v", result)
	}
	if stringFromAny(syncPayload["status"]) != "synced" {
		t.Fatalf("history sync did not complete for %s: %#v", targetProvider, syncPayload)
	}
	if stringFromAny(syncPayload["targetProvider"]) != targetProvider {
		t.Fatalf("history sync target mismatch: got %q want %q", stringFromAny(syncPayload["targetProvider"]), targetProvider)
	}
	if int64FromFlexible(syncPayload["changedSessionFiles"]) != 2 {
		t.Fatalf("history sync should rewrite both active and archived rollouts: %#v", syncPayload)
	}
}

func assertRelayHistoryRoundTripState(t *testing.T, dbPath string, fixtures []relayHistoryRoundTripFixture, targetProvider string) {
	t.Helper()
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatalf("open round-trip sqlite failed: %v", err)
	}
	var rowCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM threads").Scan(&rowCount); err != nil {
		_ = db.Close()
		t.Fatalf("count round-trip sqlite rows failed: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close round-trip sqlite failed: %v", err)
	}
	if rowCount != len(fixtures) {
		t.Fatalf("history sqlite row count changed: got %d want %d", rowCount, len(fixtures))
	}

	for _, fixture := range fixtures {
		data, err := os.ReadFile(fixture.path)
		if err != nil {
			t.Fatalf("rollout disappeared after switching to %s: %s: %v", targetProvider, fixture.path, err)
		}
		lineEnd := bytes.IndexByte(data, '\n')
		if lineEnd < 0 {
			t.Fatalf("rollout lost its message body after switching to %s: %s", targetProvider, fixture.path)
		}
		var firstRecord map[string]any
		if err := json.Unmarshal(data[:lineEnd], &firstRecord); err != nil {
			t.Fatalf("rollout first line is invalid JSON after switching to %s: %v", targetProvider, err)
		}
		payload, _ := firstRecord["payload"].(map[string]any)
		if got := stringFromAny(payload["model_provider"]); got != targetProvider {
			t.Fatalf("rollout provider mismatch for %s: got %q want %q", fixture.path, got, targetProvider)
		}
		if got := data[lineEnd+1:]; !bytes.Equal(got, []byte(fixture.body)) {
			t.Fatalf("rollout message/tool/attachment bytes changed for %s after switching to %s\n got: %q\nwant: %q", fixture.path, targetProvider, got, fixture.body)
		}
		if runtime.GOOS != "windows" {
			info, err := os.Stat(fixture.path)
			if err != nil {
				t.Fatalf("stat rollout failed: %v", err)
			}
			if got := info.Mode().Perm(); got != fixture.mode.Perm() {
				t.Fatalf("rollout permissions changed for %s: got %o want %o", fixture.path, got, fixture.mode.Perm())
			}
		}

		row := providerSyncThreadRow(t, dbPath, fixture.id)
		if got := stringFromAny(row["model_provider"]); got != targetProvider {
			t.Fatalf("sqlite provider mismatch for %s: got %q want %q", fixture.id, got, targetProvider)
		}
		if got := stringFromAny(row["rollout_path"]); got != fixture.path {
			t.Fatalf("sqlite rollout path changed for %s: got %q want %q", fixture.id, got, fixture.path)
		}
		if got := int64FromFlexible(row["archived"]); got != int64(boolInt(fixture.archived)) {
			t.Fatalf("sqlite archived flag changed for %s: got %d want %d", fixture.id, got, boolInt(fixture.archived))
		}
	}
}
