package main

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

func (s *server) syncProvidersNow() commandResult {
	result := runProviderSync(codexHomeDir())
	repairResult := repairCodexConfig(codexHomeDir(), codexConfigRepairOptions{Plugins: true})
	payload := map[string]any{
		"syncStatus":          result.Status,
		"targetProvider":      result.TargetProvider,
		"changedSessionFiles": result.ChangedSessionFiles,
		"sqliteRowsUpdated":   result.SQLiteRowsUpdated,
		"backupDir":           result.BackupDir,
		"syncMessage":         result.Message,
	}
	status := "ok"
	if result.Status == "skipped" {
		status = "not_checked"
	}
	if repairResult.Status == "failed" {
		status = "failed"
	}
	message := fmt.Sprintf("供应商已同步一次：%d 个会话文件，%d 行索引。%s", result.ChangedSessionFiles, result.SQLiteRowsUpdated, providerSyncExtraMessage(result.Message))
	if strings.TrimSpace(repairResult.Message) != "" {
		message = strings.TrimSpace(message + " " + repairResult.Message)
	}
	return commandResult{
		"status":              status,
		"message":             message,
		"syncStatus":          payload["syncStatus"],
		"targetProvider":      payload["targetProvider"],
		"changedSessionFiles": payload["changedSessionFiles"],
		"sqliteRowsUpdated":   payload["sqliteRowsUpdated"],
		"backupDir":           payload["backupDir"],
		"syncMessage":         payload["syncMessage"],
		"pluginCount":         repairResult.PluginCount,
		"marketplaceCount":    repairResult.MarketplaceCount,
		"pluginBackupPath":    repairResult.BackupPath,
	}
}

func (s *server) repairCodexPlugins() commandResult {
	result := repairCodexConfig(codexHomeDir(), codexConfigRepairOptions{Plugins: true})
	status := "ok"
	if result.Status == "failed" {
		status = "failed"
	}
	return commandResult{
		"status":           status,
		"message":          result.Message,
		"backupPath":       result.BackupPath,
		"pluginCount":      result.PluginCount,
		"marketplaceCount": result.MarketplaceCount,
		"mcpServerCount":   result.MCPServerCount,
		"configChanged":    result.PluginConfigChanged,
		"goalsEnabled":     result.GoalsEnabled,
		"configPath":       filepath.Join(codexHomeDir(), "config.toml"),
		"codexHome":        codexHomeDir(),
	}
}

func (s *server) repairCodexGoals() commandResult {
	result := repairCodexConfig(codexHomeDir(), codexConfigRepairOptions{Goals: true})
	status := "ok"
	if result.Status == "failed" {
		status = "failed"
	}
	return commandResult{
		"status":        status,
		"message":       result.Message,
		"backupPath":    result.BackupPath,
		"goalsEnabled":  result.GoalsEnabled,
		"configChanged": result.GoalsConfigChanged,
		"configPath":    filepath.Join(codexHomeDir(), "config.toml"),
		"codexHome":     codexHomeDir(),
	}
}

func providerSyncExtraMessage(message string) string {
	message = strings.TrimSpace(message)
	if message == "" || message == "Provider sync complete" || message == "Provider sync already up to date" {
		return ""
	}
	return message
}

func repairCodexConfig(home string, options codexConfigRepairOptions) codexConfigRepairResult {
	if !isDir(home) {
		return codexConfigRepairResult{Status: "failed", Message: "Codex home 不存在：" + home}
	}
	configPath := filepath.Join(home, "config.toml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return codexConfigRepairResult{Status: "failed", Message: "读取 config.toml 失败：" + err.Error()}
	}
	original := string(data)
	updated := original
	result := codexConfigRepairResult{Status: "ok"}
	if options.Plugins {
		var pluginCount, marketplaceCount, mcpCount int
		updated, pluginCount, marketplaceCount, mcpCount = repairCodexPluginConfig(home, updated)
		result.PluginCount = pluginCount
		result.MarketplaceCount = marketplaceCount
		result.MCPServerCount = mcpCount
		result.PluginConfigChanged = updated != original
	}
	beforeGoals := updated
	if options.Goals {
		updated = repairCodexGoalsConfig(updated)
		result.GoalsEnabled = true
		result.GoalsConfigChanged = updated != beforeGoals
	}
	if updated != original {
		backupPath, err := writeCodexConfigWithBackup(configPath, updated, "config-repair")
		if err != nil {
			return codexConfigRepairResult{Status: "failed", Message: "写入 config.toml 失败：" + err.Error(), BackupPath: backupPath}
		}
		result.BackupPath = backupPath
	}
	result.Message = codexConfigRepairMessage(result, options, updated != original)
	return result
}

func repairCodexPluginConfig(home, contents string) (string, int, int, int) {
	updated := contents
	marketplaces := discoverCodexMarketplaces(home)
	for _, marketplace := range marketplaces {
		if strings.TrimSpace(marketplace.Source) == "" {
			continue
		}
		if !hasTable(updated, "marketplaces."+marketplace.Name) {
			updated = appendTomlBlock(updated, []string{
				"[marketplaces." + marketplace.Name + "]",
				"last_updated = " + quoteToml(time.Now().UTC().Format(time.RFC3339)),
				`source_type = "local"`,
				"source = " + quoteToml(marketplace.Source),
			})
		}
	}
	plugins := discoverCachedPluginEnables(home)
	for _, plugin := range plugins {
		table := fmt.Sprintf("plugins.%s", quoteToml(plugin.Name+"@"+plugin.Marketplace))
		if !hasTable(updated, table) {
			updated = appendTomlBlock(updated, []string{
				"[" + table + "]",
				"enabled = true",
			})
		}
	}
	updated, mcpCount := repairNodeReplMCPConfig(home, updated)
	return updated, len(plugins), len(marketplaces), mcpCount
}

func repairCodexGoalsConfig(contents string) string {
	return upsertTableKey(contents, "features", "goals", "true")
}

func codexConfigRepairMessage(result codexConfigRepairResult, options codexConfigRepairOptions, changed bool) string {
	var parts []string
	if options.Plugins {
		if result.PluginCount == 0 {
			parts = append(parts, "未发现可恢复的插件缓存")
		} else if result.PluginConfigChanged {
			parts = append(parts, fmt.Sprintf("已恢复插件配置：%d 个插件、%d 个市场源", result.PluginCount, result.MarketplaceCount))
		} else {
			parts = append(parts, fmt.Sprintf("插件配置已完整：%d 个插件、%d 个市场源", result.PluginCount, result.MarketplaceCount))
		}
	}
	if options.Goals {
		if result.GoalsConfigChanged {
			parts = append(parts, "已开启追求目标功能 features.goals")
		} else {
			parts = append(parts, "追求目标功能已开启")
		}
	}
	if changed && result.BackupPath != nil {
		parts = append(parts, "已备份原配置："+*result.BackupPath)
	}
	if len(parts) == 0 {
		return "没有需要修复的配置。"
	}
	return strings.Join(parts, "；") + "。"
}

func backupCodexConfig(configPath, label string) (string, error) {
	backupPath := fmt.Sprintf("%s.before-%s-%s.bak", configPath, label, time.Now().Format("20060102150405"))
	if fileExists(backupPath) {
		for index := 2; ; index++ {
			candidate := fmt.Sprintf("%s.before-%s-%s-%d.bak", configPath, label, time.Now().Format("20060102150405"), index)
			if !fileExists(candidate) {
				backupPath = candidate
				break
			}
		}
	}
	return backupPath, copyFileIfExists(configPath, backupPath)
}

func discoverCodexMarketplaces(home string) []marketplaceSpec {
	paths := []marketplaceSpec{
		{Name: "openai-bundled", Source: filepath.Join(home, ".tmp", "bundled-marketplaces", "openai-bundled")},
		{Name: "openai-curated", Source: filepath.Join(home, ".tmp", "plugins")},
	}
	if userHome, err := os.UserHomeDir(); err == nil && userHome != "" {
		paths = append(paths, marketplaceSpec{Name: "openai-primary-runtime", Source: filepath.Join(userHome, ".cache", "codex-runtimes", "codex-primary-runtime", "plugins", "openai-primary-runtime")})
	}
	var marketplaces []marketplaceSpec
	for _, marketplace := range paths {
		if codexMarketplaceExists(marketplace.Source) {
			marketplaces = append(marketplaces, marketplace)
		}
	}
	return marketplaces
}

func codexMarketplaceExists(path string) bool {
	if !isDir(path) {
		return false
	}
	return fileExists(filepath.Join(path, ".agents", "plugins", "marketplace.json")) || isDir(filepath.Join(path, "plugins"))
}

func discoverCachedPluginEnables(home string) []pluginEnableSpec {
	cacheRoot := filepath.Join(home, "plugins", "cache")
	marketplaces := []string{"openai-curated", "openai-primary-runtime", "openai-bundled"}
	var plugins []pluginEnableSpec
	seen := map[string]bool{}
	for _, marketplace := range marketplaces {
		root := filepath.Join(cacheRoot, marketplace)
		if !isDir(root) {
			continue
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			name := entry.Name()
			if !cachedPluginExists(filepath.Join(root, name)) {
				continue
			}
			key := name + "@" + marketplace
			if seen[key] {
				continue
			}
			seen[key] = true
			plugins = append(plugins, pluginEnableSpec{Name: name, Marketplace: marketplace})
		}
	}
	sort.Slice(plugins, func(i, j int) bool {
		left := plugins[i].Marketplace + "/" + plugins[i].Name
		right := plugins[j].Marketplace + "/" + plugins[j].Name
		return left < right
	})
	return plugins
}

func cachedPluginExists(path string) bool {
	if fileExists(filepath.Join(path, ".codex-plugin", "plugin.json")) {
		return true
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() && fileExists(filepath.Join(path, entry.Name(), ".codex-plugin", "plugin.json")) {
			return true
		}
	}
	return false
}

func repairNodeReplMCPConfig(home, contents string) (string, int) {
	resourcesDir := codexResourcesDir()
	nodeReplPath := filepath.Join(resourcesDir, "node_repl")
	nodePath := filepath.Join(resourcesDir, "node")
	codexCLIPath := filepath.Join(resourcesDir, "codex")
	if !fileExists(nodeReplPath) || !fileExists(nodePath) {
		return contents, 0
	}
	updated := removeTable(removeTable(contents, "mcp_servers.node_repl"), "mcp_servers.node_repl.env")
	lines := []string{
		"[mcp_servers.node_repl]",
		"args = []",
		"command = " + quoteToml(nodeReplPath),
		"startup_timeout_sec = 120",
		"",
		"[mcp_servers.node_repl.env]",
		`BROWSER_USE_AVAILABLE_BACKENDS = "chrome,iab"`,
		`BROWSER_USE_MARKETPLACE_NAME = "openai-bundled"`,
		"CODEX_CLI_PATH = " + quoteToml(codexCLIPath),
		"CODEX_HOME = " + quoteToml(home),
		`NODE_REPL_NATIVE_PIPE_CONNECT_TIMEOUT_MS = "1000"`,
		`NODE_REPL_NODE_MODULE_DIRS = ""`,
		"NODE_REPL_NODE_PATH = " + quoteToml(nodePath),
	}
	if hashes := trustedBrowserClientHashes(home); len(hashes) > 0 {
		lines = append(lines, "NODE_REPL_TRUSTED_BROWSER_CLIENT_SHA256S = "+quoteToml(strings.Join(hashes, ",")))
	}
	lines = append(lines,
		"NODE_REPL_TRUSTED_CODE_PATHS = "+quoteToml(home),
		`NODE_REPL_UNTRUSTED_ENV_ALLOWLIST = "BROWSER_USE_MARKETPLACE_NAME"`,
	)
	if servicePath := bundledComputerUseServicePath(home); servicePath != "" {
		lines = append(lines, "SKY_CUA_SERVICE_PATH = "+quoteToml(servicePath))
	}
	return appendTomlBlock(updated, lines), 1
}

func trustedBrowserClientHashes(home string) []string {
	var hashes []string
	seen := map[string]bool{}
	pattern := filepath.Join(home, "plugins", "cache", "openai-bundled", "browser", "*", "scripts", "browser-client.mjs")
	matches, _ := filepath.Glob(pattern)
	sort.Strings(matches)
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		sum := sha256.Sum256(data)
		hash := hex.EncodeToString(sum[:])
		if !seen[hash] {
			seen[hash] = true
			hashes = append(hashes, hash)
		}
	}
	return hashes
}

func bundledComputerUseServicePath(home string) string {
	candidates := []string{
		filepath.Join(home, ".tmp", "bundled-marketplaces", "openai-bundled", "plugins", "computer-use", "Codex Computer Use.app"),
	}
	matches, _ := filepath.Glob(filepath.Join(home, "plugins", "cache", "openai-bundled", "computer-use", "*", "Codex Computer Use.app"))
	sort.Strings(matches)
	candidates = append(candidates, matches...)
	for i := len(candidates) - 1; i >= 0; i-- {
		if isDir(candidates[i]) {
			return candidates[i]
		}
	}
	return ""
}

func codexResourcesDir() string {
	if path := resolveCodexApp(loadSettings().CodexAppPath); path != "" && runtime.GOOS == "darwin" && strings.EqualFold(filepath.Ext(path), ".app") {
		return filepath.Join(path, "Contents", "Resources")
	}
	if path := resolveCodexApp(loadSettings().CodexAppPath); path != "" && runtime.GOOS == "windows" {
		if resources := filepath.Join(path, "resources"); isDir(resources) {
			return resources
		}
		if resources := filepath.Join(path, "app", "resources"); isDir(resources) {
			return resources
		}
	}
	if runtime.GOOS == "darwin" && isDir("/Applications/Codex.app/Contents/Resources") {
		return "/Applications/Codex.app/Contents/Resources"
	}
	return filepath.Dir(companionBinaryPath("codex"))
}

func appendTomlBlock(contents string, lines []string) string {
	trimmedLines := append([]string{}, lines...)
	for len(trimmedLines) > 0 && strings.TrimSpace(trimmedLines[len(trimmedLines)-1]) == "" {
		trimmedLines = trimmedLines[:len(trimmedLines)-1]
	}
	if len(trimmedLines) == 0 {
		return ensureTrailingNewline(contents)
	}
	updated := strings.TrimRight(contents, "\n")
	if strings.TrimSpace(updated) != "" {
		updated += "\n\n"
	}
	updated += strings.Join(trimmedLines, "\n")
	return ensureTrailingNewline(updated)
}

func hasTable(contents, table string) bool {
	header := "[" + table + "]"
	for _, line := range splitLines(contents) {
		if strings.TrimSpace(line) == header {
			return true
		}
	}
	return false
}

func upsertTableKey(contents, table, key, value string) string {
	lines := splitLines(contents)
	header := "[" + table + "]"
	tableStart := -1
	tableEnd := len(lines)
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			if tableStart >= 0 {
				tableEnd = i
				break
			}
			if trimmed == header {
				tableStart = i
			}
		}
	}
	if tableStart < 0 {
		return appendTomlBlock(contents, []string{header, key + " = " + value})
	}
	for i := tableStart + 1; i < tableEnd; i++ {
		if rootLineKey(lines[i]) == key {
			lines[i] = key + " = " + value
			return ensureTrailingNewline(strings.Join(lines, "\n"))
		}
	}
	lines = append(lines[:tableEnd], append([]string{key + " = " + value}, lines[tableEnd:]...)...)
	return ensureTrailingNewline(strings.Join(lines, "\n"))
}

func runProviderSync(home string) providerSyncResult {
	if !isDir(home) {
		return providerSyncResult{Status: "skipped", Message: "Codex home not found: " + home, TargetProvider: "openai"}
	}
	targetProvider := readCurrentProvider(filepath.Join(home, "config.toml"))
	lockDir := filepath.Join(home, "tmp", "provider-sync.lock")
	if err := os.MkdirAll(filepath.Dir(lockDir), 0o755); err != nil {
		return providerSyncResult{Status: "skipped", Message: "Provider sync skipped: " + err.Error(), TargetProvider: targetProvider}
	}
	if err := os.Mkdir(lockDir, 0o755); err != nil {
		return providerSyncResult{Status: "skipped", Message: "Provider sync lock exists: " + lockDir, TargetProvider: targetProvider}
	}
	defer os.RemoveAll(lockDir)
	_ = os.WriteFile(filepath.Join(lockDir, "owner.json"), []byte(fmt.Sprintf(`{"pid":%d,"startedAt":%d}`, os.Getpid(), time.Now().Unix())), 0o644)

	changes, err := collectSessionChanges(home, targetProvider)
	if err != nil {
		return providerSyncResult{Status: "skipped", Message: "Provider sync skipped: " + err.Error(), TargetProvider: targetProvider}
	}
	var rewriteChanges []sessionChange
	for _, change := range changes {
		if change.RewriteNeeded {
			rewriteChanges = append(rewriteChanges, change)
		}
	}
	sqliteCount := countSQLiteUpdates(filepath.Join(home, "state_5.sqlite"), targetProvider, changes)
	globalCount := countGlobalStateUpdates(filepath.Join(home, ".codex-global-state.json"), changes)
	if len(rewriteChanges) == 0 && sqliteCount == 0 && globalCount == 0 {
		return providerSyncResult{Status: "synced", Message: "Provider sync already up to date", TargetProvider: targetProvider}
	}
	backupDir, err := createProviderSyncBackup(home, targetProvider, rewriteChanges)
	if err != nil {
		return providerSyncResult{Status: "skipped", Message: "Provider sync skipped: " + err.Error(), TargetProvider: targetProvider}
	}
	if err := applySessionChanges(rewriteChanges); err != nil {
		return providerSyncResult{Status: "skipped", Message: "Provider sync skipped: " + err.Error(), TargetProvider: targetProvider, BackupDir: &backupDir}
	}
	sqliteRows, sqliteErr := applySQLiteUpdates(filepath.Join(home, "state_5.sqlite"), targetProvider, changes)
	if sqliteErr != nil {
		_ = restoreSessionChanges(rewriteChanges)
		return providerSyncResult{Status: "skipped", Message: "Provider sync skipped: " + sqliteErr.Error(), TargetProvider: targetProvider, BackupDir: &backupDir}
	}
	if _, err := applyGlobalStateUpdate(filepath.Join(home, ".codex-global-state.json"), changes); err != nil {
		_ = restoreSessionChanges(rewriteChanges)
		return providerSyncResult{Status: "skipped", Message: "Provider sync skipped: " + err.Error(), TargetProvider: targetProvider, BackupDir: &backupDir}
	}
	pruneProviderSyncBackups(home)
	return providerSyncResult{Status: "synced", Message: "Provider sync complete", TargetProvider: targetProvider, BackupDir: &backupDir, ChangedSessionFiles: len(rewriteChanges), SQLiteRowsUpdated: sqliteRows}
}

func readCurrentProvider(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "openai"
	}
	provider := rootKeyString(string(data), "model_provider")
	if provider == "" {
		return "openai"
	}
	return provider
}

func collectSessionChanges(home, targetProvider string) ([]sessionChange, error) {
	var files []string
	for _, dirname := range []string{"sessions", "archived_sessions"} {
		root := filepath.Join(home, dirname)
		if !isDir(root) {
			continue
		}
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			name := entry.Name()
			if strings.HasPrefix(name, "rollout-") && strings.HasSuffix(name, ".jsonl") {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(files)
	changes := make([]sessionChange, 0, len(files))
	for _, path := range files {
		textBytes, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		text := string(textBytes)
		firstLine, separator := splitFirstLine(text)
		if strings.TrimSpace(firstLine) == "" {
			continue
		}
		var record map[string]any
		if json.Unmarshal([]byte(firstLine), &record) != nil {
			continue
		}
		payload, ok := record["payload"].(map[string]any)
		if !ok {
			continue
		}
		threadID := stringFromAny(payload["id"])
		cwd := toDesktopWorkspacePath(stringFromAny(payload["cwd"]))
		rewriteNeeded := stringFromAny(payload["model_provider"]) != targetProvider
		if rewriteNeeded {
			payload["model_provider"] = targetProvider
		}
		nextFirstLine := firstLine
		if rewriteNeeded {
			data, err := json.Marshal(record)
			if err != nil {
				return nil, err
			}
			nextFirstLine = string(data)
		}
		meta := sessionChangeMetadata(path, text, record, payload)
		changes = append(changes, sessionChange{
			Path: path, OriginalFirstLine: firstLine, NextFirstLine: nextFirstLine, Separator: separator,
			ThreadID: threadID, CWD: firstString(cwd, meta.CWD), Source: meta.Source, Title: meta.Title, FirstUserMessage: meta.FirstUserMessage, Preview: meta.Preview,
			CreatedAt: meta.CreatedAt, UpdatedAt: meta.UpdatedAt, CreatedAtMs: meta.CreatedAtMs, UpdatedAtMs: meta.UpdatedAtMs,
			Archived: meta.Archived, CLIVersion: meta.CLIVersion, Model: meta.Model, ReasoningEffort: meta.ReasoningEffort,
			SandboxPolicy: meta.SandboxPolicy, ApprovalMode: meta.ApprovalMode,
			HasUserEvent: meta.HasUserEvent, RewriteNeeded: rewriteNeeded,
		})
	}
	return changes, nil
}

func sessionChangeMetadata(path, text string, record, payload map[string]any) sessionChange {
	createdAtMs := timestampMsFromAny(firstString(payload["timestamp"], record["timestamp"]))
	if createdAtMs == 0 {
		createdAtMs = uuidV7TimestampMs(stringFromAny(payload["id"]))
	}
	updatedAtMs := timestampMsFromAny(firstString(record["timestamp"], payload["timestamp"]))
	meta := sessionChange{
		Source:          firstString(payload["source"], payload["originator"], "vscode"),
		CLIVersion:      stringFromAny(payload["cli_version"]),
		CreatedAtMs:     createdAtMs,
		UpdatedAtMs:     updatedAtMs,
		CreatedAt:       timestampMsToSeconds(createdAtMs),
		UpdatedAt:       timestampMsToSeconds(updatedAtMs),
		Archived:        strings.Contains(filepath.ToSlash(path), "/archived_sessions/"),
		HasUserEvent:    strings.Contains(text, `"user_message"`) || strings.Contains(text, `"user_input"`),
		SandboxPolicy:   `{"type":"danger-full-access"}`,
		ApprovalMode:    "never",
		ReasoningEffort: "",
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var item map[string]any
		if json.Unmarshal([]byte(line), &item) != nil {
			continue
		}
		itemPayload, _ := item["payload"].(map[string]any)
		if itemPayload == nil {
			itemPayload = item
		}
		if ms := timestampMsFromAny(firstString(item["timestamp"], itemPayload["timestamp"])); ms > meta.UpdatedAtMs {
			meta.UpdatedAtMs = ms
			meta.UpdatedAt = timestampMsToSeconds(ms)
		}
		if stringFromAny(item["type"]) == "turn_context" {
			if cwd := toDesktopWorkspacePath(stringFromAny(itemPayload["cwd"])); cwd != "" && meta.CWD == "" {
				meta.CWD = cwd
			}
			if model := stringFromAny(itemPayload["model"]); model != "" {
				meta.Model = model
			}
			if effort := stringFromAny(itemPayload["effort"]); effort != "" {
				meta.ReasoningEffort = effort
			}
			if approval := stringFromAny(itemPayload["approval_policy"]); approval != "" {
				meta.ApprovalMode = approval
			}
			if sandbox := compactJSONOrString(itemPayload["sandbox_policy"]); sandbox != "" {
				meta.SandboxPolicy = sandbox
			}
		}
		message := markdownMessageFromRolloutRecord(item)
		if message.Role != "user" {
			continue
		}
		text := sessionDisplayText(message.Text)
		if text == "" || strings.HasPrefix(text, "<environment_context>") {
			continue
		}
		meta.HasUserEvent = true
		if meta.FirstUserMessage == "" {
			meta.FirstUserMessage = text
			meta.Title = sessionTitleFromMessage(text)
			meta.Preview = sessionPreviewFromMessage(text)
		}
	}
	if meta.UpdatedAtMs == 0 {
		if info, err := os.Stat(path); err == nil {
			meta.UpdatedAtMs = info.ModTime().UnixMilli()
			meta.UpdatedAt = timestampMsToSeconds(meta.UpdatedAtMs)
		}
	}
	if meta.UpdatedAtMs == 0 {
		meta.UpdatedAtMs = meta.CreatedAtMs
		meta.UpdatedAt = meta.CreatedAt
	}
	if meta.Title == "" {
		meta.Title = firstString(payload["title"], record["title"], meta.FirstUserMessage, stringFromAny(payload["id"]))
	}
	if meta.Preview == "" {
		meta.Preview = firstString(meta.FirstUserMessage, meta.Title)
	}
	meta.Title = truncateRunes(meta.Title, 120)
	meta.Preview = truncateRunes(meta.Preview, 240)
	return meta
}

func timestampMsToSeconds(value int64) int64 {
	if value <= 0 {
		return 0
	}
	return value / 1000
}

func compactJSONOrString(value any) string {
	if value == nil {
		return ""
	}
	data, err := json.Marshal(value)
	if err != nil {
		return stringFromAny(value)
	}
	return string(data)
}

func sessionDisplayText(value string) string {
	value = strings.ReplaceAll(value, "\r", "\n")
	lines := strings.Split(value, "\n")
	var out []string
	skipContext := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# Context from my IDE setup:") {
			continue
		}
		if strings.HasPrefix(trimmed, "## My request for Codex:") {
			continue
		}
		if strings.HasPrefix(trimmed, "<environment_context>") {
			skipContext = true
			continue
		}
		if skipContext {
			if strings.HasPrefix(trimmed, "</environment_context>") {
				skipContext = false
			}
			continue
		}
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return strings.Join(out, " ")
}

func sessionTitleFromMessage(value string) string {
	value = normalizedSessionTitle(value)
	if value == "" {
		return ""
	}
	return truncateRunes(value, 80)
}

func sessionPreviewFromMessage(value string) string {
	return truncateRunes(normalizedSessionTitle(value), 180)
}

func truncateRunes(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return strings.TrimSpace(string(runes[:limit]))
}

func splitFirstLine(text string) (string, string) {
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		return text[:index], text[index:]
	}
	return text, ""
}

func toDesktopWorkspacePath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, `\\?\unc\`) {
		return `\\` + strings.ReplaceAll(value[8:], "/", `\`)
	}
	if strings.HasPrefix(lower, `\\?\`) {
		return strings.ReplaceAll(value[4:], `\`, "/")
	}
	return value
}

func createProviderSyncBackup(home, targetProvider string, changes []sessionChange) (string, error) {
	root := filepath.Join(home, "backups_state", "provider-sync")
	name := time.Now().Format("20060102150405")
	backupDir := filepath.Join(root, name)
	for suffix := 0; fileExists(backupDir); suffix++ {
		backupDir = filepath.Join(root, fmt.Sprintf("%s-%d", name, suffix+1))
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return "", err
	}
	for _, name := range []string{"config.toml", ".codex-global-state.json", ".codex-global-state.json.bak"} {
		_ = copyFileIfExists(filepath.Join(home, name), filepath.Join(backupDir, name))
	}
	dbDir := filepath.Join(backupDir, "db")
	for _, name := range []string{"state_5.sqlite", "state_5.sqlite-wal", "state_5.sqlite-shm"} {
		if fileExists(filepath.Join(home, name)) {
			_ = os.MkdirAll(dbDir, 0o755)
			_ = copyFileIfExists(filepath.Join(home, name), filepath.Join(dbDir, name))
		}
	}
	manifest := make([]map[string]any, 0, len(changes))
	for _, change := range changes {
		manifest = append(manifest, map[string]any{"path": change.Path, "originalFirstLine": change.OriginalFirstLine, "separator": change.Separator})
	}
	if err := atomicWriteJSON(filepath.Join(backupDir, "session-meta-backup.json"), manifest); err != nil {
		return "", err
	}
	if err := atomicWriteJSON(filepath.Join(backupDir, "metadata.json"), map[string]any{"managedBy": "Codex++ provider sync", "targetProvider": targetProvider}); err != nil {
		return "", err
	}
	return backupDir, nil
}

func copyFileIfExists(source, target string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, data, 0o644)
}

func applySessionChanges(changes []sessionChange) error {
	for _, change := range changes {
		if err := os.WriteFile(change.Path, []byte(change.NextFirstLine+change.Separator), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func restoreSessionChanges(changes []sessionChange) error {
	for _, change := range changes {
		if err := os.WriteFile(change.Path, []byte(change.OriginalFirstLine+change.Separator), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func countSQLiteUpdates(path, targetProvider string, changes []sessionChange) int {
	if !fileExists(path) || !sqliteHasColumn(path, "threads", "id") {
		return 0
	}
	count, _ := sqliteScalarInt(path, "SELECT COUNT(*) FROM threads WHERE COALESCE(model_provider, '') <> ?", targetProvider)
	if sqliteHasColumn(path, "threads", "thread_source") {
		count += countSQLiteThreadSourceUpdates(path, changes)
	}
	if sqliteHasColumn(path, "threads", "has_user_event") {
		for _, change := range changes {
			if change.HasUserEvent && change.ThreadID != "" {
				n, _ := sqliteScalarInt(path, "SELECT COUNT(*) FROM threads WHERE id = ? AND COALESCE(has_user_event, 0) <> 1", change.ThreadID)
				count += n
			}
		}
	}
	if sqliteHasColumn(path, "threads", "cwd") {
		for _, change := range changes {
			if change.ThreadID != "" && change.CWD != "" {
				n, _ := sqliteScalarInt(path, "SELECT COUNT(*) FROM threads WHERE id = ? AND COALESCE(cwd, '') <> ?", change.ThreadID, change.CWD)
				count += n
			}
		}
	}
	count += countMissingSQLiteThreads(path, changes)
	return count
}

func countSQLiteThreadSourceUpdates(path string, changes []sessionChange) int {
	seen := map[string]bool{}
	count := 0
	for _, change := range changes {
		if !change.HasUserEvent || change.ThreadID == "" || seen[change.ThreadID] {
			continue
		}
		seen[change.ThreadID] = true
		n, _ := sqliteScalarInt(path, "SELECT COUNT(*) FROM threads WHERE id = ? AND COALESCE(thread_source, '') = ''", change.ThreadID)
		count += n
	}
	return count
}

func countMissingSQLiteThreads(path string, changes []sessionChange) int {
	seen := map[string]bool{}
	count := 0
	for _, change := range changes {
		if change.ThreadID == "" || !change.HasUserEvent || seen[change.ThreadID] {
			continue
		}
		seen[change.ThreadID] = true
		n, _ := sqliteScalarInt(path, "SELECT COUNT(*) FROM threads WHERE id = ?", change.ThreadID)
		if n == 0 {
			count++
		}
	}
	return count
}

func applySQLiteUpdates(path, targetProvider string, changes []sessionChange) (int, error) {
	if !fileExists(path) || !sqliteHasColumn(path, "threads", "id") {
		return 0, nil
	}
	providerRows := 0
	if sqliteHasColumn(path, "threads", "model_provider") {
		rows, err := sqliteExecRows(path, "UPDATE threads SET model_provider = ? WHERE COALESCE(model_provider, '') <> ?", targetProvider, targetProvider)
		providerRows = rows
		if err != nil {
			return 0, err
		}
	}
	totalRows := providerRows
	if sqliteHasColumn(path, "threads", "thread_source") {
		for _, change := range changes {
			if !change.HasUserEvent || change.ThreadID == "" {
				continue
			}
			rows, err := sqliteExecRows(path, "UPDATE threads SET thread_source = 'user' WHERE id = ? AND COALESCE(thread_source, '') = ''", change.ThreadID)
			totalRows += rows
			if err != nil {
				return totalRows, err
			}
		}
	}
	if sqliteHasColumn(path, "threads", "has_user_event") {
		for _, change := range changes {
			if !change.HasUserEvent || change.ThreadID == "" {
				continue
			}
			rows, err := sqliteExecRows(path, "UPDATE threads SET has_user_event = 1 WHERE id = ? AND COALESCE(has_user_event, 0) <> 1", change.ThreadID)
			totalRows += rows
			if err != nil {
				return totalRows, err
			}
		}
	}
	if sqliteHasColumn(path, "threads", "cwd") {
		for _, change := range changes {
			if change.ThreadID == "" || change.CWD == "" {
				continue
			}
			rows, err := sqliteExecRows(path, "UPDATE threads SET cwd = ? WHERE id = ? AND COALESCE(cwd, '') <> ?", change.CWD, change.ThreadID, change.CWD)
			totalRows += rows
			if err != nil {
				return totalRows, err
			}
		}
	}
	inserted, err := insertMissingSQLiteThreads(path, targetProvider, changes)
	totalRows += inserted
	return totalRows, err
}

func insertMissingSQLiteThreads(path, targetProvider string, changes []sessionChange) (int, error) {
	db, err := openSQLite(path)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	columns, err := sqliteTableColumns(db, "threads")
	if err != nil || len(columns) == 0 || !containsString(columns, "id") {
		return 0, err
	}
	columnSet := map[string]bool{}
	for _, column := range columns {
		columnSet[column] = true
	}
	inserted := 0
	seen := map[string]bool{}
	for _, change := range changes {
		if change.ThreadID == "" || !change.HasUserEvent || seen[change.ThreadID] {
			continue
		}
		seen[change.ThreadID] = true
		var exists int
		if err := db.QueryRow("SELECT COUNT(*) FROM threads WHERE id = ?", change.ThreadID).Scan(&exists); err != nil {
			return inserted, err
		}
		if exists > 0 {
			continue
		}
		row := sqliteThreadRowFromChange(change, targetProvider)
		var insertColumns []string
		var args []any
		for _, column := range columns {
			if !columnSet[column] {
				continue
			}
			if value, ok := row[column]; ok {
				insertColumns = append(insertColumns, column)
				args = append(args, value)
			}
		}
		if len(insertColumns) == 0 {
			continue
		}
		quoted := make([]string, len(insertColumns))
		for index, column := range insertColumns {
			quoted[index] = quoteSQLiteIdentifier(column)
		}
		query := "INSERT INTO threads (" + strings.Join(quoted, ", ") + ") VALUES (" + sqlitePlaceholders(len(insertColumns)) + ")"
		if _, err := db.Exec(query, args...); err != nil {
			return inserted, err
		}
		inserted++
	}
	return inserted, nil
}

func sqliteThreadRowFromChange(change sessionChange, targetProvider string) map[string]any {
	createdAtMs := change.CreatedAtMs
	if createdAtMs == 0 {
		createdAtMs = uuidV7TimestampMs(change.ThreadID)
	}
	updatedAtMs := change.UpdatedAtMs
	if updatedAtMs == 0 {
		updatedAtMs = createdAtMs
	}
	createdAt := change.CreatedAt
	if createdAt == 0 {
		createdAt = timestampMsToSeconds(createdAtMs)
	}
	updatedAt := change.UpdatedAt
	if updatedAt == 0 {
		updatedAt = timestampMsToSeconds(updatedAtMs)
	}
	title := firstString(change.Title, change.FirstUserMessage, change.ThreadID)
	preview := firstString(change.Preview, change.FirstUserMessage, title)
	threadSource := ""
	if change.HasUserEvent {
		threadSource = "user"
	}
	archived := 0
	if change.Archived {
		archived = 1
	}
	return map[string]any{
		"id":                 change.ThreadID,
		"rollout_path":       change.Path,
		"created_at":         createdAt,
		"updated_at":         updatedAt,
		"source":             firstString(change.Source, "vscode"),
		"model_provider":     targetProvider,
		"cwd":                change.CWD,
		"title":              title,
		"sandbox_policy":     firstString(change.SandboxPolicy, `{"type":"danger-full-access"}`),
		"approval_mode":      firstString(change.ApprovalMode, "never"),
		"tokens_used":        0,
		"has_user_event":     boolInt(change.HasUserEvent),
		"archived":           archived,
		"archived_at":        nil,
		"git_sha":            nil,
		"git_branch":         nil,
		"git_origin_url":     nil,
		"cli_version":        change.CLIVersion,
		"first_user_message": firstString(change.FirstUserMessage, preview),
		"agent_nickname":     nil,
		"agent_role":         nil,
		"memory_mode":        "enabled",
		"model":              nullableString(change.Model),
		"reasoning_effort":   nullableString(change.ReasoningEffort),
		"agent_path":         nil,
		"created_at_ms":      createdAtMs,
		"updated_at_ms":      updatedAtMs,
		"thread_source":      nullableString(threadSource),
		"preview":            preview,
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func sqliteHasColumn(path, table, column string) bool {
	if !fileExists(path) {
		return false
	}
	out, err := sqliteQuery(path, "PRAGMA table_info("+quoteSQLiteIdentifier(table)+")")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.Split(line, "|")
		if len(parts) > 1 && parts[1] == column {
			return true
		}
	}
	return false
}

func sqliteScalarInt(path, query string, args ...string) (int, error) {
	out, err := sqliteQuery(path, query, args...)
	if err != nil {
		return 0, err
	}
	value, _ := strconv.Atoi(strings.TrimSpace(out))
	return value, nil
}

func sqliteExecRows(path, query string, args ...string) (int, error) {
	db, err := openSQLite(path)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	result, err := db.Exec(query, sqliteArgs(args)...)
	if err != nil {
		return 0, err
	}
	rows, _ := result.RowsAffected()
	return int(rows), nil
}

func sqliteQuery(path, query string, args ...string) (string, error) {
	db, err := openSQLite(path)
	if err != nil {
		return "", err
	}
	defer db.Close()
	rows, err := db.Query(query, sqliteArgs(args)...)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return "", err
	}
	var lines []string
	for rows.Next() {
		values := make([]sql.NullString, len(columns))
		scan := make([]any, len(columns))
		for index := range values {
			scan[index] = &values[index]
		}
		if err := rows.Scan(scan...); err != nil {
			return "", err
		}
		parts := make([]string, len(columns))
		for index, value := range values {
			if value.Valid {
				parts[index] = value.String
			}
		}
		lines = append(lines, strings.Join(parts, "|"))
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(lines) == 0 {
		return "", nil
	}
	return strings.Join(lines, "\n") + "\n", nil
}

func openSQLite(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec("PRAGMA busy_timeout = 2000"); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func quoteSQLiteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func sqliteArgs(args []string) []any {
	values := make([]any, len(args))
	for index, arg := range args {
		values[index] = arg
	}
	return values
}

func loadGlobalState(path string) map[string]any {
	var state map[string]any
	if err := readJSON(path, &state); err != nil || state == nil {
		return map[string]any{}
	}
	return state
}

func normalizedGlobalState(state map[string]any, changes []sessionChange) map[string]any {
	next := map[string]any{}
	historyRoots := providerSyncWorkspaceRoots(changes)
	if value, ok := state["electron-saved-workspace-roots"]; ok {
		next["electron-saved-workspace-roots"] = dedupePaths(append(pathArray(value), historyRoots...))
	} else if len(historyRoots) > 0 {
		next["electron-saved-workspace-roots"] = historyRoots
	}
	if value, ok := state["project-order"]; ok {
		next["project-order"] = dedupePaths(append(pathArray(value), historyRoots...))
	} else if len(historyRoots) > 0 {
		next["project-order"] = historyRoots
	}
	if value, ok := state["active-workspace-roots"]; ok {
		normalized := dedupePaths(pathArray(value))
		if _, array := value.([]any); array {
			next["active-workspace-roots"] = normalized
		} else if len(normalized) > 0 {
			next["active-workspace-roots"] = normalized[0]
		} else {
			next["active-workspace-roots"] = value
		}
	}
	if value, ok := state["electron-workspace-root-labels"].(map[string]any); ok {
		labels := map[string]any{}
		for key, item := range value {
			normalized := toDesktopWorkspacePath(key)
			if normalized == "" {
				normalized = key
			}
			labels[normalized] = item
		}
		next["electron-workspace-root-labels"] = labels
	}
	hints := providerSyncWorkspaceHints(state["thread-workspace-root-hints"], changes)
	if len(hints) > 0 {
		next["thread-workspace-root-hints"] = hints
	}
	return next
}

func countGlobalStateUpdates(path string, changes []sessionChange) int {
	state := loadGlobalState(path)
	next := normalizedGlobalState(state, changes)
	count := 0
	for key, value := range next {
		if !jsonEqual(state[key], value) {
			count++
		}
	}
	return count
}

func applyGlobalStateUpdate(path string, changes []sessionChange) (int, error) {
	state := loadGlobalState(path)
	next := normalizedGlobalState(state, changes)
	count := 0
	for key, value := range next {
		if !jsonEqual(state[key], value) {
			state[key] = value
			count++
		}
	}
	if count > 0 {
		return count, atomicWriteJSON(path, state)
	}
	return 0, nil
}

func providerSyncWorkspaceRoots(changes []sessionChange) []string {
	seen := map[string]bool{}
	var roots []string
	for _, change := range changes {
		if change.ThreadID == "" || !change.HasUserEvent || !providerSyncShouldRestoreWorkspace(change.CWD) {
			continue
		}
		key := strings.ToLower(strings.TrimRight(strings.ReplaceAll(change.CWD, "/", `\`), `\`))
		if seen[key] {
			continue
		}
		seen[key] = true
		roots = append(roots, change.CWD)
	}
	return roots
}

func providerSyncShouldRestoreWorkspace(cwd string) bool {
	cwd = toDesktopWorkspacePath(cwd)
	if strings.TrimSpace(cwd) == "" {
		return false
	}
	slash := filepath.ToSlash(cwd)
	lower := strings.ToLower(slash)
	if strings.Contains(lower, "/.codex/worktrees/") || strings.Contains(lower, "/documents/codex/") {
		return false
	}
	if base := strings.ToLower(filepath.Base(cwd)); base == "outputs" || strings.HasPrefix(base, "new-chat") {
		return false
	}
	return true
}

func providerSyncWorkspaceHints(existing any, changes []sessionChange) map[string]any {
	hints := mapFromAny(existing)
	for _, change := range changes {
		if change.ThreadID == "" || !change.HasUserEvent || !providerSyncShouldRestoreWorkspace(change.CWD) {
			continue
		}
		for _, id := range sessionIDVariants(change.ThreadID) {
			if strings.HasPrefix(id, "local:") {
				continue
			}
			hints[id] = change.CWD
		}
	}
	return hints
}

func pathArray(value any) []string {
	switch typed := value.(type) {
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(stringFromAny(item)); text != "" {
				out = append(out, text)
			}
		}
		return out
	case []string:
		return typed
	case string:
		if strings.TrimSpace(typed) != "" {
			return []string{typed}
		}
	}
	return nil
}

func dedupePaths(paths []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, path := range paths {
		desktop := toDesktopWorkspacePath(path)
		if desktop == "" {
			continue
		}
		comparable := strings.ToLower(strings.TrimRight(strings.ReplaceAll(desktop, "/", `\`), `\`))
		if seen[comparable] {
			continue
		}
		seen[comparable] = true
		out = append(out, desktop)
	}
	return out
}

func jsonEqual(left, right any) bool {
	leftBytes, _ := json.Marshal(left)
	rightBytes, _ := json.Marshal(right)
	return bytes.Equal(leftBytes, rightBytes)
}

func pruneProviderSyncBackups(home string) {
	root := filepath.Join(home, "backups_state", "provider-sync")
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	var managed []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name())
		var meta map[string]any
		if readJSON(filepath.Join(path, "metadata.json"), &meta) == nil && stringFromAny(meta["managedBy"]) == "Codex++ provider sync" {
			managed = append(managed, path)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(managed)))
	if len(managed) <= 5 {
		return
	}
	for _, path := range managed[5:] {
		_ = os.RemoveAll(path)
	}
}
