package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestParseLaunchRequestReadsRestartFlag(t *testing.T) {
	request := parseLaunchRequest([]string{"--launcher", "--debug-port", "9229", "--helper-port", "57321", "--restart"})

	if !request.restart {
		t.Fatal("restart flag should be true")
	}
	if request.debugPort != 9229 {
		t.Fatalf("debug port mismatch: %d", request.debugPort)
	}
	if request.helperPort != 57321 {
		t.Fatalf("helper port mismatch: %d", request.helperPort)
	}
}

func TestBuildManagerLauncherCommandUsesCodexPlusLauncher(t *testing.T) {
	command := buildManagerLauncherCommand(`C:\Tools\codextools-launcher.exe`, `C:\Codex\app`, 9229, 57321, true)

	expected := []string{
		`C:\Tools\codextools-launcher.exe`,
		"--launcher",
		"--debug-port",
		"9229",
		"--helper-port",
		"57321",
		"--app-path",
		`C:\Codex\app`,
		"--restart",
	}
	if strings.Join(command, "\x00") != strings.Join(expected, "\x00") {
		t.Fatalf("manager launch command should target Codex++ launcher:\n got: %#v\nwant: %#v", command, expected)
	}
	payload := managerLauncherPayload(command[0], command, `C:\Codex\app`, 9229, 57321)
	if got := stringFromAny(payload["launch_chain"]); got != "codex_plus_launcher" {
		t.Fatalf("launch chain mismatch: %q", got)
	}
	if got := stringFromAny(payload["launcher_path"]); !strings.Contains(strings.ToLower(got), "codextools-launcher") {
		t.Fatalf("launcher path should reference codextools-launcher, got %q", got)
	}
}

func TestBuildCodexLaunchCommandKeepsDebugArgumentsForExecutable(t *testing.T) {
	command := buildCodexLaunchCommand(filepath.Join(t.TempDir(), "Codex.exe"), 9229, []string{"--force_high_performance_gpu"})

	if len(command) != 4 {
		t.Fatalf("command length mismatch: %#v", command)
	}
	if !strings.EqualFold(filepath.Base(command[0]), "Codex.exe") {
		t.Fatalf("command should target Codex.exe: %#v", command)
	}
	if command[1] != "--remote-debugging-port=9229" {
		t.Fatalf("debug port argument mismatch: %#v", command)
	}
	if command[2] != "--remote-allow-origins=http://127.0.0.1:9229" {
		t.Fatalf("remote origin argument mismatch: %#v", command)
	}
	if command[3] != "--force_high_performance_gpu" {
		t.Fatalf("extra argument mismatch: %#v", command)
	}
}

func TestManagerLaunchAppPathSkipsPackagedOverrideWithoutExecutable(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows launch path fallback only applies on Windows")
	}
	requested := `C:\Program Files\WindowsApps\OpenAI.Codex_26.519.11010.0_x64__2p2nqsd0c76g0\app`

	if got := managerLaunchAppPath(requested, defaultSettings()); got != "" {
		t.Fatalf("packaged override without direct executable should be skipped, got %q", got)
	}
}

func TestManagerLaunchAppPathKeepsDirectExecutableOverride(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows launch path fallback only applies on Windows")
	}
	appDir := filepath.Join(t.TempDir(), "Codex")
	exe := filepath.Join(appDir, "Codex.exe")
	writeTestFile(t, exe, "binary")

	if got := managerLaunchAppPath(exe, defaultSettings()); got != appDir {
		t.Fatalf("direct executable override should be normalized and kept, got %q", got)
	}
}

func TestRendererInjectionPatchesPluginAvailabilityWithoutAds(t *testing.T) {
	for _, required := range []string{
		`pluginEntryUnlock`,
		`pluginMarketplaceUnlock`,
		`forcePluginInstall`,
		`pluginPatchDisabledInRelayMode`,
		`codexPlusBackendSettings.launchMode === "relay"`,
		`codexAppVersion`,
		`codexPluginLegacyEntryUnlockBeforeVersion`,
		`26.601.2237`,
		`parseCodexVersionParts`,
		`compareCodexVersions`,
		`codexPluginUnlockStrategy`,
		`plugin_unlock_strategy_selected`,
		`pluginUnlockStrategy === "legacy"`,
		`pluginUnlockStrategy === "modern"`,
		`pluginUnlockStrategy === "unknown"`,
		`installPluginMarketplaceRequestPatch`,
		`enablePluginEntry`,
		`unblockPluginInstallButtons`,
		`插件 - 已解锁`,
		`Plugins - Unlocked`,
		`强制安装`,
	} {
		if !strings.Contains(rendererInjectScript, required) {
			t.Fatalf("renderer injection should include plugin unlock capability; missing %q", required)
		}
	}
	for _, forbidden := range []string{
		`/ads`,
		`load_ads`,
		`RecommendationsScreen`,
		`codexPlusAds`,
		`codex-plus-ad`,
		`赞助商推荐`,
		`推荐内容`,
		`请作者喝咖啡`,
	} {
		if strings.Contains(rendererInjectScript, forbidden) {
			t.Fatalf("renderer injection should not include ads or recommendations; found %q", forbidden)
		}
	}
}

func TestBuildWatcherInstallPlanMatchesOriginalWindowsShape(t *testing.T) {
	plan := buildWatcherInstallPlan(`C:\Tools\Codex++.exe`, 9229, `C:\Users\A\AppData\Roaming\Microsoft\Windows\Start Menu\Programs\Startup\CodexPlusPlusWatcher.lnk`)

	if plan.LauncherPath != `C:\Tools\Codex++.exe` {
		t.Fatalf("launcher path mismatch: %q", plan.LauncherPath)
	}
	if plan.Arguments != "--debug-port 9229" {
		t.Fatalf("arguments mismatch: %q", plan.Arguments)
	}
	if plan.RunValue != `"C:\Tools\Codex++.exe" --debug-port 9229` {
		t.Fatalf("run value mismatch: %q", plan.RunValue)
	}
	if plan.ShortcutPath == "" {
		t.Fatal("shortcut path should be preserved")
	}
}

func TestParseWindowsUninstallRegistryValue(t *testing.T) {
	output := `HKEY_CURRENT_USER\Software\Microsoft\Windows\CurrentVersion\Uninstall\CodexTools
    DisplayName    REG_SZ    CodexTools
    UninstallString    REG_SZ    "C:\Users\A\AppData\Local\CodexTools\Uninstall.exe"
`
	value := parseWindowsRegQueryValue(output, "UninstallString")
	if value != `"C:\Users\A\AppData\Local\CodexTools\Uninstall.exe"` {
		t.Fatalf("uninstall registry value mismatch: %q", value)
	}
}

func TestWindowsExecutableFromCommandParsesQuotedPath(t *testing.T) {
	command := `"C:\Users\A\AppData\Local\CodexTools\Uninstall.exe" /S`
	if got := windowsExecutableFromCommand(command); got != `C:\Users\A\AppData\Local\CodexTools\Uninstall.exe` {
		t.Fatalf("quoted command path mismatch: %q", got)
	}
}

func TestNormalizeSettingsLanguage(t *testing.T) {
	settings := normalizeSettings(backendSettings{Language: "ja"})

	if settings.Language != "ja-JP" {
		t.Fatalf("language should normalize to ja-JP, got %q", settings.Language)
	}

	settings = normalizeSettings(backendSettings{Language: "unsupported"})
	if settings.Language != defaultLanguage {
		t.Fatalf("unsupported language should fall back to %q, got %q", defaultLanguage, settings.Language)
	}
}

func TestChatGPTAuthStatusFromContentsReadsEmail(t *testing.T) {
	status := chatGPTAuthStatusFromContents(fakeChatGPTAuthJSON(t, "alpha@example.com"), "test")

	if !status.Authenticated {
		t.Fatal("official auth should be authenticated")
	}
	if status.AccountLabel != "alpha@example.com" {
		t.Fatalf("account label mismatch: %q", status.AccountLabel)
	}
}

func TestLoadSettingsMigratesCurrentOfficialAuthToActiveProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	settings := defaultSettings()
	settings.RelayProfiles = []relayProfile{
		{ID: "first", Name: "First", RelayMode: "official", Protocol: "responses"},
		{ID: "second", Name: "Second", RelayMode: "official", Protocol: "responses"},
	}
	settings.ActiveRelayID = "second"
	writeTestFile(t, filepath.Join(home, ".codex", "auth.json"), fakeChatGPTAuthJSON(t, "active@example.com"))
	writeTestFile(t, filepath.Join(home, ".codex", "config.toml"), `model_provider = "openai"`+"\n")
	if err := atomicWriteJSON(settingsPath(), settings); err != nil {
		t.Fatalf("failed to write settings: %v", err)
	}

	loaded := loadSettings()

	if loaded.RelayProfiles[0].OfficialAuthContents != "" {
		t.Fatal("inactive profile should not receive migrated official auth")
	}
	active := activeRelayProfile(loaded)
	if active.ID != "second" {
		t.Fatalf("active profile mismatch: %q", active.ID)
	}
	if active.OfficialAccountLabel != "active@example.com" {
		t.Fatalf("official account label mismatch: %q", active.OfficialAccountLabel)
	}
	if active.OfficialAuthContents == "" {
		t.Fatal("official auth contents should be migrated")
	}
	if active.AuthContents == "" {
		t.Fatal("auth contents should be migrated")
	}
	if active.ConfigContents == "" {
		t.Fatal("config contents should be migrated for active profile")
	}
}

func TestSaveSettingsPreservesUnknownTopLevelFields(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeTestFile(t, settingsPath(), `{
  "futureFeature": {"enabled": true, "mode": "next"},
  "codexAppPath": "",
  "relayProfiles": [{"id":"default","name":"Default","protocol":"responses","relayMode":"official"}],
  "activeRelayId": "default"
}`)

	settings := loadSettings()
	settings.Language = "en-US"
	if err := saveSettings(settings); err != nil {
		t.Fatalf("failed to save settings: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := readJSON(settingsPath(), &raw); err != nil {
		t.Fatalf("failed to read saved settings: %v", err)
	}
	if string(raw["futureFeature"]) == "" {
		t.Fatal("unknown top-level field should be preserved")
	}
	if !strings.Contains(string(raw["futureFeature"]), `"mode": "next"`) {
		t.Fatalf("unknown field value changed: %s", string(raw["futureFeature"]))
	}
}

func TestRelayStatusDetectsBoundOfficialAuthWithoutCurrentAuthFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	settings := defaultSettings()
	settings.RelayProfiles = []relayProfile{{
		ID:                   "official",
		Name:                 "Official",
		RelayMode:            "official",
		Protocol:             "responses",
		OfficialAuthContents: fakeChatGPTAuthJSON(t, "bound@example.com"),
		OfficialAccountLabel: "bound@example.com",
	}}
	settings.ActiveRelayID = "official"

	status := relayStatusFromHome(filepath.Join(home, ".codex"), settings)

	if boolFromAny(status["currentAuthenticated"]) {
		t.Fatal("current auth file should not be detected")
	}
	if !boolFromAny(status["boundOfficialAuthenticated"]) {
		t.Fatalf("bound official auth should be detected: %#v", status)
	}
	if !boolFromAny(status["officialAuthenticated"]) {
		t.Fatalf("overall official auth should be detected: %#v", status)
	}
	if got := stringFromAny(status["boundOfficialAccountLabel"]); got != "bound@example.com" {
		t.Fatalf("bound account label mismatch: %q", got)
	}
	if got := stringFromAny(status["boundOfficialProfileId"]); got != "official" {
		t.Fatalf("bound profile id mismatch: %q", got)
	}
}

func TestInstallGuideConnectionDetectsBoundOfficialAuth(t *testing.T) {
	settings := defaultSettings()
	settings.RelayProfiles = []relayProfile{{
		ID:                   "official",
		Name:                 "Official",
		RelayMode:            "official",
		Protocol:             "responses",
		OfficialAuthContents: fakeChatGPTAuthJSON(t, "bound@example.com"),
		OfficialAccountLabel: "bound@example.com",
	}}
	settings.ActiveRelayID = "official"
	relayStatus := relayStatusFromHome(filepath.Join(t.TempDir(), ".codex"), settings)

	payload := installGuideConnectionPayload(settings, relayStatus)

	if !boolFromAny(payload["ready"]) {
		t.Fatalf("bound official auth should make guide connection ready: %#v", payload)
	}
	if !boolFromAny(payload["officialReady"]) {
		t.Fatalf("official account should be ready: %#v", payload)
	}
	if got := stringFromAny(payload["profileId"]); got != "official" {
		t.Fatalf("profile id mismatch: %q", got)
	}
}

func TestInstallGuideConnectionRequiresMixedApiFields(t *testing.T) {
	settings := defaultSettings()
	settings.RelayProfiles = []relayProfile{{
		ID:                   "mixed",
		Name:                 "Mixed",
		RelayMode:            "mixedApi",
		Protocol:             "responses",
		OfficialAuthContents: fakeChatGPTAuthJSON(t, "mixed@example.com"),
		OfficialAccountLabel: "mixed@example.com",
	}}
	settings.ActiveRelayID = "mixed"
	relayStatus := relayStatusFromHome(filepath.Join(t.TempDir(), ".codex"), settings)

	payload := installGuideConnectionPayload(settings, relayStatus)

	if boolFromAny(payload["ready"]) {
		t.Fatalf("mixed API without base URL/key should not be ready: %#v", payload)
	}
	if !boolFromAny(payload["officialReady"]) || boolFromAny(payload["apiReady"]) {
		t.Fatalf("mixed API readiness flags mismatch: %#v", payload)
	}
}

func TestDefaultCCSDBPathPrefersExistingHomeDatabase(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	expected := filepath.Join(home, ".cc-switch", "cc-switch.db")
	writeTestFile(t, expected, "sqlite")

	if got := defaultCCSDBPath(); got != expected {
		t.Fatalf("ccswitch database path mismatch: %q", got)
	}
}

func TestPlatformAndArchDisplayNames(t *testing.T) {
	if got := platformDisplayName("windows"); got != "Windows" {
		t.Fatalf("windows platform label mismatch: %q", got)
	}
	if got := archDisplayName("amd64"); got != "x64" {
		t.Fatalf("amd64 arch label mismatch: %q", got)
	}
	if got := archDisplayName("arm64"); got != "ARM64" {
		t.Fatalf("arm64 arch label mismatch: %q", got)
	}
}

func TestListCCSProvidersReadsSQLiteDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cc-switch.db")
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE providers (id TEXT, name TEXT, settings_config TEXT, app_type TEXT, sort_index INTEGER, created_at TEXT)`); err != nil {
		t.Fatalf("failed to create providers table: %v", err)
	}
	config := `{"base_url":"https://relay.example.com/v1","api_key":"relay-key"}`
	if _, err := db.Exec(`INSERT INTO providers (id, name, settings_config, app_type, sort_index, created_at) VALUES (?, ?, ?, ?, ?, ?)`, "provider-1", "Relay One", config, "codex", 1, "2026-05-27"); err != nil {
		t.Fatalf("failed to insert provider: %v", err)
	}

	providers, err := listCCSProviders(dbPath)
	if err != nil {
		t.Fatalf("failed to list providers: %v", err)
	}
	if len(providers) != 1 {
		t.Fatalf("provider count mismatch: %d", len(providers))
	}
	if providers[0].SourceID != "provider-1" || providers[0].BaseURL != "https://relay.example.com/v1" || providers[0].APIKey != "relay-key" {
		t.Fatalf("provider mismatch: %#v", providers[0])
	}
}

func TestListCCSProvidersReadsSettingsConfigVariants(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cc-switch.db")
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE providers (providerId TEXT, displayName TEXT, settingsConfig TEXT)`); err != nil {
		t.Fatalf("failed to create providers table: %v", err)
	}
	configText := strings.Join([]string{
		`model_provider = "CodexPlusPlus"`,
		`[model_providers.CodexPlusPlus]`,
		`wire_api = "chat"`,
		`base_url = "https://relay.example.com/v1/"`,
		`experimental_bearer_token = "toml-key"`,
	}, "\n")
	settingsConfig, _ := json.Marshal(map[string]any{
		"config": configText,
		"auth":   `{"tokens":{"id_token":"official-token"}}`,
	})
	if _, err := db.Exec(`INSERT INTO providers (providerId, displayName, settingsConfig) VALUES (?, ?, ?)`, "provider-2", "Relay Two", string(settingsConfig)); err != nil {
		t.Fatalf("failed to insert provider: %v", err)
	}

	providers, err := listCCSProviders(dbPath)
	if err != nil {
		t.Fatalf("failed to list providers: %v", err)
	}
	if len(providers) != 1 {
		t.Fatalf("provider count mismatch: %d", len(providers))
	}
	provider := providers[0]
	if provider.SourceID != "provider-2" || provider.Name != "Relay Two" {
		t.Fatalf("provider identity mismatch: %#v", provider)
	}
	if provider.BaseURL != "https://relay.example.com/v1" || provider.APIKey != "toml-key" || provider.Protocol != "chatCompletions" {
		t.Fatalf("provider settings mismatch: %#v", provider)
	}
	if provider.ConfigContents != configText {
		t.Fatalf("config contents mismatch:\n%s", provider.ConfigContents)
	}
	if provider.AuthContents != `{"tokens":{"id_token":"official-token"}}` {
		t.Fatalf("auth contents mismatch: %q", provider.AuthContents)
	}
}

func TestListCCSProvidersReadsRawTomlConfigColumn(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cc-switch.db")
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE providers (id TEXT, name TEXT, config TEXT, app_type TEXT)`); err != nil {
		t.Fatalf("failed to create providers table: %v", err)
	}
	configText := strings.Join([]string{
		`model_provider = "CodexPlusPlus"`,
		`[model_providers.CodexPlusPlus]`,
		`base_url = "https://relay.example.com/v1"`,
		`experimental_bearer_token = "raw-key"`,
	}, "\n")
	if _, err := db.Exec(`INSERT INTO providers (id, name, config, app_type) VALUES (?, ?, ?, ?)`, "provider-3", "Relay Three", configText, "codex"); err != nil {
		t.Fatalf("failed to insert provider: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO providers (id, name, config, app_type) VALUES (?, ?, ?, ?)`, "provider-4", "Other", configText, "claude"); err != nil {
		t.Fatalf("failed to insert non-codex provider: %v", err)
	}

	providers, err := listCCSProviders(dbPath)
	if err != nil {
		t.Fatalf("failed to list providers: %v", err)
	}
	if len(providers) != 1 {
		t.Fatalf("provider count mismatch: %d", len(providers))
	}
	if providers[0].SourceID != "provider-3" || providers[0].APIKey != "raw-key" || providers[0].ConfigContents != configText {
		t.Fatalf("provider mismatch: %#v", providers[0])
	}
}

func TestListCCSProvidersReadsMetaAPIFormatAndFillsBearerToken(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cc-switch.db")
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE providers (id TEXT, name TEXT, settings_config TEXT, meta TEXT, app_type TEXT)`); err != nil {
		t.Fatalf("failed to create providers table: %v", err)
	}
	configText := strings.Join([]string{
		`model_provider = "custom"`,
		`[model_providers.custom]`,
		`name = "Custom"`,
		`wire_api = "responses"`,
		`base_url = "https://relay.example.com/v1"`,
	}, "\n")
	settingsConfig, _ := json.Marshal(map[string]any{
		"config": configText,
		"auth":   map[string]any{"OPENAI_API_KEY": "auth-key"},
	})
	meta, _ := json.Marshal(map[string]any{"apiFormat": "openai_chat"})
	if _, err := db.Exec(`INSERT INTO providers (id, name, settings_config, meta, app_type) VALUES (?, ?, ?, ?, ?)`, "provider-5", "Relay Five", string(settingsConfig), string(meta), "codex"); err != nil {
		t.Fatalf("failed to insert provider: %v", err)
	}

	providers, err := listCCSProviders(dbPath)
	if err != nil {
		t.Fatalf("failed to list providers: %v", err)
	}
	if len(providers) != 1 {
		t.Fatalf("provider count mismatch: %d", len(providers))
	}
	provider := providers[0]
	if provider.Protocol != "chatCompletions" || provider.APIKey != "auth-key" {
		t.Fatalf("provider metadata mismatch: %#v", provider)
	}
	if !strings.Contains(provider.ConfigContents, `experimental_bearer_token = "auth-key"`) {
		t.Fatalf("config should be filled with bearer token:\n%s", provider.ConfigContents)
	}
}

func TestOfficialModeRequiresBoundOfficialAuth(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	settings := defaultSettings()
	settings.RelayProfiles = []relayProfile{{ID: "official", Name: "Official", RelayMode: "official", Protocol: "responses"}}
	settings.ActiveRelayID = "official"
	if err := saveSettings(settings); err != nil {
		t.Fatalf("failed to save settings: %v", err)
	}

	result := (&server{}).clearRelayInjection()

	if result["status"] != "failed" {
		t.Fatalf("official switch without bound auth should fail: %#v", result)
	}
}

func TestOfficialModeWritesBoundOfficialAuth(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	officialAuth := fakeChatGPTAuthJSON(t, "official@example.com")
	settings := defaultSettings()
	settings.RelayProfiles = []relayProfile{{
		ID:                   "official",
		Name:                 "Official",
		RelayMode:            "official",
		Protocol:             "responses",
		OfficialAuthContents: officialAuth,
		OfficialAccountLabel: "official@example.com",
	}}
	settings.ActiveRelayID = "official"
	if err := saveSettings(settings); err != nil {
		t.Fatalf("failed to save settings: %v", err)
	}
	writeTestFile(t, filepath.Join(home, ".codex", "config.toml"), `model_provider = "CodexPlusPlus"`+"\n\n[model_providers.CodexPlusPlus]\nbase_url = \"https://api.example.com\"\n")
	result := (&server{}).clearRelayInjection()

	if result["status"] != "ok" {
		t.Fatalf("official switch should succeed: %#v", result)
	}
	status := chatGPTAuthStatus(filepath.Join(home, ".codex"))
	if !status.Authenticated || status.AccountLabel != "official@example.com" {
		t.Fatalf("bound official auth was not written: %#v", status)
	}
	config, _ := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if strings.Contains(string(config), "CodexPlusPlus") {
		t.Fatalf("official mode should clear relay provider config:\n%s", string(config))
	}
}

func TestActivateOfficialAuthWritesBoundOfficialAuth(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	officialAuth := fakeChatGPTAuthJSON(t, "bound@example.com")
	currentAuth := fakeChatGPTAuthJSON(t, "current@example.com")
	writeTestFile(t, filepath.Join(home, ".codex", "auth.json"), currentAuth)
	settings := defaultSettings()
	settings.RelayProfiles = []relayProfile{{
		ID:                   "official",
		Name:                 "Official",
		RelayMode:            "official",
		Protocol:             "responses",
		OfficialAuthContents: officialAuth,
		OfficialAccountLabel: "bound@example.com",
	}}
	settings.ActiveRelayID = "official"
	if err := saveSettings(settings); err != nil {
		t.Fatalf("failed to save settings: %v", err)
	}

	result := (&server{}).activateOfficialAuth(map[string]any{
		"request": map[string]any{"profileId": "official"},
	})

	if result["status"] != "ok" {
		t.Fatalf("activate official auth should succeed: %#v", result)
	}
	status := chatGPTAuthStatus(filepath.Join(home, ".codex"))
	if !status.Authenticated || status.AccountLabel != "bound@example.com" {
		t.Fatalf("bound official auth was not activated: %#v", status)
	}
}

func TestImportCurrentRelayFilesUpdatesTargetProfileSnapshot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeTestFile(t, filepath.Join(home, ".codex", "config.toml"), "model_provider = \"openai\"\n")
	writeTestFile(t, filepath.Join(home, ".codex", "auth.json"), fakeChatGPTAuthJSON(t, "import@example.com"))
	settings := defaultSettings()
	settings.RelayProfiles = []relayProfile{{ID: "one", Name: "One", RelayMode: "official", Protocol: "responses"}}
	settings.ActiveRelayID = "one"
	if err := saveSettings(settings); err != nil {
		t.Fatalf("failed to save settings: %v", err)
	}

	result := (&server{}).importCurrentRelayFiles(map[string]any{
		"request": map[string]any{"profileId": "one"},
	})

	if result["status"] != "ok" {
		t.Fatalf("import current relay files should succeed: %#v", result)
	}
	loaded := loadSettings()
	profile := activeRelayProfile(loaded)
	if !strings.Contains(profile.ConfigContents, `model_provider = "openai"`) {
		t.Fatalf("config snapshot mismatch:\n%s", profile.ConfigContents)
	}
	if profile.AuthContents == "" || profile.OfficialAuthContents == "" {
		t.Fatalf("auth snapshot should be imported: %#v", profile)
	}
	if profile.OfficialAccountLabel != "import@example.com" {
		t.Fatalf("official account label mismatch: %q", profile.OfficialAccountLabel)
	}
}

func TestClearRelayInjectionFallsBackToLegacyOfficialAuthContents(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	officialAuth := fakeChatGPTAuthJSON(t, "legacy@example.com")
	settings := defaultSettings()
	settings.RelayProfiles = []relayProfile{{
		ID:                   "legacy",
		Name:                 "Legacy",
		RelayMode:            "official",
		Protocol:             "responses",
		OfficialAuthContents: officialAuth,
		OfficialAccountLabel: "legacy@example.com",
		AuthContents:         "",
		ConfigContents:       "",
	}}
	settings.ActiveRelayID = "legacy"
	if err := saveSettings(settings); err != nil {
		t.Fatalf("failed to save settings: %v", err)
	}
	writeTestFile(t, filepath.Join(home, ".codex", "config.toml"), `model_provider = "CodexPlusPlus"`+"\n")

	result := (&server{}).clearRelayInjection()

	if result["status"] != "ok" {
		t.Fatalf("official switch should succeed with legacy auth fallback: %#v", result)
	}
	auth, _ := os.ReadFile(filepath.Join(home, ".codex", "auth.json"))
	if chatGPTAuthStatusFromContents(string(auth), "auth").AccountLabel != "legacy@example.com" {
		t.Fatalf("legacy official auth should be written to live auth.json, got:\n%s", string(auth))
	}
}

func TestMixedModeWritesBoundOfficialAuthAndRelayConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	officialAuth := fakeChatGPTAuthJSON(t, "mixed@example.com")
	settings := defaultSettings()
	settings.RelayProfiles = []relayProfile{{
		ID:                   "mixed",
		Name:                 "Mixed",
		BaseURL:              "https://api.example.com",
		APIKey:               "relay-key",
		RelayMode:            "mixedApi",
		Protocol:             "responses",
		OfficialAuthContents: officialAuth,
		OfficialAccountLabel: "mixed@example.com",
	}}
	settings.ActiveRelayID = "mixed"
	if err := saveSettings(settings); err != nil {
		t.Fatalf("failed to save settings: %v", err)
	}

	result := (&server{}).applyRelayInjection(false)

	if result["status"] != "ok" {
		t.Fatalf("mixed switch should succeed: %#v", result)
	}
	status := chatGPTAuthStatus(filepath.Join(home, ".codex"))
	if !status.Authenticated || status.AccountLabel != "mixed@example.com" {
		t.Fatalf("bound mixed official auth was not written: %#v", status)
	}
	config, _ := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if !strings.Contains(string(config), `experimental_bearer_token = "relay-key"`) {
		t.Fatalf("mixed relay config missing bearer token:\n%s", string(config))
	}
	auth, _ := os.ReadFile(filepath.Join(home, ".codex", "auth.json"))
	if chatGPTAuthStatusFromContents(string(auth), "auth").AccountLabel != "mixed@example.com" {
		t.Fatalf("mixed relay should use profile auth snapshot, got:\n%s", string(auth))
	}
}

func TestPureAPIModeKeepsCurrentAuthWhenProfileSnapshotMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	officialAuth := fakeChatGPTAuthJSON(t, "stored@example.com")
	currentAuth := fakeChatGPTAuthJSON(t, "current@example.com")
	writeTestFile(t, filepath.Join(home, ".codex", "auth.json"), currentAuth)
	settings := defaultSettings()
	settings.RelayProfiles = []relayProfile{{
		ID:                   "pure",
		Name:                 "Pure",
		BaseURL:              "https://api.example.com",
		APIKey:               "pure-key",
		RelayMode:            "pureApi",
		Protocol:             "responses",
		OfficialAuthContents: officialAuth,
		OfficialAccountLabel: "stored@example.com",
	}}
	settings.ActiveRelayID = "pure"
	if err := saveSettings(settings); err != nil {
		t.Fatalf("failed to save settings: %v", err)
	}

	result := (&server{}).applyRelayInjection(true)

	if result["status"] != "ok" {
		t.Fatalf("pure API switch should succeed: %#v", result)
	}
	auth, _ := os.ReadFile(filepath.Join(home, ".codex", "auth.json"))
	if chatGPTAuthStatusFromContents(string(auth), "auth").AccountLabel != "current@example.com" {
		t.Fatalf("pure API mode should preserve auth.json, got:\n%s", string(auth))
	}
	config, _ := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if !strings.Contains(string(config), `experimental_bearer_token = "pure-key"`) {
		t.Fatalf("pure API config should carry provider bearer token:\n%s", string(config))
	}
	loaded := loadSettings()
	if activeRelayProfile(loaded).OfficialAccountLabel != "stored@example.com" {
		t.Fatal("pure API mode should not remove stored official binding")
	}
}

func TestPureAPIModeWritesProfileAuthSnapshotWhenPresent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	currentAuth := fakeChatGPTAuthJSON(t, "current@example.com")
	profileAuth := fakeChatGPTAuthJSON(t, "profile@example.com")
	writeTestFile(t, filepath.Join(home, ".codex", "auth.json"), currentAuth)
	settings := defaultSettings()
	settings.RelayProfiles = []relayProfile{{
		ID:             "pure",
		Name:           "Pure",
		BaseURL:        "https://api.example.com",
		APIKey:         "pure-key",
		RelayMode:      "pureApi",
		Protocol:       "responses",
		ConfigContents: buildTestRelayConfig("https://api.example.com", "pure-key"),
		AuthContents:   profileAuth,
	}}
	settings.ActiveRelayID = "pure"
	if err := saveSettings(settings); err != nil {
		t.Fatalf("failed to save settings: %v", err)
	}

	result := (&server{}).applyRelayInjection(true)

	if result["status"] != "ok" {
		t.Fatalf("pure API switch should succeed: %#v", result)
	}
	auth, _ := os.ReadFile(filepath.Join(home, ".codex", "auth.json"))
	if chatGPTAuthStatusFromContents(string(auth), "auth").AccountLabel != "profile@example.com" {
		t.Fatalf("pure API should restore saved auth snapshot, got:\n%s", string(auth))
	}
}

func TestPureAPIModeWritesImportedConfigWithoutAuthOverwrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	currentAuth := fakeChatGPTAuthJSON(t, "current@example.com")
	writeTestFile(t, filepath.Join(home, ".codex", "auth.json"), currentAuth)
	importedConfig := strings.Join([]string{
		`model_provider = "custom"`,
		`[model_providers.custom]`,
		`name = "Custom"`,
		`wire_api = "responses"`,
		`base_url = "https://imported.example.com/v1"`,
	}, "\n")
	settings := defaultSettings()
	settings.RelayProfiles = []relayProfile{{
		ID:             "pure-imported",
		Name:           "Pure Imported",
		BaseURL:        "https://fallback.example.com",
		APIKey:         "imported-key",
		RelayMode:      "pureApi",
		Protocol:       "responses",
		ConfigContents: importedConfig,
	}}
	settings.ActiveRelayID = "pure-imported"
	if err := saveSettings(settings); err != nil {
		t.Fatalf("failed to save settings: %v", err)
	}

	result := (&server{}).applyRelayInjection(true)

	if result["status"] != "ok" {
		t.Fatalf("pure API switch should succeed: %#v", result)
	}
	auth, _ := os.ReadFile(filepath.Join(home, ".codex", "auth.json"))
	if chatGPTAuthStatusFromContents(string(auth), "auth").AccountLabel != "current@example.com" {
		t.Fatalf("pure API mode should preserve auth.json, got:\n%s", string(auth))
	}
	config, _ := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	configText := string(config)
	if !strings.Contains(configText, `base_url = "https://imported.example.com/v1"`) {
		t.Fatalf("pure API should write imported config.toml:\n%s", configText)
	}
	if strings.Contains(configText, "fallback.example.com") {
		t.Fatalf("pure API should not regenerate over imported config:\n%s", configText)
	}
	if !strings.Contains(configText, `experimental_bearer_token = "imported-key"`) {
		t.Fatalf("pure API should ensure bearer token in imported config:\n%s", configText)
	}
}

func TestPureAPISwitchRestoresPluginsAfterSnapshotOverwrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	stubCodexPluginCommands(t, nil)
	writeTestFile(t, filepath.Join(home, ".codex", "auth.json"), fakeChatGPTAuthJSON(t, "current@example.com"))
	writeTestFile(t, filepath.Join(home, ".codex", "config.toml"), strings.Join([]string{
		`model_provider = "openai"`,
		``,
		`[marketplaces.openai-curated]`,
		`source_type = "local"`,
		`source = "` + filepath.Join(home, ".codex", ".tmp", "plugins") + `"`,
		``,
		`[plugins."github@openai-curated"]`,
		`enabled = true`,
		``,
	}, "\n"))
	writeTestFile(t, filepath.Join(home, ".codex", ".tmp", "plugins", ".agents", "plugins", "marketplace.json"), `{"name":"openai-curated"}`)
	writeTestFile(t, filepath.Join(home, ".codex", "plugins", "cache", "openai-curated", "github", "v1", ".codex-plugin", "plugin.json"), `{"name":"github"}`)
	settings := defaultSettings()
	settings.RelayProfiles = []relayProfile{{
		ID:             "pure",
		Name:           "Pure",
		BaseURL:        "https://api.example.com",
		APIKey:         "pure-key",
		RelayMode:      "pureApi",
		Protocol:       "responses",
		ConfigContents: buildTestRelayConfig("https://api.example.com", "pure-key"),
	}}
	settings.ActiveRelayID = "pure"
	if err := saveSettings(settings); err != nil {
		t.Fatalf("failed to save settings: %v", err)
	}

	result := (&server{}).applyRelayInjection(true)

	if result["status"] != "ok" {
		t.Fatalf("pure API switch should succeed: %#v", result)
	}
	config := readFile(filepath.Join(home, ".codex", "config.toml"))
	for _, expected := range []string{
		`[marketplaces.openai-curated]`,
		`source = "` + filepath.Join(home, ".codex", ".tmp", "plugins") + `"`,
		`[plugins."github@openai-curated"]`,
	} {
		if !strings.Contains(config, expected) {
			t.Fatalf("config missing %q after pure API switch:\n%s", expected, config)
		}
	}
	repair := result["pluginRepair"].(map[string]any)
	if stringFromAny(repair["marketplaceRefreshStatus"]) != "ok" {
		t.Fatalf("plugin repair should refresh marketplace: %#v", repair)
	}
}

func TestOfficialSwitchRestoresPluginsAfterSnapshotOverwrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	stubCodexPluginCommands(t, nil)
	officialAuth := fakeChatGPTAuthJSON(t, "official@example.com")
	writeTestFile(t, filepath.Join(home, ".codex", ".tmp", "plugins", ".agents", "plugins", "marketplace.json"), `{"name":"openai-curated"}`)
	writeTestFile(t, filepath.Join(home, ".codex", "plugins", "cache", "openai-curated", "github", "v1", ".codex-plugin", "plugin.json"), `{"name":"github"}`)
	settings := defaultSettings()
	settings.RelayProfiles = []relayProfile{{
		ID:                   "official",
		Name:                 "Official",
		RelayMode:            "official",
		Protocol:             "responses",
		ConfigContents:       `model_provider = "openai"` + "\n",
		OfficialAuthContents: officialAuth,
		OfficialAccountLabel: "official@example.com",
	}}
	settings.ActiveRelayID = "official"
	if err := saveSettings(settings); err != nil {
		t.Fatalf("failed to save settings: %v", err)
	}

	result := (&server{}).clearRelayInjection()

	if result["status"] != "ok" {
		t.Fatalf("official switch should succeed: %#v", result)
	}
	config := readFile(filepath.Join(home, ".codex", "config.toml"))
	for _, expected := range []string{
		`[marketplaces.openai-curated]`,
		`[plugins."github@openai-curated"]`,
	} {
		if !strings.Contains(config, expected) {
			t.Fatalf("config missing %q after official switch:\n%s", expected, config)
		}
	}
}

func TestRepairCodexGoalsConfigEnablesGoalsFeature(t *testing.T) {
	contents := strings.Join([]string{
		`model_provider = "CodexPlusPlus"`,
		"",
		"[features]",
		"remote_connections = true",
		"",
	}, "\n")

	updated := repairCodexGoalsConfig(contents)

	if !strings.Contains(updated, "[features]\nremote_connections = true\ngoals = true") {
		t.Fatalf("goals feature was not added to features table:\n%s", updated)
	}
	if strings.Count(updated, "goals = true") != 1 {
		t.Fatalf("goals feature should be written exactly once:\n%s", updated)
	}
}

func TestRepairCodexPluginConfigRestoresCachedPluginTables(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeTestFile(t, filepath.Join(home, ".tmp", "plugins", ".agents", "plugins", "marketplace.json"), `{"name":"openai-curated"}`)
	writeTestFile(t, filepath.Join(home, "plugins", "cache", "openai-curated", "github", "6188456f", ".codex-plugin", "plugin.json"), `{"name":"github"}`)
	writeTestFile(t, filepath.Join(home, "plugins", "cache", "openai-bundled", "browser", "26.519.41501", ".codex-plugin", "plugin.json"), `{"name":"browser"}`)

	updated, pluginCount, marketplaceCount, _ := repairCodexPluginConfig(home, `model_provider = "CodexPlusPlus"`+"\n")

	if pluginCount != 2 {
		t.Fatalf("plugin count mismatch: %d", pluginCount)
	}
	if marketplaceCount != 1 {
		t.Fatalf("marketplace count mismatch: %d", marketplaceCount)
	}
	for _, expected := range []string{
		`[marketplaces.openai-curated]`,
		`source = "` + filepath.Join(home, ".tmp", "plugins") + `"`,
		`[plugins."browser@openai-bundled"]`,
		`[plugins."github@openai-curated"]`,
		`enabled = true`,
	} {
		if !strings.Contains(updated, expected) {
			t.Fatalf("updated config missing %q:\n%s", expected, updated)
		}
	}
}

func TestRepairCodexPluginConfigCorrectsMarketplaceSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeTestFile(t, filepath.Join(home, ".tmp", "plugins", ".agents", "plugins", "marketplace.json"), `{"name":"openai-curated"}`)

	updated, _, marketplaceCount, _ := repairCodexPluginConfig(home, strings.Join([]string{
		`model_provider = "CodexPlusPlus"`,
		``,
		`[marketplaces.openai-curated]`,
		`last_updated = "old"`,
		`source_type = "local"`,
		`source = "/stale/plugins"`,
		`custom_flag = "keep"`,
		``,
	}, "\n"))

	if marketplaceCount != 1 {
		t.Fatalf("marketplace count mismatch: %d", marketplaceCount)
	}
	if strings.Contains(updated, `/stale/plugins`) {
		t.Fatalf("stale marketplace source should be replaced:\n%s", updated)
	}
	if !strings.Contains(updated, `source = "`+filepath.Join(home, ".tmp", "plugins")+`"`) {
		t.Fatalf("marketplace source should point to discovered local marketplace:\n%s", updated)
	}
	if !strings.Contains(updated, `custom_flag = "keep"`) {
		t.Fatalf("marketplace repair should preserve unrelated table keys:\n%s", updated)
	}
}

func TestRefreshCodexMarketplacesRereadsLocalMarketplaceWithoutGitSource(t *testing.T) {
	home := t.TempDir()
	writeTestFile(t, filepath.Join(home, "config.toml"), `model_provider = "CodexPlusPlus"`+"\n")
	writeTestFile(t, filepath.Join(home, ".tmp", "plugins", ".agents", "plugins", "marketplace.json"), `{"name":"openai-curated"}`)
	var commands []string
	stubCodexPluginCommands(t, nil, func(args []string) {
		commands = append(commands, strings.Join(args, " "))
	})

	result := repairCodexConfig(home, codexConfigRepairOptions{Plugins: true, RefreshMarketplaces: true})

	if result.Status != "ok" {
		t.Fatalf("local marketplace refresh should not require git source: %#v", result)
	}
	expected := []string{"plugin marketplace upgrade", "plugin marketplace list", "plugin list"}
	if strings.Join(commands, "\x00") != strings.Join(expected, "\x00") {
		t.Fatalf("refresh commands mismatch:\n got: %#v\nwant: %#v", commands, expected)
	}
	if result.MarketplaceRefreshStatus != "ok" {
		t.Fatalf("marketplace refresh should be ok: %#v", result)
	}
}

func TestRepairCodexConfigRefreshFailureKeepsRestoredPluginTables(t *testing.T) {
	home := t.TempDir()
	writeTestFile(t, filepath.Join(home, "config.toml"), `model_provider = "CodexPlusPlus"`+"\n")
	writeTestFile(t, filepath.Join(home, ".tmp", "plugins", ".agents", "plugins", "marketplace.json"), `{"name":"openai-curated"}`)
	writeTestFile(t, filepath.Join(home, "plugins", "cache", "openai-curated", "github", "v1", ".codex-plugin", "plugin.json"), `{"name":"github"}`)
	stubCodexPluginCommands(t, map[string]error{
		"plugin marketplace upgrade": errors.New("boom"),
	})

	result := repairCodexConfig(home, codexConfigRepairOptions{Plugins: true, RefreshMarketplaces: true})

	if result.Status != "failed" {
		t.Fatalf("refresh failure should fail repair result: %#v", result)
	}
	if !strings.Contains(result.MarketplaceRefreshError, "boom") {
		t.Fatalf("refresh error should mention command failure: %#v", result)
	}
	config := readFile(filepath.Join(home, "config.toml"))
	for _, expected := range []string{
		`[marketplaces.openai-curated]`,
		`[plugins."github@openai-curated"]`,
	} {
		if !strings.Contains(config, expected) {
			t.Fatalf("config should keep restored plugin table %q after refresh failure:\n%s", expected, config)
		}
	}
}

func TestWriteCodexConfigWithBackupCreatesUniqueBackup(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, ".codex", "config.toml")
	original := "model_provider = \"openai\"\n"
	writeTestFile(t, configPath, original)

	firstBackup, err := writeCodexConfigWithBackup(configPath, "model_provider = \"one\"\n", "unit")
	if err != nil {
		t.Fatalf("first write failed: %v", err)
	}
	if firstBackup == nil {
		t.Fatal("first write should return a backup path")
	}
	firstData, _ := os.ReadFile(*firstBackup)
	if string(firstData) != original {
		t.Fatalf("first backup content mismatch:\n%s", string(firstData))
	}
	secondBackup, err := writeCodexConfigWithBackup(configPath, "model_provider = \"two\"\n", "unit")
	if err != nil {
		t.Fatalf("second write failed: %v", err)
	}
	if secondBackup == nil || *secondBackup == *firstBackup {
		t.Fatalf("second write should create a unique backup: first=%v second=%v", firstBackup, secondBackup)
	}
	secondData, _ := os.ReadFile(*secondBackup)
	if string(secondData) != "model_provider = \"one\"\n" {
		t.Fatalf("second backup content mismatch:\n%s", string(secondData))
	}
}

func TestSaveRelayFileConfigReturnsBackupPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configPath := filepath.Join(home, ".codex", "config.toml")
	writeTestFile(t, configPath, "before = true\n")

	result := (&server{}).saveRelayFile(map[string]any{
		"request": map[string]any{"kind": "config", "contents": "after = true\n"},
	})

	if result["status"] != "ok" {
		t.Fatalf("save config should succeed: %#v", result)
	}
	backupPath := stringFromAny(result["backupPath"])
	if backupPath == "" {
		t.Fatalf("save config should return backupPath: %#v", result)
	}
	backup, _ := os.ReadFile(backupPath)
	if string(backup) != "before = true\n" {
		t.Fatalf("backup content mismatch:\n%s", string(backup))
	}
}

func TestRelaySwitchesReturnBackupPath(t *testing.T) {
	for _, pure := range []bool{false, true} {
		t.Run(fmt.Sprintf("pure=%v", pure), func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			writeTestFile(t, filepath.Join(home, ".codex", "config.toml"), "before = true\n")
			settings := defaultSettings()
			settings.RelayProfiles = []relayProfile{{
				ID:        "relay",
				Name:      "Relay",
				BaseURL:   "https://api.example.com",
				APIKey:    "relay-key",
				RelayMode: "pureApi",
				Protocol:  "responses",
			}}
			settings.ActiveRelayID = "relay"
			if err := saveSettings(settings); err != nil {
				t.Fatalf("failed to save settings: %v", err)
			}
			var result commandResult
			if pure {
				result = (&server{}).applyRelayInjection(true)
			} else {
				result = (&server{}).applyRelayInjection(false)
			}
			if result["status"] != "ok" {
				t.Fatalf("relay switch should succeed: %#v", result)
			}
			backupPath := stringFromAny(result["backupPath"])
			if backupPath == "" {
				t.Fatalf("relay switch should return backupPath: %#v", result)
			}
			backup, _ := os.ReadFile(backupPath)
			if string(backup) != "before = true\n" {
				t.Fatalf("relay backup content mismatch:\n%s", string(backup))
			}
		})
	}
}

func TestRepairComputerUseBuildsWindowsCompatibilityTree(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_ELECTRON_ENABLE_WINDOWS_COMPUTER_USE", "1")
	writeTestFile(t, filepath.Join(home, "config.toml"), "model_provider = \"openai\"\n")

	status, err := repairComputerUse(home, "windows", false)
	if err != nil {
		t.Fatalf("repair computer use failed: %v", err)
	}
	if !status.MarketplacePlugin || !status.CacheLatest || !status.HelperTransport || !status.ConfigReady {
		t.Fatalf("computer use status incomplete: %#v", status)
	}
	if status.BackupPath == nil {
		t.Fatal("computer use repair should backup existing config")
	}
	for _, path := range []string{
		filepath.Join(home, ".tmp", "bundled-marketplaces", "openai-bundled"),
		filepath.Join(home, ".tmp", "bundled-marketplaces", "openai-bundled", ".agents", "plugins", "marketplace.json"),
		filepath.Join(home, ".tmp", "bundled-marketplaces", "openai-bundled", "plugins", "computer-use", ".codex-plugin", "plugin.json"),
		filepath.Join(home, "plugins", "cache", "openai-bundled", "computer-use", "latest", ".codex-plugin", "plugin.json"),
		filepath.Join(home, "plugins", "cache", "openai-bundled", "computer-use", "latest", "node_modules", "@oai", "sky", "dist", "project", "cua", "sky_js", "src", "targets", "windows", "internal", "helper_transport.js"),
	} {
		if !fileExists(path) {
			t.Fatalf("expected generated path: %s", path)
		}
	}
	config := readFile(filepath.Join(home, "config.toml"))
	for _, expected := range []string{
		"[marketplaces.openai-bundled]",
		`[plugins."computer-use@openai-bundled"]`,
		"[mcp_servers.node_repl]",
		`BROWSER_USE_MARKETPLACE_NAME = "openai-bundled"`,
		"CODEX_HOME = " + quoteToml(home),
		"[windows]",
		`sandbox = "unelevated"`,
	} {
		if !strings.Contains(config, expected) {
			t.Fatalf("config missing %q:\n%s", expected, config)
		}
	}
}

func TestRepairComputerUsePreservesOfficialMarketplacePlugin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_ELECTRON_ENABLE_WINDOWS_COMPUTER_USE", "1")
	writeTestFile(t, filepath.Join(home, "config.toml"), "model_provider = \"openai\"\n")
	officialPlugin := filepath.Join(home, ".tmp", "bundled-marketplaces", "openai-bundled", "plugins", "computer-use")
	writeTestFile(t, filepath.Join(officialPlugin, ".codex-plugin", "plugin.json"), `{"name":"computer-use","version":"26.527.31326"}`)
	writeTestFile(t, filepath.Join(officialPlugin, "node_modules", "@oai", "sky", "dist", "project", "cua", "sky_js", "src", "targets", "windows", "internal", "helper_transport.js"), "official-helper")
	writeTestFile(t, filepath.Join(home, ".tmp", "bundled-marketplaces", "openai-bundled", ".agents", "plugins", "marketplace.json"), `{"name":"openai-bundled","plugins":[{"name":"computer-use","source":{"source":"bundled","path":"./plugins/computer-use"}}]}`)

	status, err := repairComputerUse(home, "windows", false)
	if err != nil {
		t.Fatalf("repair computer use failed: %v", err)
	}
	if !status.MarketplacePlugin || !status.CacheLatest || !status.HelperTransport || !status.ConfigReady {
		t.Fatalf("computer use files should be ready with official plugin: %#v", status)
	}
	if got := readFile(filepath.Join(officialPlugin, "node_modules", "@oai", "sky", "dist", "project", "cua", "sky_js", "src", "targets", "windows", "internal", "helper_transport.js")); got != "official-helper" {
		t.Fatalf("official marketplace plugin should not be overwritten: %q", got)
	}
	config := readFile(filepath.Join(home, "config.toml"))
	if !strings.Contains(config, `source = "`+filepath.Join(home, ".tmp", "bundled-marketplaces", "openai-bundled")+`"`) {
		t.Fatalf("config should point to local bundled marketplace:\n%s", config)
	}
}

func TestRepairComputerUsePreservesOfficialCachedPlugin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_ELECTRON_ENABLE_WINDOWS_COMPUTER_USE", "1")
	writeTestFile(t, filepath.Join(home, "config.toml"), "model_provider = \"openai\"\n")
	officialCache := filepath.Join(home, "plugins", "cache", "openai-bundled", "computer-use", "26.527.31326")
	writeTestFile(t, filepath.Join(officialCache, ".codex-plugin", "plugin.json"), `{"name":"computer-use","version":"26.527.31326"}`)
	writeTestFile(t, filepath.Join(officialCache, "node_modules", "@oai", "sky", "dist", "project", "cua", "sky_js", "src", "targets", "windows", "internal", "helper_transport.js"), "official-cache-helper")

	status, err := repairComputerUse(home, "windows", false)
	if err != nil {
		t.Fatalf("repair computer use failed: %v", err)
	}
	if !status.MarketplacePlugin || !status.CacheLatest || !status.HelperTransport || !status.ConfigReady {
		t.Fatalf("computer use files should be ready with official cache: %#v", status)
	}
	if got := readFile(filepath.Join(officialCache, "node_modules", "@oai", "sky", "dist", "project", "cua", "sky_js", "src", "targets", "windows", "internal", "helper_transport.js")); got != "official-cache-helper" {
		t.Fatalf("official cached plugin should not be overwritten: %q", got)
	}
	if strings.Contains(readFile(filepath.Join(officialCache, ".codex-plugin", "plugin.json")), computerUsePluginVersion) {
		t.Fatalf("official cache manifest should not be replaced by local fallback")
	}
}

func TestRepairComputerUseMaterializesMissingMarketplaceFromCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_ELECTRON_ENABLE_WINDOWS_COMPUTER_USE", "1")
	writeTestFile(t, filepath.Join(home, "config.toml"), "model_provider = \"openai\"\n")
	cacheLatest := filepath.Join(home, "plugins", "cache", "openai-bundled", "computer-use", "latest")
	writeTestFile(t, filepath.Join(cacheLatest, ".codex-plugin", "plugin.json"), `{"name":"computer-use","version":"26.527.31326"}`)
	writeTestFile(t, filepath.Join(cacheLatest, "node_modules", "@oai", "sky", "dist", "project", "cua", "sky_js", "src", "targets", "windows", "internal", "helper_transport.js"), "official-cache-helper")

	status, err := repairComputerUse(home, "windows", false)
	if err != nil {
		t.Fatalf("repair computer use failed: %v", err)
	}
	if !status.MarketplaceManifest || !status.MarketplacePlugin || !status.CacheLatest || !status.HelperTransport || !status.ConfigReady {
		t.Fatalf("computer use files should be ready when marketplace is restored from cache: %#v", status)
	}
	marketplacePlugin := filepath.Join(home, ".tmp", "bundled-marketplaces", "openai-bundled", "plugins", "computer-use")
	if got := readFile(filepath.Join(marketplacePlugin, "node_modules", "@oai", "sky", "dist", "project", "cua", "sky_js", "src", "targets", "windows", "internal", "helper_transport.js")); got != "official-cache-helper" {
		t.Fatalf("marketplace plugin should be copied from official cache: %q", got)
	}
	manifest := readFile(filepath.Join(home, ".tmp", "bundled-marketplaces", "openai-bundled", ".agents", "plugins", "marketplace.json"))
	for _, expected := range []string{`"name": "computer-use"`, `"path": "./plugins/computer-use"`} {
		if !strings.Contains(manifest, expected) {
			t.Fatalf("marketplace manifest missing %q:\n%s", expected, manifest)
		}
	}
	if strings.Contains(readFile(filepath.Join(marketplacePlugin, ".codex-plugin", "plugin.json")), computerUsePluginVersion) {
		t.Fatalf("marketplace plugin should not be replaced by local fallback")
	}
}

func TestRepairComputerUseReturnsFailureWhenFinalStatusIncomplete(t *testing.T) {
	home := t.TempDir()
	writeTestFile(t, filepath.Join(home, "config.toml"), "model_provider = \"openai\"\n")

	status, err := repairComputerUse(home, "windows", false)
	if err == nil {
		t.Fatal("repair computer use should fail when environment variable was not enabled")
	}
	if !strings.Contains(err.Error(), "CODEX_ELECTRON_ENABLE_WINDOWS_COMPUTER_USE") {
		t.Fatalf("failure should include missing env detail: %v", err)
	}
	if !status.MarketplaceManifest || !status.MarketplacePlugin || !status.CacheLatest || !status.HelperTransport || !status.ConfigReady {
		t.Fatalf("repair should still report completed file state: %#v", status)
	}
}

func TestRepairComputerUseFailureReloadsPartialStatus(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_ELECTRON_ENABLE_WINDOWS_COMPUTER_USE", "1")
	configPath := filepath.Join(home, "config.toml")
	if err := os.MkdirAll(configPath, 0o755); err != nil {
		t.Fatalf("create config directory failed: %v", err)
	}

	status, err := repairComputerUse(home, "windows", false)
	if err == nil {
		t.Fatal("repair computer use should fail when config.toml is a directory")
	}
	if !status.MarketplaceReady || !status.MarketplaceManifest || !status.MarketplacePlugin || !status.CacheLatest || !status.HelperTransport {
		t.Fatalf("partial status should be reloaded after failure: %#v", status)
	}
}

func TestCodexHomeDirHonorsCODEXHOME(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	if got := codexHomeDir(); got != filepath.Clean(home) {
		t.Fatalf("codexHomeDir should honor CODEX_HOME: got %q want %q", got, filepath.Clean(home))
	}
}

func TestAtomicWriteUsesReplaceFile(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows replaceFile behavior is covered by cross-compiled source check")
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("before"), 0o644); err != nil {
		t.Fatalf("seed file failed: %v", err)
	}
	if err := atomicWrite(path, []byte("after")); err != nil {
		t.Fatalf("atomic write should replace existing file on windows: %v", err)
	}
	if got := readFile(path); got != "after" {
		t.Fatalf("atomic write content mismatch: %q", got)
	}
}

func TestWindowsAtomicWriteUsesReplaceExistingRename(t *testing.T) {
	data, err := os.ReadFile("atomic_rename_windows.go")
	if err != nil {
		t.Fatalf("read atomic_rename_windows.go failed: %v", err)
	}
	matched, err := regexp.Match(`windows\.Rename\(\s*source,\s*target\s*\)`, data)
	if err != nil {
		t.Fatalf("regexp failed: %v", err)
	}
	if !matched {
		t.Fatalf("windows replaceFile must use windows.Rename so existing targets are replaced:\n%s", string(data))
	}
}

func TestSkillMCPBackupRestoreOnlyReplacesManagedState(t *testing.T) {
	home := t.TempDir()
	writeTestFile(t, filepath.Join(home, "skills", "alpha", "SKILL.md"), "alpha")
	writeTestFile(t, filepath.Join(home, "plugins", "cache", "openai-bundled", "browser", "v1", ".codex-plugin", "plugin.json"), "{}")
	writeTestFile(t, filepath.Join(home, ".tmp", "bundled-marketplaces", "openai-bundled", ".agents", "plugins", "marketplace.json"), "{}")
	writeTestFile(t, filepath.Join(home, "config.toml"), strings.Join([]string{
		`model_provider = "openai"`,
		`OPENAI_API_KEY = "keep"`,
		``,
		`[model_providers.openai]`,
		`name = "OpenAI"`,
		``,
		`[mcp_servers.old]`,
		`command = "old"`,
		``,
		`[plugins."old@market"]`,
		`enabled = true`,
		``,
		`[features]`,
		`goals = true`,
		``,
	}, "\n"))
	backup, err := createSkillMCPBackup(home, "first")
	if err != nil {
		t.Fatalf("create backup failed: %v", err)
	}
	if !backup.HasSkills || !backup.HasPluginCache || !backup.HasBundledMarket || !backup.HasConfigSnapshot {
		t.Fatalf("backup missing expected parts: %#v", backup)
	}

	_ = os.RemoveAll(filepath.Join(home, "skills"))
	writeTestFile(t, filepath.Join(home, "skills", "beta", "SKILL.md"), "beta")
	_ = os.RemoveAll(filepath.Join(home, "plugins", "cache"))
	writeTestFile(t, filepath.Join(home, "plugins", "cache", "other", "plugin", ".codex-plugin", "plugin.json"), "{}")
	writeTestFile(t, filepath.Join(home, "config.toml"), strings.Join([]string{
		`model_provider = "custom"`,
		`OPENAI_API_KEY = "preserve"`,
		``,
		`[model_providers.custom]`,
		`name = "Custom"`,
		``,
		`[mcp_servers.new]`,
		`command = "new"`,
		``,
		`[plugins."new@market"]`,
		`enabled = true`,
		``,
		`[windows]`,
		`sandbox = "danger-full-access"`,
		``,
	}, "\n"))

	current, restored, err := restoreSkillMCPBackup(home, backup.ID)
	if err != nil {
		t.Fatalf("restore backup failed: %v", err)
	}
	if current.ID == "" || restored.RestoreSourceBackup != current.ID {
		t.Fatalf("restore should create current backup: current=%#v restored=%#v", current, restored)
	}
	if !fileExists(filepath.Join(home, "skills", "alpha", "SKILL.md")) || fileExists(filepath.Join(home, "skills", "beta", "SKILL.md")) {
		t.Fatalf("skills directory was not restored")
	}
	config := readFile(filepath.Join(home, "config.toml"))
	for _, expected := range []string{
		`model_provider = "custom"`,
		`OPENAI_API_KEY = "preserve"`,
		`[model_providers.custom]`,
		`[mcp_servers.old]`,
		`[plugins."old@market"]`,
		`[features]`,
		`goals = true`,
	} {
		if !strings.Contains(config, expected) {
			t.Fatalf("restored config missing %q:\n%s", expected, config)
		}
	}
	for _, unexpected := range []string{
		`[mcp_servers.new]`,
		`[plugins."new@market"]`,
		`sandbox = "danger-full-access"`,
		`[model_providers.openai]`,
	} {
		if strings.Contains(config, unexpected) {
			t.Fatalf("restored config should not contain %q:\n%s", unexpected, config)
		}
	}
}

func TestSkillMCPBackupRejectsPathTraversal(t *testing.T) {
	home := t.TempDir()
	if _, err := resolveSkillMCPBackupDir(home, "../escape"); err == nil {
		t.Fatal("resolve should reject traversal id")
	}
	if err := deleteSkillMCPBackup(home, "../escape"); err == nil {
		t.Fatal("delete should reject traversal id")
	}
	if _, _, err := restoreSkillMCPBackup(home, "../escape"); err == nil {
		t.Fatal("restore should reject traversal id")
	}
}

func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("failed to create test dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}
}

func stubCodexPluginCommands(t *testing.T, failures map[string]error, observers ...func([]string)) {
	t.Helper()
	original := runCodexPluginCommand
	runCodexPluginCommand = func(home string, args ...string) codexCommandOutput {
		for _, observer := range observers {
			observer(append([]string{}, args...))
		}
		key := strings.Join(args, " ")
		if err := failures[key]; err != nil {
			return codexCommandOutput{Command: "codex " + key, Output: "failed " + key, Err: err}
		}
		return codexCommandOutput{Command: "codex " + key, Output: "ok " + key}
	}
	t.Cleanup(func() {
		runCodexPluginCommand = original
	})
}

func createProviderSyncThreadsTable(t *testing.T, dbPath string, includeThreadSource bool) {
	t.Helper()
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to open test sqlite db: %v", err)
	}
	defer db.Close()
	threadSourceColumn := ""
	if includeThreadSource {
		threadSourceColumn = "thread_source TEXT,"
	}
	if _, err := db.Exec(`CREATE TABLE threads (
		id TEXT PRIMARY KEY,
		rollout_path TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		source TEXT NOT NULL,
		model_provider TEXT NOT NULL,
		cwd TEXT NOT NULL,
		title TEXT NOT NULL,
		sandbox_policy TEXT NOT NULL,
		approval_mode TEXT NOT NULL,
		tokens_used INTEGER NOT NULL DEFAULT 0,
		has_user_event INTEGER NOT NULL DEFAULT 0,
		archived INTEGER NOT NULL DEFAULT 0,
		archived_at INTEGER,
		git_sha TEXT,
		git_branch TEXT,
		git_origin_url TEXT,
		cli_version TEXT NOT NULL DEFAULT '',
		first_user_message TEXT NOT NULL DEFAULT '',
		agent_nickname TEXT,
		agent_role TEXT,
		memory_mode TEXT NOT NULL DEFAULT 'enabled',
		model TEXT,
		reasoning_effort TEXT,
		agent_path TEXT,
		created_at_ms INTEGER,
		updated_at_ms INTEGER,
		` + threadSourceColumn + `
		preview TEXT NOT NULL DEFAULT ''
	)`); err != nil {
		t.Fatalf("failed to create provider sync threads table: %v", err)
	}
}

func insertProviderSyncThread(t *testing.T, dbPath string, values map[string]any) {
	t.Helper()
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to open test sqlite db: %v", err)
	}
	defer db.Close()
	columns, err := sqliteTableColumns(db, "threads")
	if err != nil {
		t.Fatalf("failed to inspect test sqlite db: %v", err)
	}
	var insertColumns []string
	var args []any
	for _, column := range columns {
		if value, ok := values[column]; ok {
			insertColumns = append(insertColumns, column)
			args = append(args, value)
		}
	}
	quoted := make([]string, len(insertColumns))
	for index, column := range insertColumns {
		quoted[index] = quoteSQLiteIdentifier(column)
	}
	if _, err := db.Exec("INSERT INTO threads ("+strings.Join(quoted, ", ")+") VALUES ("+sqlitePlaceholders(len(insertColumns))+")", args...); err != nil {
		t.Fatalf("failed to insert provider sync thread: %v", err)
	}
}

func providerSyncThreadRow(t *testing.T, dbPath, sessionID string) map[string]any {
	t.Helper()
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to open test sqlite db: %v", err)
	}
	defer db.Close()
	rows, err := querySessionSQLiteRows(db, "SELECT * FROM threads WHERE id = ?", sessionID)
	if err != nil {
		t.Fatalf("failed to query thread row: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected one thread row, got %d", len(rows))
	}
	return rows[0].Values
}

func buildTestRelayConfig(baseURL, apiKey string) string {
	return strings.Join([]string{
		`model_provider = "CodexPlusPlus"`,
		``,
		`[model_providers.CodexPlusPlus]`,
		`name = "CodexPlusPlus"`,
		`wire_api = "responses"`,
		`requires_openai_auth = true`,
		`base_url = "` + baseURL + `"`,
		`experimental_bearer_token = "` + apiKey + `"`,
		``,
	}, "\n")
}

func fakeChatGPTAuthJSON(t *testing.T, email string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]string{"email": email})
	if err != nil {
		t.Fatalf("failed to marshal token payload: %v", err)
	}
	token := "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
	data, err := json.MarshalIndent(map[string]any{
		"auth_mode": "chatgpt",
		"tokens": map[string]string{
			"id_token":      token,
			"refresh_token": "refresh-token",
		},
	}, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal auth json: %v", err)
	}
	return string(data) + "\n"
}

func TestSelectCodexMirrorAssetPrefersWindowsInstaller(t *testing.T) {
	asset, ok := selectCodexMirrorAsset([]codexAppMirrorAsset{
		{Name: "release-manifest.json", BrowserDownloadURL: "https://example.com/release-manifest.json"},
		{Name: "SHA256SUMS-windows.txt", BrowserDownloadURL: "https://example.com/SHA256SUMS-windows.txt"},
		{Name: "OpenAI.Codex_26.519.5221.0_x64__2p2nqsd0c76g0.Msix", BrowserDownloadURL: "https://example.com/OpenAI.Codex_26.519.5221.0_x64__2p2nqsd0c76g0.Msix"},
	}, "windows", "amd64")

	if !ok {
		t.Fatal("expected a windows asset")
	}
	if asset.Name != "OpenAI.Codex_26.519.5221.0_x64__2p2nqsd0c76g0.Msix" {
		t.Fatalf("selected wrong asset: %q", asset.Name)
	}
}

func TestSelectCodexMirrorAssetPrefersMacArchitecture(t *testing.T) {
	asset, ok := selectCodexMirrorAsset([]codexAppMirrorAsset{
		{Name: "Codex-mac-x64.dmg", BrowserDownloadURL: "https://example.com/Codex-mac-x64.dmg"},
		{Name: "Codex-mac-arm64.dmg", BrowserDownloadURL: "https://example.com/Codex-mac-arm64.dmg"},
	}, "darwin", "arm64")

	if !ok {
		t.Fatal("expected a macOS asset")
	}
	if asset.Name != "Codex-mac-arm64.dmg" {
		t.Fatalf("selected wrong asset: %q", asset.Name)
	}
}

func TestNormalizeCodexAppPathAcceptsWindowsExecutableAndAppDir(t *testing.T) {
	root := t.TempDir()
	appDir := filepath.Join(root, "OpenAI.Codex_1.2.3.0_x64__test", "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatalf("failed to create app dir: %v", err)
	}
	exe := filepath.Join(appDir, "Codex.exe")
	writeTestFile(t, exe, "binary")

	if got := normalizeCodexAppPath(exe); got != appDir {
		t.Fatalf("executable should normalize to app dir: %q", got)
	}
	if got := normalizeCodexAppPath(filepath.Dir(appDir)); got != appDir {
		t.Fatalf("package root should normalize to nested app dir: %q", got)
	}
	if got := normalizeCodexAppPath(appDir); got != appDir {
		t.Fatalf("app dir should stay app dir: %q", got)
	}
}

func TestPackagedWindowsAppUserModelIDMatchesOriginalLauncherShape(t *testing.T) {
	path := `C:\Program Files\WindowsApps\OpenAI.Codex_26.519.11010.0_x64__2p2nqsd0c76g0\app`

	if got := packagedWindowsAppUserModelID(path); got != "OpenAI.Codex_2p2nqsd0c76g0!App" {
		t.Fatalf("app user model id mismatch: %q", got)
	}
}

func TestPackagedWindowsAppUserModelIDIsCaseInsensitive(t *testing.T) {
	path := `C:\Program Files\WindowsApps\OpenAI.Codex_26.519.11010.0_x64__2p2nqsd0c76g0\app`
	mistypedCase := strings.Replace(path, "OpenAI.Codex_", "OpenAl.Codex_", 1)

	if got := packagedWindowsAppUserModelID(strings.ToLower(path)); got != "OpenAI.Codex_2p2nqsd0c76g0!App" {
		t.Fatalf("lowercase package path should still resolve app id: %q", got)
	}
	if got := packagedWindowsAppUserModelID(mistypedCase); got != "" {
		t.Fatalf("non Codex package identity should not resolve app id: %q", got)
	}
}

func TestWindowsPackagePathNormalizesToAppDirOnWindows(t *testing.T) {
	path := `C:\Program Files\WindowsApps\OpenAI.Codex_26.519.11010.0_x64__2p2nqsd0c76g0\app\Codex.exe`

	if runtime.GOOS != "windows" {
		if got := normalizeCodexAppPath(path); got != "" {
			t.Fatalf("Windows package paths should not normalize outside Windows: %q", got)
		}
		return
	}
	want := `C:\Program Files\WindowsApps\OpenAI.Codex_26.519.11010.0_x64__2p2nqsd0c76g0\app`
	if got := normalizeCodexAppPath(path); got != want {
		t.Fatalf("Windows package path should normalize to app dir: %q", got)
	}
}

func TestWindowsPackageShapeNormalizesWithoutReadableExecutable(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows package normalization only applies on Windows")
	}
	path := `C:\Program Files\WindowsApps\OpenAI.Codex_26.519.11010.0_x64__2p2nqsd0c76g0`
	want := `C:\Program Files\WindowsApps\OpenAI.Codex_26.519.11010.0_x64__2p2nqsd0c76g0\app`

	if got := normalizeCodexAppPath(path); got != want {
		t.Fatalf("package shape should normalize without file access: %q", got)
	}
}

func TestMissingWindowsExecutionAliasDoesNotNormalize(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("execution alias guard only applies on Windows")
	}
	path := filepath.Join(t.TempDir(), "Microsoft", "WindowsApps", "Codex.exe")

	if got := normalizeCodexAppPath(path); got != "" {
		t.Fatalf("missing Windows execution alias should not normalize: %q", got)
	}
}

func TestWindowsPlainDirectoryWithoutCodexExecutableDoesNotNormalize(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows directory normalization only applies on Windows")
	}
	dir := filepath.Join(t.TempDir(), "Codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("failed to create plain directory: %v", err)
	}

	if got := normalizeCodexAppPath(dir); got != "" {
		t.Fatalf("plain directory without Codex.exe should not normalize: %q", got)
	}
}

func TestBuildCodexExecutableRejectsWindowsPlainDirectory(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows executable lookup only applies on Windows")
	}
	dir := filepath.Join(t.TempDir(), "Codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("failed to create plain directory: %v", err)
	}

	if got := buildCodexExecutable(dir); got != "" {
		t.Fatalf("plain directory should not be treated as executable: %q", got)
	}
}

func TestBuildWindowsPackagedActivationArguments(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("packaged activation is only built on Windows")
	}
	path := `C:\Program Files\WindowsApps\OpenAI.Codex_26.519.11010.0_x64__2p2nqsd0c76g0\app`

	activation := buildWindowsPackagedActivation(path, 9229, []string{"--force_high_performance_gpu"})

	if activation == nil {
		t.Fatal("activation should be built")
	}
	if activation.appUserModelID != "OpenAI.Codex_2p2nqsd0c76g0!App" {
		t.Fatalf("app user model id mismatch: %q", activation.appUserModelID)
	}
	if activation.arguments != "--remote-debugging-port=9229 --remote-allow-origins=http://127.0.0.1:9229 --force_high_performance_gpu" {
		t.Fatalf("activation arguments mismatch: %q", activation.arguments)
	}
}

func TestProviderSyncRestoresThreadSourceForLegacyRows(t *testing.T) {
	home := t.TempDir()
	writeTestFile(t, filepath.Join(home, "config.toml"), `model_provider = "CodexPlusPlus"`+"\n")
	sessionID := "019a61dd-9748-7743-9ce9-92b8663a935b"
	rolloutPath := filepath.Join(home, "sessions", "2026", "05", "28", "rollout-"+sessionID+".jsonl")
	writeTestFile(t, rolloutPath, strings.Join([]string{
		testSessionRolloutLine(sessionID, "/project", "legacy title"),
		testRolloutResponseMessage("user", "修复历史对话"),
	}, "\n")+"\n")
	createProviderSyncThreadsTable(t, filepath.Join(home, "state_5.sqlite"), true)
	insertProviderSyncThread(t, filepath.Join(home, "state_5.sqlite"), map[string]any{
		"id":                 sessionID,
		"rollout_path":       rolloutPath,
		"created_at":         1779962400,
		"updated_at":         1779962500,
		"source":             "vscode",
		"model_provider":     "openai",
		"cwd":                "/project",
		"title":              "legacy title",
		"sandbox_policy":     `{"type":"danger-full-access"}`,
		"approval_mode":      "never",
		"tokens_used":        0,
		"has_user_event":     0,
		"archived":           0,
		"cli_version":        "",
		"first_user_message": "",
		"memory_mode":        "enabled",
		"created_at_ms":      1779962400000,
		"updated_at_ms":      1779962500000,
		"preview":            "",
	})

	result := runProviderSync(home)

	if result.Status != "synced" {
		t.Fatalf("sync should succeed: %#v", result)
	}
	if result.SQLiteRowsUpdated < 3 {
		t.Fatalf("sync should update provider, user flag, and thread source: %#v", result)
	}
	row := providerSyncThreadRow(t, filepath.Join(home, "state_5.sqlite"), sessionID)
	if got := stringFromAny(row["model_provider"]); got != "CodexPlusPlus" {
		t.Fatalf("provider mismatch: %q", got)
	}
	if got := stringFromAny(row["thread_source"]); got != "user" {
		t.Fatalf("thread_source should be restored, got %q", got)
	}
	if got := int64FromFlexible(row["has_user_event"]); got != 1 {
		t.Fatalf("has_user_event should be restored, got %#v", row["has_user_event"])
	}
}

func TestProviderSyncRestoresMissingThreadRowsFromRollouts(t *testing.T) {
	home := t.TempDir()
	writeTestFile(t, filepath.Join(home, "config.toml"), `model_provider = "CodexPlusPlus"`+"\n")
	sessionID := "019a61dd-9748-7743-9ce9-92b8663a935b"
	rolloutPath := filepath.Join(home, "sessions", "2026", "05", "28", "rollout-"+sessionID+".jsonl")
	writeTestFile(t, rolloutPath, strings.Join([]string{
		testSessionRolloutLine(sessionID, "/Users/test/project", ""),
		testRolloutResponseMessage("user", "# Context from my IDE setup:\n\n## My request for Codex:\n恢复这个历史项目会话"),
	}, "\n")+"\n")
	createProviderSyncThreadsTable(t, filepath.Join(home, "state_5.sqlite"), true)
	writeTestFile(t, filepath.Join(home, ".codex-global-state.json"), `{"electron-saved-workspace-roots":["/existing"],"project-order":["/existing"]}`+"\n")

	result := runProviderSync(home)

	if result.Status != "synced" {
		t.Fatalf("sync should succeed: %#v", result)
	}
	if result.SQLiteRowsUpdated == 0 {
		t.Fatalf("sync should insert missing thread row: %#v", result)
	}
	row := providerSyncThreadRow(t, filepath.Join(home, "state_5.sqlite"), sessionID)
	for key, expected := range map[string]string{
		"model_provider":     "CodexPlusPlus",
		"thread_source":      "user",
		"cwd":                "/Users/test/project",
		"title":              "恢复这个历史项目会话",
		"first_user_message": "恢复这个历史项目会话",
		"preview":            "恢复这个历史项目会话",
	} {
		if got := stringFromAny(row[key]); got != expected {
			t.Fatalf("%s mismatch: got %q want %q (row=%#v)", key, got, expected, row)
		}
	}
	var state map[string]any
	if err := readJSON(filepath.Join(home, ".codex-global-state.json"), &state); err != nil {
		t.Fatalf("global state should be readable: %v", err)
	}
	if !containsAnyString(state["electron-saved-workspace-roots"], "/Users/test/project") {
		t.Fatalf("workspace roots should include restored project: %#v", state)
	}
	hints, _ := state["thread-workspace-root-hints"].(map[string]any)
	if got := stringFromAny(hints[sessionID]); got != "/Users/test/project" {
		t.Fatalf("workspace hint mismatch: %q state=%#v", got, state)
	}
}

func TestCodexLaunchPayloadPrefersDirectExecutableWhenReadable(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows executable preference only applies on Windows")
	}
	appDir := filepath.Join(t.TempDir(), "OpenAI.Codex_26.519.11010.0_x64__2p2nqsd0c76g0", "app")
	exe := filepath.Join(appDir, "Codex.exe")
	writeTestFile(t, exe, "binary")

	payload := codexLaunchPayload(appDir)

	if got := stringFromAny(payload["method"]); got != "executable" {
		t.Fatalf("readable MSIX app dir should prefer direct executable launch: %#v", payload)
	}
	if got := stringFromAny(payload["executable"]); got != exe {
		t.Fatalf("executable mismatch: %q", got)
	}
}

func TestWindowsPackagedExplorerCommandShape(t *testing.T) {
	command := windowsPackagedExplorerCommand("OpenAI.Codex_abc!App", []string{"--remote-debugging-port=9229", "--remote-allow-origins=http://127.0.0.1:9229"})

	if len(command) != 2 {
		t.Fatalf("command length mismatch: %#v", command)
	}
	if command[0] != "explorer.exe" || command[1] != `shell:AppsFolder\OpenAI.Codex_abc!App` {
		t.Fatalf("command shape mismatch: %#v", command)
	}
	for _, part := range command {
		if strings.Contains(part, "127.0.0.1:9229") || strings.Contains(part, "--remote-debugging-port") {
			t.Fatalf("explorer fallback must not receive CDP arguments that can be opened as a URL: %#v", command)
		}
	}
}

func TestPackagedCodexDebugPortErrorGivesActionableGuidance(t *testing.T) {
	err := packagedCodexDebugPortError("OpenAI.Codex_abc!App", 9229, "explorer")
	message := err.Error()

	for _, expected := range []string{"Windows Store/MSIX", "调试端口 9229", "Codex.exe", "--remote-debugging-port"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("error should contain %q, got %q", expected, message)
		}
	}
}

func TestLaunchFailureDetailIncludesRecommendedAction(t *testing.T) {
	detail := launchFailureDetail(`C:\Program Files\WindowsApps\OpenAI.Codex_26.519.11010.0_x64__2p2nqsd0c76g0\app`, 9229, 57321, errors.New("no cdp"))

	if got := stringFromAny(detail["recommended_action"]); !strings.Contains(got, "Codex.exe") {
		t.Fatalf("recommended action should mention Codex.exe, got %q", got)
	}
	if got := stringFromAny(detail["error"]); got != "no cdp" {
		t.Fatalf("error mismatch: %q", got)
	}
	if got := stringFromAny(detail["codex_app"]); got == "" {
		t.Fatal("codex app should be included")
	}
}

func TestCodexLaunchPayloadUsesPackagedActivationShape(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("packaged activation is only used on Windows")
	}
	path := `C:\Program Files\WindowsApps\OpenAI.Codex_26.519.11010.0_x64__2p2nqsd0c76g0\app`

	payload := codexLaunchPayload(path)

	if !boolFromAny(payload["ready"]) {
		t.Fatalf("packaged app should be launch-ready: %#v", payload)
	}
	if got := stringFromAny(payload["method"]); got != "packaged_activation" {
		t.Fatalf("launch method mismatch: %q", got)
	}
	if got := stringFromAny(payload["appUserModelId"]); got != "OpenAI.Codex_2p2nqsd0c76g0!App" {
		t.Fatalf("app user model id mismatch: %q", got)
	}
}

func TestFindLatestWindowsCodexAppDirPrefersHighestVersion(t *testing.T) {
	root := t.TempDir()
	oldApp := filepath.Join(root, "OpenAI.Codex_1.2.3.0_x64__abc", "app")
	newApp := filepath.Join(root, "OpenAI.Codex_26.519.11010.0_x64__abc", "app")
	if err := os.MkdirAll(oldApp, 0o755); err != nil {
		t.Fatalf("failed to create old app dir: %v", err)
	}
	if err := os.MkdirAll(newApp, 0o755); err != nil {
		t.Fatalf("failed to create new app dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "OpenAI.Codex_not-a-version_x64__abc"), 0o755); err != nil {
		t.Fatalf("failed to create invalid app dir: %v", err)
	}

	if got := findLatestWindowsCodexAppDir(root); got != newApp {
		t.Fatalf("latest app dir mismatch: %q", got)
	}
}

func TestCompareVersionsHandlesSemverTags(t *testing.T) {
	if compareVersions("v1.1.13", "1.1.12") <= 0 {
		t.Fatal("v1.1.13 should be newer than 1.1.12")
	}
	if compareVersions("1.2.0", "1.10.0") >= 0 {
		t.Fatal("1.2.0 should be older than 1.10.0")
	}
	if compareVersions("CodexTools 1.1.12", "1.1.12") != 0 {
		t.Fatal("release name prefix should be ignored")
	}
}

func TestSelectCodexToolsAssetPrefersPlatformAndArchitecture(t *testing.T) {
	asset, ok := selectCodexToolsAsset([]codexAppMirrorAsset{
		{Name: "CodexTools-1.1.13-windows-x64.zip", BrowserDownloadURL: "https://example.com/windows.zip"},
		{Name: "CodexTools-1.1.13-macos-x64.zip", BrowserDownloadURL: "https://example.com/macos-x64.zip"},
		{Name: "CodexTools-1.1.13-macos-arm64.zip", BrowserDownloadURL: "https://example.com/macos-arm64.zip"},
		{Name: "SHA256SUMS.txt", BrowserDownloadURL: "https://example.com/SHA256SUMS.txt"},
	}, "darwin", "arm64")

	if !ok {
		t.Fatal("expected a matching CodexTools asset")
	}
	if asset.Name != "CodexTools-1.1.13-macos-arm64.zip" {
		t.Fatalf("selected wrong asset: %q", asset.Name)
	}
}

func TestSelectCodexToolsAssetPrefersMacOSInstaller(t *testing.T) {
	asset, ok := selectCodexToolsAsset([]codexAppMirrorAsset{
		{Name: "CodexTools-1.1.19-macos-arm64.zip", BrowserDownloadURL: "https://example.com/macos-arm64.zip"},
		{Name: "CodexTools-1.1.19-macos-arm64.pkg", BrowserDownloadURL: "https://example.com/macos-arm64.pkg"},
	}, "darwin", "arm64")

	if !ok {
		t.Fatal("expected a matching CodexTools asset")
	}
	if asset.Name != "CodexTools-1.1.19-macos-arm64.pkg" {
		t.Fatalf("selected wrong asset: %q", asset.Name)
	}
}

func TestSelectCodexToolsAssetPrefersWindowsSetup(t *testing.T) {
	asset, ok := selectCodexToolsAsset([]codexAppMirrorAsset{
		{Name: "CodexTools-1.1.13-windows-x64.zip", BrowserDownloadURL: "https://example.com/windows.zip"},
		{Name: "CodexTools-1.1.13-windows-x64-setup.exe", BrowserDownloadURL: "https://example.com/windows-setup.exe"},
	}, "windows", "amd64")

	if !ok {
		t.Fatal("expected a matching CodexTools asset")
	}
	if asset.Name != "CodexTools-1.1.13-windows-x64-setup.exe" {
		t.Fatalf("selected wrong asset: %q", asset.Name)
	}
}

func TestCodexToolsDownloadsAssetsParsesGitHubPagesLinks(t *testing.T) {
	assets := codexToolsDownloadsAssets(`
		<a href="./releases/CodexTools-1.1.26-macos-arm64.pkg">Apple Silicon</a>
		<a href="./releases/CodexTools-1.1.26-macos-arm64.zip">Apple Silicon zip</a>
		<a href="./releases/CodexTools-1.1.26-windows-x64-setup.exe">Windows</a>
		<a href="./releases/CodexTools-1.1.26-macos-arm64.pkg">Duplicate</a>
	`)

	if len(assets) != 3 {
		t.Fatalf("expected 3 unique assets, got %d: %#v", len(assets), assets)
	}
	if assets[0].Name != "CodexTools-1.1.26-macos-arm64.pkg" {
		t.Fatalf("asset name mismatch: %q", assets[0].Name)
	}
	if assets[0].BrowserDownloadURL != codexToolsPagesBaseURL+"releases/CodexTools-1.1.26-macos-arm64.pkg" {
		t.Fatalf("absolute download URL mismatch: %q", assets[0].BrowserDownloadURL)
	}
	asset, ok := selectCodexToolsAsset(assets, "darwin", "arm64")
	if !ok {
		t.Fatal("expected Apple Silicon asset from downloads page")
	}
	if latest := codexToolsAssetVersion(asset.Name); latest != "1.1.26" {
		t.Fatalf("selected version mismatch: %q", latest)
	}
}

func TestCodexToolsDownloadsAssetsSelectsMacOSInstallerFromFallbackPage(t *testing.T) {
	assets := codexToolsDownloadsAssets(`
		<a href="./releases/CodexTools-1.1.27-macos-arm64.zip">Portable Apple Silicon zip</a>
		<a href="./releases/CodexTools-1.1.27-macos-arm64.pkg">Download Apple Silicon installer</a>
		<a href="./releases/CodexTools-1.1.27-macos-x64.pkg">Download Intel installer</a>
	`)

	asset, ok := selectCodexToolsAsset(assets, "darwin", "arm64")
	if !ok {
		t.Fatal("expected a matching macOS asset")
	}
	if asset.Name != "CodexTools-1.1.27-macos-arm64.pkg" {
		t.Fatalf("selected wrong fallback asset: %q", asset.Name)
	}
}

func TestSelectCodexToolsAssetDoesNotCrossArchitectures(t *testing.T) {
	assets := []codexAppMirrorAsset{
		{Name: "CodexTools-1.1.26-macos-arm64.pkg", BrowserDownloadURL: "https://example.com/macos-arm64.pkg"},
		{Name: "CodexTools-1.1.25-macos-x64.pkg", BrowserDownloadURL: "https://example.com/macos-x64.pkg"},
	}

	asset, ok := selectCodexToolsAsset(assets, "darwin", "amd64")
	if !ok {
		t.Fatal("expected an Intel macOS asset")
	}
	if asset.Name != "CodexTools-1.1.25-macos-x64.pkg" {
		t.Fatalf("selected cross-architecture asset: %q", asset.Name)
	}
}

func TestPickCDPPageTargetPrefersCodexAppPage(t *testing.T) {
	target, err := pickCDPPageTarget([]cdpTarget{
		{ID: "worker", Type: "worker", WebSocketDebuggerURL: "ws://127.0.0.1:9229/devtools/page/worker"},
		{ID: "blank-page", Type: "page", URL: "about:blank", WebSocketDebuggerURL: "ws://127.0.0.1:9229/devtools/page/blank"},
		{ID: "codex-page", Type: "page", URL: "app://-/index.html", Title: "Codex", WebSocketDebuggerURL: "ws://127.0.0.1:9229/devtools/page/codex"},
	})

	if err != nil {
		t.Fatalf("pickCDPPageTarget returned error: %v", err)
	}
	if target.ID != "codex-page" {
		t.Fatalf("selected wrong target: %q", target.ID)
	}
}

func TestPickCDPPageTargetFallsBackToFirstPage(t *testing.T) {
	target, err := pickCDPPageTarget([]cdpTarget{
		{ID: "worker", Type: "worker", WebSocketDebuggerURL: "ws://127.0.0.1:9229/devtools/page/worker"},
		{ID: "page", Type: "page", URL: "https://example.com", WebSocketDebuggerURL: "ws://127.0.0.1:9229/devtools/page/page"},
	})

	if err != nil {
		t.Fatalf("pickCDPPageTarget returned error: %v", err)
	}
	if target.ID != "page" {
		t.Fatalf("selected wrong fallback target: %q", target.ID)
	}
}

func TestDecideRelayRouteKeepsToolDeclarationOnTextRoute(t *testing.T) {
	body := []byte(`{"model":"gpt-test","input":"hi","tools":[{"type":"web_search"},{"type":"image_generation"}]}`)
	decision := decideRelayRoute(body, relayProfile{
		ImageGenerationEnabled:        true,
		ImageGenerationUseSeparateAPI: true,
		ImageGenerationBaseURL:        "https://image.example/v1",
		ImageGenerationAPIKey:         "image-key",
	})

	if decision.useImageAPI {
		t.Fatal("plain text with image tool declaration should use text relay")
	}
	if !decision.strippedImageTool {
		t.Fatal("image_generation tool should be stripped from text relay requests")
	}
	if hasImageGenerationTool(t, decision.body) {
		t.Fatal("stripped request body still contains image_generation tool")
	}
}

func TestRelayProxyBaseURLAddsV1ForBareResponsesHost(t *testing.T) {
	baseURL := relayProxyBaseURL("https://api.example.com/", "responses")

	if baseURL != "https://api.example.com/v1" {
		t.Fatalf("responses relay should append /v1 for bare hosts, got %q", baseURL)
	}
}

func TestRelayProxyBaseURLKeepsExistingResponsesPath(t *testing.T) {
	baseURL := relayProxyBaseURL("https://api.example.com/openai/", "responses")

	if baseURL != "https://api.example.com/openai" {
		t.Fatalf("responses relay should preserve existing paths, got %q", baseURL)
	}
}

func TestSetRelayProxyUserAgentForwardsCodexClientAgent(t *testing.T) {
	source := http.Header{"User-Agent": []string{"codex-cli/1.2.3"}}
	target := http.Header{"User-Agent": []string{"CodexPlusPlus-GoRelay/" + version}}

	setRelayProxyUserAgent(source, target)

	if got := target.Get("User-Agent"); got != "codex-cli/1.2.3" {
		t.Fatalf("relay proxy should forward Codex user agent, got %q", got)
	}
}

func TestSetRelayProxyUserAgentFallsBackToCodex(t *testing.T) {
	target := http.Header{"User-Agent": []string{"CodexPlusPlus-GoRelay/" + version}}

	setRelayProxyUserAgent(http.Header{}, target)

	if got := target.Get("User-Agent"); got != "Codex" {
		t.Fatalf("relay proxy should not expose GoRelay user agent, got %q", got)
	}
}

func TestCopyProxyHeadersSkipsAcceptEncoding(t *testing.T) {
	source := http.Header{
		"Accept-Encoding": []string{"gzip, br"},
		"Content-Type":    []string{"application/json"},
	}
	target := http.Header{}

	copyProxyHeaders(source, target)

	if got := target.Get("Accept-Encoding"); got != "" {
		t.Fatalf("relay proxy should control upstream encoding, got %q", got)
	}
	if got := target.Get("Content-Type"); got != "application/json" {
		t.Fatalf("content type should still be copied, got %q", got)
	}
}

func TestRelayTestPayloadNormalizesResponsesBaseURL(t *testing.T) {
	endpoint, _ := relayTestPayload(relayProfile{BaseURL: "https://api.example.com", Protocol: "responses"}, "gpt-test")

	if endpoint != "https://api.example.com/v1/responses" {
		t.Fatalf("relay test should use normalized responses endpoint, got %q", endpoint)
	}
}

func TestDecideRelayRouteUsesImageForExplicitToolChoice(t *testing.T) {
	body := []byte(`{"model":"gpt-test","input":"make it","tools":[{"type":"image_generation"}],"tool_choice":{"type":"image_generation"}}`)
	decision := decideRelayRoute(body, relayProfile{
		ImageGenerationEnabled:        true,
		ImageGenerationUseSeparateAPI: true,
		ImageGenerationBaseURL:        "https://image.example/v1",
		ImageGenerationAPIKey:         "image-key",
	})

	if !decision.useImageAPI {
		t.Fatal("explicit image_generation tool_choice should use image relay")
	}
	if decision.strippedImageTool {
		t.Fatal("image relay requests should keep image_generation tool")
	}
	if !hasImageGenerationTool(t, decision.body) {
		t.Fatal("image relay request lost image_generation tool")
	}
}

func TestDecideRelayRouteUsesImageForChineseImageIntent(t *testing.T) {
	body := []byte(`{"model":"gpt-test","input":"帮我生成一个猫猫图标","tools":[{"type":"image_generation"}]}`)
	decision := decideRelayRoute(body, relayProfile{
		ImageGenerationEnabled:        true,
		ImageGenerationUseSeparateAPI: true,
		ImageGenerationBaseURL:        "https://image.example/v1",
		ImageGenerationAPIKey:         "image-key",
	})

	if !decision.useImageAPI {
		t.Fatal("clear Chinese image generation intent should use image relay")
	}
	if decision.reason != "latest_user_image_intent" {
		t.Fatalf("unexpected reason: %q", decision.reason)
	}
}

func TestDecideRelayRouteIgnoresOlderImageIntentHistory(t *testing.T) {
	body := []byte(`{"model":"gpt-test","input":[{"role":"user","content":"帮我生成一个猫猫图标"},{"role":"assistant","content":"好的"},{"role":"user","content":"检查图片中转逻辑 / 图标中转配置"}],"tools":[{"type":"image_generation"}]}`)
	decision := decideRelayRoute(body, relayProfile{
		ImageGenerationEnabled:        true,
		ImageGenerationUseSeparateAPI: true,
		ImageGenerationBaseURL:        "https://image.example/v1",
		ImageGenerationAPIKey:         "image-key",
	})

	if decision.useImageAPI {
		t.Fatal("older image intent in history should not route latest text request to image relay")
	}
	if decision.keySource != "default" {
		t.Fatalf("text route should use default key, got %q", decision.keySource)
	}
}

func TestDecideRelayRouteDoesNotUseImageForRelayConfigDiscussion(t *testing.T) {
	body := []byte(`{"model":"gpt-test","input":"检查图片中转逻辑 / 图标中转配置","tools":[{"type":"image_generation"}]}`)
	decision := decideRelayRoute(body, relayProfile{
		ImageGenerationEnabled:        true,
		ImageGenerationUseSeparateAPI: true,
		ImageGenerationBaseURL:        "https://image.example/v1",
		ImageGenerationAPIKey:         "image-key",
	})

	if decision.useImageAPI {
		t.Fatal("discussion about image relay config should not use image relay")
	}
	if !decision.strippedImageTool {
		t.Fatal("image_generation tool should be stripped for config discussion")
	}
}

func TestDecideRelayRouteStripsImageToolWhenImageDisabled(t *testing.T) {
	body := []byte(`{"model":"gpt-test","input":"帮我生成一个猫猫图标","tools":[{"type":"image_generation"}],"tool_choice":{"type":"image_generation"}}`)
	decision := decideRelayRoute(body, relayProfile{
		ImageGenerationEnabled:        false,
		ImageGenerationUseSeparateAPI: true,
		ImageGenerationBaseURL:        "https://image.example/v1",
		ImageGenerationAPIKey:         "image-key",
	})

	if decision.useImageAPI {
		t.Fatal("disabled image generation should always use text relay")
	}
	if !decision.strippedImageTool {
		t.Fatal("disabled image generation should strip image_generation tool")
	}
	if hasImageGenerationTool(t, decision.body) {
		t.Fatal("disabled image generation request still contains image_generation tool")
	}
	if hasToolChoice(t, decision.body) {
		t.Fatal("disabled image generation request still contains image tool_choice")
	}
}

func hasImageGenerationTool(t *testing.T, body []byte) bool {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatalf("failed to unmarshal body: %v", err)
	}
	tools, _ := value["tools"].([]any)
	for _, tool := range tools {
		if object, ok := tool.(map[string]any); ok && object["type"] == "image_generation" {
			return true
		}
	}
	return false
}

func hasToolChoice(t *testing.T, body []byte) bool {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatalf("failed to unmarshal body: %v", err)
	}
	_, ok := value["tool_choice"]
	return ok
}
