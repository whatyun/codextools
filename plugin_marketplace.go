package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	openAICuratedMarketplaceName    = "openai-curated"
	openAIPluginsZipURL             = "https://codeload.github.com/openai/plugins/zip/refs/heads/main"
	openAIPluginsDownloadLimitBytes = 128 * 1024 * 1024
)

type pluginMarketplaceStatus struct {
	CodexHome        string  `json:"codexHome"`
	MarketplaceRoot  *string `json:"marketplaceRoot,omitempty"`
	ConfigRegistered bool    `json:"configRegistered"`
	NeedsRepair      bool    `json:"needsRepair"`
}

type pluginMarketplaceRepairPayload struct {
	CodexHome       string  `json:"codexHome"`
	MarketplaceRoot *string `json:"marketplaceRoot,omitempty"`
	Initialized     bool    `json:"initialized"`
	Configured      bool    `json:"configured"`
	NeedsRepair     bool    `json:"needsRepair"`
}

func (s *server) pluginMarketplaceStatus() commandResult {
	status := openAICuratedMarketplaceStatus(codexHomeDir())
	message := "插件市场已可用。"
	if status.NeedsRepair {
		message = "插件市场需要初始化或注册。"
	}
	payload := map[string]any{
		"codexHome":        status.CodexHome,
		"marketplaceRoot":  nullableStringPtr(status.MarketplaceRoot),
		"configRegistered": status.ConfigRegistered,
		"needsRepair":      status.NeedsRepair,
	}
	return ok(message, payload)
}

func (s *server) repairPluginMarketplace(ctx context.Context) commandResult {
	home := codexHomeDir()
	initialized := false
	if localOpenAICuratedMarketplaceRoot(home) == "" {
		if err := initializeOpenAICuratedMarketplaceFromGitHub(ctx, home); err != nil {
			status := openAICuratedMarketplaceStatus(home)
			return failed("插件市场修复失败："+err.Error(), map[string]any{
				"codexHome":       status.CodexHome,
				"marketplaceRoot": nullableStringPtr(status.MarketplaceRoot),
				"initialized":     initialized,
				"configured":      status.ConfigRegistered,
				"needsRepair":     status.NeedsRepair,
			})
		}
		initialized = true
	}
	configured, err := ensureOpenAICuratedMarketplaceConfig(home)
	if err != nil {
		status := openAICuratedMarketplaceStatus(home)
		return failed("插件市场修复失败："+err.Error(), map[string]any{
			"codexHome":       status.CodexHome,
			"marketplaceRoot": nullableStringPtr(status.MarketplaceRoot),
			"initialized":     initialized,
			"configured":      status.ConfigRegistered,
			"needsRepair":     status.NeedsRepair,
		})
	}
	status := openAICuratedMarketplaceStatus(home)
	message := "插件市场已可用，无需修复。"
	if initialized {
		message = "插件市场已从 openai/plugins 初始化并注册。"
	} else if configured {
		message = "已注册本地插件市场。"
	}
	return ok(message, map[string]any{
		"codexHome":       status.CodexHome,
		"marketplaceRoot": nullableStringPtr(status.MarketplaceRoot),
		"initialized":     initialized,
		"configured":      configured,
		"needsRepair":     false,
	})
}

func openAICuratedMarketplaceStatus(home string) pluginMarketplaceStatus {
	root := localOpenAICuratedMarketplaceRoot(home)
	var rootPtr *string
	if root != "" {
		rootPtr = &root
	}
	registered := root != "" && marketplaceConfigPointsToRoot(home, openAICuratedMarketplaceName, root)
	return pluginMarketplaceStatus{
		CodexHome:        home,
		MarketplaceRoot:  rootPtr,
		ConfigRegistered: registered,
		NeedsRepair:      root == "" || !registered,
	}
}

func localOpenAICuratedMarketplaceRoot(home string) string {
	root := filepath.Join(home, ".tmp", "plugins")
	manifestPath := filepath.Join(root, ".agents", "plugins", "marketplace.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return ""
	}
	var manifest map[string]any
	if json.Unmarshal(data, &manifest) != nil {
		return ""
	}
	if stringFromAny(manifest["name"]) != openAICuratedMarketplaceName {
		return ""
	}
	plugins, _ := manifest["plugins"].([]any)
	if len(plugins) == 0 || !isDir(filepath.Join(root, "plugins")) {
		return ""
	}
	return root
}

func marketplaceConfigPointsToRoot(home, name, root string) bool {
	contents := readFile(filepath.Join(home, "config.toml"))
	values := tableValues(contents, "marketplaces."+name)
	return strings.TrimSpace(unquoteToml(values["source_type"])) == "local" && samePath(strings.TrimSpace(unquoteToml(values["source"])), root)
}

func ensureOpenAICuratedMarketplaceConfig(home string) (bool, error) {
	root := localOpenAICuratedMarketplaceRoot(home)
	if root == "" {
		return false, nil
	}
	path := filepath.Join(home, "config.toml")
	contents := readFile(path)
	updated := repairCodexMarketplaceTable(contents, marketplaceSpec{Name: openAICuratedMarketplaceName, Source: root})
	if updated == contents {
		return false, nil
	}
	if _, err := writeCodexConfigWithBackup(path, updated, "plugin-marketplace"); err != nil {
		return false, err
	}
	return true, nil
}

func initializeOpenAICuratedMarketplaceFromGitHub(ctx context.Context, home string) error {
	bytes, err := downloadOpenAIPluginsZip(ctx)
	if err != nil {
		return err
	}
	return installOpenAIPluginsZip(home, bytes)
}

func downloadOpenAIPluginsZip(ctx context.Context) ([]byte, error) {
	client := &http.Client{Timeout: 90 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, openAIPluginsZipURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("accept", "application/zip")
	req.Header.Set("user-agent", appName+"/"+version)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai/plugins marketplace download returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, openAIPluginsDownloadLimitBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > openAIPluginsDownloadLimitBytes {
		return nil, fmt.Errorf("openai/plugins marketplace download is too large: %d bytes", len(body))
	}
	return body, nil
}

func installOpenAIPluginsZip(home string, data []byte) error {
	destination := filepath.Join(home, ".tmp", "plugins")
	stagingParent := filepath.Join(home, ".tmp")
	if err := os.MkdirAll(stagingParent, 0o755); err != nil {
		return err
	}
	staging := filepath.Join(stagingParent, fmt.Sprintf("plugins-download-%d", time.Now().UnixMilli()))
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(staging)
		}
	}()
	if err := extractOpenAIPluginsZip(data, staging); err != nil {
		return err
	}
	if localOpenAICuratedMarketplaceRoot(staging) == "" {
		return errors.New("downloaded openai/plugins marketplace is invalid")
	}
	if err := replaceDirectory(destination, staging); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func extractOpenAIPluginsZip(data []byte, destination string) error {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	for _, file := range reader.File {
		relative, ok := zipEntryRelativePath(file.Name)
		if !ok {
			continue
		}
		target := filepath.Join(destination, relative)
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, file.Mode().Perm()); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		input, err := file.Open()
		if err != nil {
			return err
		}
		err = writeZipFile(target, input, file.Mode().Perm())
		closeErr := input.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func zipEntryRelativePath(name string) (string, bool) {
	name = filepath.ToSlash(strings.TrimSpace(name))
	parts := strings.Split(name, "/")
	if len(parts) < 2 {
		return "", false
	}
	relativeParts := parts[1:]
	for _, part := range relativeParts {
		if part == "" || part == "." || part == ".." {
			return "", false
		}
	}
	return filepath.Join(relativeParts...), true
}

func writeZipFile(path string, input io.Reader, perm os.FileMode) error {
	output, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func localPluginMarketplacesValue(home string) []map[string]any {
	root := localOpenAICuratedMarketplaceRoot(home)
	if root == "" {
		return []map[string]any{}
	}
	manifestPath := filepath.Join(root, ".agents", "plugins", "marketplace.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return []map[string]any{}
	}
	var marketplace map[string]any
	if json.Unmarshal(data, &marketplace) != nil {
		return []map[string]any{}
	}
	marketplace["path"] = manifestPath
	expandLocalPluginMarketplace(marketplace, root, installedPluginsFromConfig(home))
	return []map[string]any{marketplace}
}

func expandLocalPluginMarketplace(marketplace map[string]any, root string, installed map[string]bool) {
	marketplaceName := stringFromAny(marketplace["name"])
	rawPlugins, _ := marketplace["plugins"].([]any)
	for _, raw := range rawPlugins {
		plugin, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name := strings.TrimSpace(firstNonEmpty(stringFromAny(plugin["name"]), stringFromAny(plugin["id"])))
		if strings.Contains(name, "@") {
			name = strings.SplitN(name, "@", 2)[0]
		}
		if name == "" {
			continue
		}
		manifestPath := filepath.Join(root, "plugins", name, ".codex-plugin", "plugin.json")
		var manifest map[string]any
		if data, err := os.ReadFile(manifestPath); err == nil && json.Unmarshal(data, &manifest) == nil {
			for key, value := range manifest {
				if _, exists := plugin[key]; !exists {
					plugin[key] = value
				}
			}
		}
		if _, exists := plugin["name"]; !exists {
			plugin["name"] = name
		}
		if _, exists := plugin["id"]; !exists {
			plugin["id"] = name + "@" + marketplaceName
		}
		if _, exists := plugin["keywords"]; !exists {
			plugin["keywords"] = []any{}
		}
		pluginRoot := filepath.Join(root, "plugins", name)
		plugin["installed"] = installed[name+"@"+marketplaceName]
		plugin["enabled"] = installed[name+"@"+marketplaceName]
		plugin["__codexPlusLocalPluginPath"] = pluginRoot
		absolutizePluginAssetPaths(plugin, pluginRoot)
	}
}

func absolutizePluginAssetPaths(plugin map[string]any, root string) {
	for _, key := range []string{"composerIconPath", "logoPath"} {
		if path := absolutePluginAssetPath(stringFromAny(plugin[key]), root); path != "" {
			plugin[key] = path
		}
	}
	iface, _ := plugin["interface"].(map[string]any)
	if iface == nil {
		iface = map[string]any{}
		plugin["interface"] = iface
	}
	for _, key := range []string{"composerIcon", "composerIconUrl", "logo", "logoUrl"} {
		if path := absolutePluginAssetPath(stringFromAny(iface[key]), root); path != "" {
			iface[key] = path
		}
	}
}

func absolutePluginAssetPath(value, root string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || strings.HasPrefix(trimmed, "data:") || strings.HasPrefix(trimmed, "http:") || strings.HasPrefix(trimmed, "https:") || filepath.IsAbs(trimmed) {
		return ""
	}
	return filepath.Join(root, strings.TrimPrefix(trimmed, "./"))
}

func installedPluginsFromConfig(home string) map[string]bool {
	contents := readFile(filepath.Join(home, "config.toml"))
	out := map[string]bool{}
	current := ""
	for _, line := range splitLines(contents) {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[plugins.") && strings.HasSuffix(trimmed, "]") {
			current = strings.TrimSuffix(strings.TrimPrefix(trimmed, "[plugins."), "]")
			current = strings.Trim(current, `"`)
			continue
		}
		if current != "" && strings.HasPrefix(trimmed, "[") {
			current = ""
			continue
		}
		if current != "" && rootLineKey(trimmed) == "enabled" && strings.Contains(trimmed, "true") {
			out[current] = true
		}
	}
	return out
}
