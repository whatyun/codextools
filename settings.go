package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if fileExists(filepath.Join(dir, "go.mod")) && fileExists(filepath.Join(dir, "web", "package.json")) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("unable to locate codextools repository root")
		}
		dir = parent
	}
}

func managerDistFS(root string) (fs.FS, string, error) {
	if root != "" {
		dist := filepath.Join(root, "web", "dist")
		if fileExists(filepath.Join(dist, "index.html")) {
			return os.DirFS(dist), dist, nil
		}
	}
	dist, err := fs.Sub(embeddedDist, "web/dist")
	if err == nil {
		if _, statErr := fs.Stat(dist, "index.html"); statErr == nil {
			return dist, "embedded:web/dist", nil
		}
	}
	if root != "" {
		return nil, filepath.Join(root, "web", "dist"), fmt.Errorf("未找到前端构建产物 %s，请先运行 npm --prefix web run vite:build 并重新构建下载包", filepath.Join(root, "web", "dist"))
	}
	return nil, "embedded:web/dist", errors.New("内嵌前端资源缺失，请先运行 npm --prefix web run vite:build 后重新执行 go build")
}

func stateDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return stateDirName
	}
	return filepath.Join(home, stateDirName)
}

func defaultInstallRoot() string {
	switch runtime.GOOS {
	case "darwin":
		return "/Applications"
	case "windows":
		if userProfile := os.Getenv("USERPROFILE"); userProfile != "" {
			return filepath.Join(userProfile, "Desktop")
		}
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "."
	}
	return filepath.Join(home, "Desktop")
}

func settingsPath() string {
	return filepath.Join(stateDir(), settingsFileName)
}

func latestStatusPath() string {
	return filepath.Join(stateDir(), latestStatusFileName)
}

func diagnosticLogPath() string {
	return filepath.Join(stateDir(), diagnosticLogFileName)
}

func codexHomeDir() string {
	if custom := strings.TrimSpace(os.Getenv("CODEX_HOME")); custom != "" {
		expanded := filepath.Clean(os.ExpandEnv(custom))
		if isDir(expanded) {
			return expanded
		}
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ".codex"
	}
	return filepath.Join(home, ".codex")
}

func defaultSettings() backendSettings {
	return backendSettings{
		CodexExtraArgs:                  []string{},
		Language:                        defaultLanguage,
		ProviderSyncSavedProviders:      []string{},
		ProviderSyncManualProviders:     []string{},
		RelayProfilesEnabled:            true,
		Enhancements:                    true,
		CodexAppPluginAutoExpand:        true,
		CodexAppPluginMarketplaceUnlock: true,
		CodexAppForcePluginInstall:      true,
		CodexAppModelWhitelistUnlock:    true,
		CodexAppSessionDelete:           true,
		CodexAppMarkdownExport:          true,
		CodexAppForceChineseLocale:      true,
		CodexAppFastStartup:             true,
		CodexAppProjectMove:             true,
		CodexAppThreadScrollRestore:     true,
		CodexAppZedRemoteOpen:           true,
		CodexAppUpstreamWorktreeCreate:  true,
		CodexAppNativeMenuPlacement:     true,
		CodexAppNativeMenuLocalization:  true,
		ZedRemoteOpenStrategy:           "addToFocusedWorkspace",
		ZedRemoteProjectRegistryEnabled: true,
		CodexAppImageOverlayOpacity:     35,
		LaunchMode:                      "patch",
		RelayProfiles:                   []relayProfile{defaultRelayProfile()},
		AggregateRelayProfiles:          []aggregateRelayProfile{},
		ActiveRelayID:                   "default",
		RelayTestModel:                  defaultRelayTestModel,
		CLIWrapperAPIKeyEnv:             defaultAPIKeyEnvironment,
	}
}

func defaultRelayProfile() relayProfile {
	return relayProfile{
		ID:                          "default",
		Name:                        "默认中转",
		Protocol:                    "responses",
		RelayMode:                   "official",
		UseCommonConfig:             true,
		ContextSelection:            relayContextSelection{},
		ContextSelectionInitialized: true,
		ModelInsertMode:             "patch",
	}
}

func loadSettings() backendSettings {
	settings := defaultSettings()
	data, err := os.ReadFile(settingsPath())
	if err != nil {
		return settings
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return defaultSettings()
	}
	settings = normalizeSettings(settings)
	if migrated, changed := migrateOfficialAuthBinding(settings); changed {
		settings = migrated
		_ = atomicWriteSettingsPreservingUnknown(settingsPath(), settings)
	}
	return settings
}

func normalizeSettings(settings backendSettings) backendSettings {
	if settings.CodexExtraArgs == nil {
		settings.CodexExtraArgs = []string{}
	}
	if settings.ProviderSyncSavedProviders == nil {
		settings.ProviderSyncSavedProviders = []string{}
	}
	if settings.ProviderSyncManualProviders == nil {
		settings.ProviderSyncManualProviders = []string{}
	}
	settings.Language = normalizeLanguage(settings.Language)
	settings = normalizeDefaultEnabledSettings(settings)
	settings.ZedRemoteOpenStrategy = normalizeZedOpenStrategy(settings.ZedRemoteOpenStrategy)
	if settings.CodexAppImageOverlayOpacity <= 0 {
		settings.CodexAppImageOverlayOpacity = 35
	}
	if settings.CodexAppImageOverlayOpacity > 100 {
		settings.CodexAppImageOverlayOpacity = 100
	}
	if settings.CodexAppImageOverlayOpacity < 1 {
		settings.CodexAppImageOverlayOpacity = 1
	}
	settings.CodexAppImageOverlayPath = strings.TrimSpace(settings.CodexAppImageOverlayPath)
	settings.MobileControlRelayURL = strings.TrimSpace(settings.MobileControlRelayURL)
	settings.MobileControlRoom = strings.TrimSpace(settings.MobileControlRoom)
	settings.MobileControlKey = strings.TrimSpace(settings.MobileControlKey)
	common, extractedContext := splitContextConfigSections(settings.RelayCommonConfigContents)
	settings.RelayCommonConfigContents = normalizeConfigText(common)
	settings.RelayContextConfigContents = normalizeConfigText(joinConfigSections(settings.RelayContextConfigContents, extractedContext))
	if settings.LaunchMode != "patch" && settings.LaunchMode != "relay" {
		settings.LaunchMode = "patch"
	}
	if len(settings.RelayProfiles) == 0 {
		settings.RelayProfiles = []relayProfile{defaultRelayProfile()}
	}
	for index := range settings.RelayProfiles {
		if settings.RelayProfiles[index].ID == "" {
			settings.RelayProfiles[index].ID = fmt.Sprintf("relay-%d", index+1)
		}
		if settings.RelayProfiles[index].Protocol == "" {
			settings.RelayProfiles[index].Protocol = "responses"
		}
		if settings.RelayProfiles[index].Name == "" {
			settings.RelayProfiles[index].Name = settings.RelayProfiles[index].ID
		}
		if settings.RelayProfiles[index].UpstreamBaseURL == "" {
			settings.RelayProfiles[index].UpstreamBaseURL = settings.RelayProfiles[index].BaseURL
		}
		if settings.RelayProfiles[index].ModelInsertMode == "" {
			settings.RelayProfiles[index].ModelInsertMode = "patch"
		}
		settings.RelayProfiles[index].ModelList, settings.RelayProfiles[index].ModelWindows = normalizeModelListAndWindows(settings.RelayProfiles[index].ModelList, settings.RelayProfiles[index].ModelWindows)
		if !settings.RelayProfiles[index].ContextSelectionInitialized {
			settings.RelayProfiles[index].ContextSelection = contextSelectionForAllEntries(settings.RelayContextConfigContents)
			settings.RelayProfiles[index].ContextSelectionInitialized = true
		}
		settings.RelayProfiles[index].ContextSelection = normalizeContextSelection(settings.RelayProfiles[index].ContextSelection)
		switch settings.RelayProfiles[index].RelayMode {
		case "mixedApi":
			settings.RelayProfiles[index].OfficialMixAPIKey = true
		case "pureApi":
			settings.RelayProfiles[index].OfficialMixAPIKey = false
		case "aggregate":
			settings.RelayProfiles[index].OfficialMixAPIKey = false
		case "official":
			if settings.RelayProfiles[index].OfficialMixAPIKey {
				settings.RelayProfiles[index].RelayMode = "mixedApi"
			}
		default:
			if settings.RelayProfiles[index].OfficialMixAPIKey {
				settings.RelayProfiles[index].RelayMode = "mixedApi"
			} else {
				settings.RelayProfiles[index].RelayMode = "official"
				settings.RelayProfiles[index].OfficialMixAPIKey = false
			}
		}
		if strings.TrimSpace(settings.RelayProfiles[index].AuthContents) != "" {
			settings.RelayProfiles[index] = syncOfficialAuthMetadataFromAuth(settings.RelayProfiles[index])
		} else if strings.TrimSpace(settings.RelayProfiles[index].OfficialAuthContents) == "" {
			settings.RelayProfiles[index].OfficialAccountLabel = ""
			settings.RelayProfiles[index].OfficialAuthUpdatedAt = ""
		} else if settings.RelayProfiles[index].OfficialAccountLabel == "" {
			status := chatGPTAuthStatusFromContents(settings.RelayProfiles[index].OfficialAuthContents, "settings")
			settings.RelayProfiles[index].OfficialAccountLabel = status.AccountLabel
		}
	}
	if settings.ActiveRelayID == "" {
		settings.ActiveRelayID = settings.RelayProfiles[0].ID
	}
	if settings.AggregateRelayProfiles == nil {
		settings.AggregateRelayProfiles = []aggregateRelayProfile{}
	}
	for index := range settings.AggregateRelayProfiles {
		settings.AggregateRelayProfiles[index] = normalizeAggregateRelayProfile(settings.AggregateRelayProfiles[index], index)
	}
	if settings.ActiveAggregateRelayID == "" && activeRelayProfile(settings).RelayMode == "aggregate" {
		settings.ActiveAggregateRelayID = settings.ActiveRelayID
	}
	if settings.RelayTestModel == "" {
		settings.RelayTestModel = defaultRelayTestModel
	}
	if settings.CLIWrapperAPIKeyEnv == "" {
		settings.CLIWrapperAPIKeyEnv = defaultAPIKeyEnvironment
	}
	if !settings.OnboardingCompleted {
		settings.OnboardingCompletedAt = ""
		settings.OnboardingCompletedPlatform = ""
	} else if settings.OnboardingCompletedPlatform == "" {
		settings.OnboardingCompletedPlatform = runtime.GOOS
	}
	return settings
}

func normalizeAggregateRelayProfile(profile aggregateRelayProfile, index int) aggregateRelayProfile {
	profile.ID = strings.TrimSpace(profile.ID)
	if profile.ID == "" {
		profile.ID = fmt.Sprintf("aggregate-%d", index+1)
	}
	profile.Name = strings.TrimSpace(profile.Name)
	if profile.Name == "" {
		profile.Name = profile.ID
	}
	profile.Strategy = normalizeAggregateRelayStrategy(profile.Strategy)
	if profile.Members == nil {
		profile.Members = []aggregateRelayMember{}
	}
	for memberIndex := range profile.Members {
		profile.Members[memberIndex].RelayID = strings.TrimSpace(profile.Members[memberIndex].RelayID)
		if profile.Members[memberIndex].Weight <= 0 {
			profile.Members[memberIndex].Weight = 1
		}
	}
	return profile
}

func normalizeAggregateRelayStrategy(value string) string {
	switch strings.TrimSpace(value) {
	case "failover", "conversationRoundRobin", "requestRoundRobin", "weightedRoundRobin":
		return strings.TrimSpace(value)
	default:
		return "failover"
	}
}

func normalizeDefaultEnabledSettings(settings backendSettings) backendSettings {
	defaults := defaultEnabledSettingsFromRaw()
	if !settings.RelayProfilesEnabled && !defaults["relayProfilesEnabled"] {
		settings.RelayProfilesEnabled = true
	}
	if !settings.Enhancements && !defaults["enhancementsEnabled"] {
		settings.Enhancements = true
	}
	if !settings.CodexAppPluginAutoExpand && !defaults["codexAppPluginAutoExpand"] {
		settings.CodexAppPluginAutoExpand = true
	}
	if !settings.CodexAppPluginMarketplaceUnlock && !defaults["codexAppPluginMarketplaceUnlock"] {
		settings.CodexAppPluginMarketplaceUnlock = true
	}
	if !settings.CodexAppForcePluginInstall && !defaults["codexAppForcePluginInstall"] {
		settings.CodexAppForcePluginInstall = true
	}
	if !settings.CodexAppModelWhitelistUnlock && !defaults["codexAppModelWhitelistUnlock"] {
		settings.CodexAppModelWhitelistUnlock = true
	}
	if !settings.CodexAppSessionDelete && !defaults["codexAppSessionDelete"] {
		settings.CodexAppSessionDelete = true
	}
	if !settings.CodexAppMarkdownExport && !defaults["codexAppMarkdownExport"] {
		settings.CodexAppMarkdownExport = true
	}
	if !settings.CodexAppForceChineseLocale && !defaults["codexAppForceChineseLocale"] {
		settings.CodexAppForceChineseLocale = true
	}
	if !settings.CodexAppFastStartup && !defaults["codexAppFastStartup"] {
		settings.CodexAppFastStartup = true
	}
	if !settings.CodexAppProjectMove && !defaults["codexAppProjectMove"] {
		settings.CodexAppProjectMove = true
	}
	if !settings.CodexAppThreadScrollRestore && !defaults["codexAppThreadScrollRestore"] {
		settings.CodexAppThreadScrollRestore = true
	}
	if !settings.CodexAppZedRemoteOpen && !defaults["codexAppZedRemoteOpen"] {
		settings.CodexAppZedRemoteOpen = true
	}
	if !settings.CodexAppUpstreamWorktreeCreate && !defaults["codexAppUpstreamWorktreeCreate"] {
		settings.CodexAppUpstreamWorktreeCreate = true
	}
	if !settings.CodexAppNativeMenuPlacement && !defaults["codexAppNativeMenuPlacement"] {
		settings.CodexAppNativeMenuPlacement = true
	}
	if !settings.CodexAppNativeMenuLocalization && !defaults["codexAppNativeMenuLocalization"] {
		settings.CodexAppNativeMenuLocalization = true
	}
	if !settings.ZedRemoteProjectRegistryEnabled && !defaults["zedRemoteProjectRegistryEnabled"] {
		settings.ZedRemoteProjectRegistryEnabled = true
	}
	return settings
}

func normalizeZedOpenStrategy(value string) string {
	switch strings.TrimSpace(value) {
	case "reuseWindow", "newWindow", "default", "addToFocusedWorkspace":
		return strings.TrimSpace(value)
	default:
		return "addToFocusedWorkspace"
	}
}

func defaultEnabledSettingsFromRaw() map[string]bool {
	data, err := os.ReadFile(settingsPath())
	if err != nil {
		return map[string]bool{}
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return map[string]bool{}
	}
	out := map[string]bool{}
	for _, key := range []string{
		"relayProfilesEnabled",
		"enhancementsEnabled",
		"codexAppPluginAutoExpand",
		"codexAppPluginMarketplaceUnlock",
		"codexAppForcePluginInstall",
		"codexAppModelWhitelistUnlock",
		"codexAppSessionDelete",
		"codexAppMarkdownExport",
		"codexAppPasteFix",
		"codexAppForceChineseLocale",
		"codexAppFastStartup",
		"codexAppProjectMove",
		"codexAppThreadIdBadge",
		"codexAppThreadScrollRestore",
		"codexAppZedRemoteOpen",
		"codexAppUpstreamWorktreeCreate",
		"codexAppNativeMenuPlacement",
		"codexAppNativeMenuLocalization",
		"zedRemoteProjectRegistryEnabled",
	} {
		_, out[key] = raw[key]
	}
	return out
}

func migrateOfficialAuthBinding(settings backendSettings) (backendSettings, bool) {
	activeID := activeRelayProfile(settings).ID
	currentConfig := readFile(filepath.Join(codexHomeDir(), "config.toml"))
	currentAuth := readFile(filepath.Join(codexHomeDir(), "auth.json"))
	changed := false
	for index := range settings.RelayProfiles {
		profile := settings.RelayProfiles[index]
		if profile.ID == activeID && relayProfileUsesLiveFiles(profile) && strings.TrimSpace(profile.ConfigContents) == "" && strings.TrimSpace(currentConfig) != "" {
			settings.RelayProfiles[index].ConfigContents = currentConfig
			changed = true
		}
		if profile.ID == activeID {
			if strings.TrimSpace(profile.AuthContents) == "" && strings.TrimSpace(profile.OfficialAuthContents) == "" && strings.TrimSpace(currentAuth) != "" {
				settings.RelayProfiles[index].AuthContents = currentAuth
				settings.RelayProfiles[index].OfficialAuthUpdatedAt = timeNowRFC3339()
				changed = true
			}
		}
		if strings.TrimSpace(settings.RelayProfiles[index].AuthContents) != "" {
			normalized := syncOfficialAuthMetadataFromAuth(settings.RelayProfiles[index])
			if normalized.AuthContents != settings.RelayProfiles[index].AuthContents ||
				normalized.OfficialAuthContents != settings.RelayProfiles[index].OfficialAuthContents ||
				normalized.OfficialAccountLabel != settings.RelayProfiles[index].OfficialAccountLabel ||
				normalized.OfficialAuthUpdatedAt != settings.RelayProfiles[index].OfficialAuthUpdatedAt {
				settings.RelayProfiles[index] = normalized
				changed = true
			}
		}
	}
	return settings, changed
}

func relayProfileUsesLiveFiles(profile relayProfile) bool {
	return profile.RelayMode != "official" || profile.OfficialMixAPIKey
}

func timeNowRFC3339() string {
	return time.Now().Format(time.RFC3339)
}

func normalizeLanguage(language string) string {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "", "zh", "zh-cn", "zh_cn", "cn", "chinese":
		return "zh-CN"
	case "en", "en-us", "en_us", "english":
		return "en-US"
	case "ko", "ko-kr", "ko_kr", "kr", "korean":
		return "ko-KR"
	case "ja", "ja-jp", "ja_jp", "jp", "japanese":
		return "ja-JP"
	default:
		return defaultLanguage
	}
}

func saveSettings(settings backendSettings) error {
	settings = normalizeSettings(settings)
	settings.CodexExtraArgs = normalizeExtraArgs(settings.CodexExtraArgs)
	if settings.CodexAppPath != "" {
		if normalized := normalizeCodexAppPath(settings.CodexAppPath); normalized != "" {
			settings.CodexAppPath = normalized
		} else if runtime.GOOS == "windows" {
			settings.CodexAppPath = ""
		}
	}
	return atomicWriteSettingsPreservingUnknown(settingsPath(), settings)
}

func atomicWriteSettingsPreservingUnknown(path string, settings backendSettings) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	var next map[string]json.RawMessage
	if err := json.Unmarshal(data, &next); err != nil {
		return err
	}
	var merged map[string]json.RawMessage
	if existing, err := os.ReadFile(path); err == nil && len(existing) > 0 {
		_ = json.Unmarshal(existing, &merged)
	}
	if merged == nil {
		merged = map[string]json.RawMessage{}
	}
	for key, value := range next {
		merged[key] = value
	}
	out, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, out)
}

func normalizeExtraArgs(args []string) []string {
	var normalized []string
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg != "" {
			normalized = append(normalized, arg)
		}
	}
	if normalized == nil {
		return []string{}
	}
	return normalized
}

func atomicWriteJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, data)
}

func atomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return replaceFile(tmp, path)
}

func settingsPayload(message string) commandResult {
	return ok(message, settingsPayloadValue(loadSettings()))
}

func settingsPayloadValue(settings backendSettings) map[string]any {
	return map[string]any{
		"settings":              settings,
		"settings_path":         settingsPath(),
		"user_scripts":          userScriptInventoryValue(),
		"envConflicts":          detectEnvConflicts(),
		"pendingProviderImport": pendingProviderImportPayload(),
	}
}

func pendingProviderImportPayload() any {
	request, err := loadPendingProviderImport()
	if err != nil || request == nil {
		return nil
	}
	return request
}

func (s *server) saveSettings(args map[string]any) commandResult {
	var settings backendSettings
	if err := remarshal(args["settings"], &settings); err != nil {
		return failed("保存设置失败："+err.Error(), settingsPayloadValue(loadSettings()))
	}
	if err := saveSettings(settings); err != nil {
		return failed("保存设置失败："+err.Error(), settingsPayloadValue(normalizeSettings(settings)))
	}
	return settingsPayload("设置已保存。")
}

func (s *server) checkEnvConflicts() commandResult {
	payload := settingsPayloadValue(loadSettings())
	payload["envConflicts"] = detectEnvConflicts()
	return ok("环境变量冲突已检测。", payload)
}

func (s *server) removeEnvConflicts(args map[string]any) commandResult {
	names := stringSliceArg(args, "names")
	if len(names) == 0 {
		for _, conflict := range detectEnvConflicts() {
			names = append(names, conflict.Name)
		}
	}
	removal, err := removeEnvConflicts(names, filepath.Join(stateDir(), "env-conflict-backups"))
	payload := settingsPayloadValue(loadSettings())
	if err != nil {
		return failed("环境变量清理失败："+err.Error(), payload)
	}
	payload["envConflictRemoval"] = removal
	payload["envConflicts"] = detectEnvConflicts()
	return ok("环境变量冲突已清理；如 Codex 已在运行，请重启 Codex。", payload)
}
