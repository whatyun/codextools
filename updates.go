package main

import (
	"context"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

func (s *server) checkUpdate(ctx context.Context) commandResult {
	payload, err := latestCodexToolsUpdate(ctx, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		payload["updateStatus"] = "failed"
		payload["message"] = "检查更新失败：" + err.Error()
		return failed("检查更新失败："+err.Error(), payload)
	}
	if stringFromAny(payload["updateStatus"]) == "available" {
		return ok("发现新版本 "+stringFromAny(payload["latestVersion"])+"。", payload)
	}
	if stringFromAny(payload["updateStatus"]) == "up_to_date" {
		return ok("当前已是最新版本。", payload)
	}
	return ok(stringFromAny(payload["message"]), payload)
}

func (s *server) installUpdate(ctx context.Context) commandResult {
	update, err := latestCodexToolsUpdate(ctx, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		update["updateStatus"] = "failed"
		update["message"] = "获取更新失败：" + err.Error()
		return failed("获取更新失败："+err.Error(), update)
	}
	if stringFromAny(update["updateStatus"]) != "available" {
		message := stringFromAny(update["message"])
		if message == "" {
			message = "当前没有可安装的新版本。"
		}
		return ok(message, update)
	}
	downloadURL := stringFromAny(update["downloadUrl"])
	if downloadURL == "" {
		if releaseURL := stringFromAny(update["releaseUrl"]); releaseURL != "" {
			if err := openURL(releaseURL); err != nil {
				update["updateStatus"] = "failed"
				update["message"] = "打开发布页面失败：" + err.Error()
				return failed("打开发布页面失败："+err.Error(), update)
			}
			update["updateStatus"] = "opened_release"
			update["message"] = "已打开发布页面，请选择当前系统的安装包。"
			return ok("已打开发布页面，请选择当前系统的安装包。", update)
		}
		update["updateStatus"] = "failed"
		update["message"] = "最新版本没有可下载地址。"
		return failed("最新版本没有可下载地址。", update)
	}
	data, err := getBytes(ctx, downloadURL)
	if err != nil {
		update["updateStatus"] = "failed"
		update["message"] = "下载更新失败：" + err.Error()
		return failed("下载更新失败："+err.Error(), update)
	}
	target := filepath.Join(downloadsDir(), safeUpdateFilename(stringFromAny(update["assetName"]), stringFromAny(update["latestVersion"])))
	if err := atomicWrite(target, data); err != nil {
		update["updateStatus"] = "failed"
		update["message"] = "保存更新包失败：" + err.Error()
		return failed("保存更新包失败："+err.Error(), update)
	}
	update["downloadedPath"] = target
	if err := openPath(target); err != nil {
		update["updateStatus"] = "downloaded"
		update["message"] = "更新包已下载到 " + target + "，但自动打开失败：" + err.Error()
		return ok("更新包已下载到 "+target+"，但自动打开失败："+err.Error(), update)
	}
	update["updateStatus"] = "downloaded"
	update["message"] = "更新包已下载并打开，请按系统提示替换当前版本。"
	return ok("更新包已下载并打开，请按系统提示替换当前版本。", update)
}

func latestCodexToolsUpdate(ctx context.Context, goos, goarch string) (map[string]any, error) {
	payload := map[string]any{
		"updateStatus":   "not_checked",
		"currentVersion": version,
		"projectUrl":     codexToolsProjectURL,
		"releaseUrl":     codexToolsReleaseURL,
		"downloadsUrl":   codexToolsDownloadsURL,
		"platform":       goos,
		"arch":           goarch,
	}
	release, err := getJSON[codexToolsRelease](ctx, codexToolsLatestAPIURL)
	if err != nil {
		fallback, fallbackErr := latestCodexToolsUpdateFromDownloadsPage(ctx, payload, goos, goarch, err)
		if fallbackErr == nil {
			return fallback, nil
		}
		payload["updateStatus"] = "degraded"
		payload["message"] = "GitHub API 暂时不可用（" + err.Error() + "），可打开下载页手动检查更新。"
		payload["apiError"] = err.Error()
		payload["fallbackError"] = fallbackErr.Error()
		return payload, nil
	}
	latest := cleanVersion(firstString(release.TagName, release.Name))
	payload["latestVersion"] = latest
	payload["releaseName"] = release.Name
	payload["tagName"] = release.TagName
	payload["publishedAt"] = release.PublishedAt
	if release.HTMLURL != "" {
		payload["releaseUrl"] = release.HTMLURL
	}
	if latest == "" {
		payload["updateStatus"] = "failed"
		payload["message"] = "最新发布没有版本号。"
		return payload, nil
	}
	if compareVersions(latest, version) <= 0 {
		payload["updateStatus"] = "up_to_date"
		payload["message"] = "当前已是最新版本。"
		return payload, nil
	}
	asset, ok := selectCodexToolsAsset(release.Assets, goos, goarch)
	if !ok {
		payload["updateStatus"] = "missing_asset"
		payload["message"] = "发现新版本，但没有找到当前系统对应安装包。"
		return payload, nil
	}
	payload["updateStatus"] = "available"
	payload["assetName"] = asset.Name
	payload["downloadUrl"] = asset.BrowserDownloadURL
	payload["size"] = asset.Size
	payload["contentType"] = asset.ContentType
	annotateCodexToolsAssetPayload(payload, asset.Name, goos)
	payload["message"] = codexToolsUpdateAvailableMessage(goos, stringFromAny(payload["assetKind"]))
	return payload, nil
}

func latestCodexToolsUpdateFromDownloadsPage(ctx context.Context, base map[string]any, goos, goarch string, apiErr error) (map[string]any, error) {
	data, err := getBytes(ctx, codexToolsDownloadsURL)
	if err != nil {
		return nil, err
	}
	assets := codexToolsDownloadsAssets(string(data))
	if len(assets) == 0 {
		return nil, fmt.Errorf("下载页没有找到 CodexTools 安装包链接")
	}
	asset, ok := selectCodexToolsAsset(assets, goos, goarch)
	if !ok {
		payload := cloneStringAnyMap(base)
		payload["releaseUrl"] = codexToolsDownloadsURL
		payload["updateSource"] = "downloads_page"
		if apiErr != nil {
			payload["apiError"] = apiErr.Error()
		}
		payload["updateStatus"] = "missing_asset"
		payload["message"] = "GitHub API 暂时不可用，下载页也没有找到当前系统对应安装包。"
		return payload, nil
	}
	latest := codexToolsAssetVersion(asset.Name)
	payload := cloneStringAnyMap(base)
	payload["latestVersion"] = latest
	payload["releaseName"] = "CodexTools " + latest
	payload["tagName"] = "v" + latest
	payload["releaseUrl"] = codexToolsDownloadsURL
	payload["updateSource"] = "downloads_page"
	if apiErr != nil {
		payload["apiError"] = apiErr.Error()
	}
	if latest == "" {
		payload["updateStatus"] = "degraded"
		payload["message"] = "GitHub API 暂时不可用，下载页也没有可识别的版本号。"
		return payload, nil
	}
	if compareVersions(latest, version) <= 0 {
		payload["updateStatus"] = "up_to_date"
		payload["message"] = "当前已是最新版本。"
		return payload, nil
	}
	payload["updateStatus"] = "available"
	payload["assetName"] = asset.Name
	payload["downloadUrl"] = asset.BrowserDownloadURL
	payload["size"] = asset.Size
	payload["contentType"] = asset.ContentType
	annotateCodexToolsAssetPayload(payload, asset.Name, goos)
	payload["message"] = codexToolsUpdateAvailableMessage(goos, stringFromAny(payload["assetKind"]))
	return payload, nil
}

var codexToolsDownloadHrefPattern = regexp.MustCompile(`(?i)href=["']([^"']*CodexTools-[^"']+\.(pkg|dmg|zip|exe))["']`)

func codexToolsDownloadsAssets(page string) []codexAppMirrorAsset {
	matches := codexToolsDownloadHrefPattern.FindAllStringSubmatch(page, -1)
	assets := make([]codexAppMirrorAsset, 0, len(matches))
	seen := map[string]bool{}
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		href := html.UnescapeString(strings.TrimSpace(match[1]))
		if href == "" {
			continue
		}
		downloadURL := codexToolsAbsoluteDownloadURL(href)
		if seen[downloadURL] {
			continue
		}
		seen[downloadURL] = true
		name := filepath.Base(strings.TrimPrefix(href, "./"))
		assets = append(assets, codexAppMirrorAsset{
			Name:               name,
			BrowserDownloadURL: downloadURL,
			ContentType:        codexToolsAssetContentType(name),
		})
	}
	return assets
}

func codexToolsAbsoluteDownloadURL(href string) string {
	if strings.HasPrefix(href, "https://") || strings.HasPrefix(href, "http://") {
		return href
	}
	cleaned := strings.TrimLeft(strings.TrimPrefix(href, "./"), "/")
	return codexToolsPagesBaseURL + cleaned
}

func codexToolsAssetContentType(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".pkg"):
		return "application/octet-stream"
	case strings.HasSuffix(lower, ".dmg"):
		return "application/x-apple-diskimage"
	case strings.HasSuffix(lower, ".zip"):
		return "application/zip"
	case strings.HasSuffix(lower, ".exe"):
		return "application/vnd.microsoft.portable-executable"
	default:
		return ""
	}
}

func codexToolsAssetVersion(name string) string {
	lower := strings.ToLower(name)
	prefix := "codextools-"
	index := strings.Index(lower, prefix)
	if index < 0 {
		return ""
	}
	rest := name[index+len(prefix):]
	parts := strings.Split(rest, "-")
	if len(parts) == 0 {
		return ""
	}
	return cleanVersion(parts[0])
}

func selectCodexToolsAsset(assets []codexAppMirrorAsset, goos, goarch string) (codexAppMirrorAsset, bool) {
	var best codexAppMirrorAsset
	bestScore := -1_000_000
	bestVersion := ""
	for _, asset := range assets {
		if strings.TrimSpace(asset.BrowserDownloadURL) == "" {
			continue
		}
		if !codexToolsAssetCompatible(asset.Name, goos, goarch) {
			continue
		}
		score := codexToolsAssetScore(asset.Name, goos, goarch)
		if score <= 0 {
			continue
		}
		assetVersion := codexToolsAssetVersion(asset.Name)
		if bestVersion == "" || compareVersions(assetVersion, bestVersion) > 0 || (compareVersions(assetVersion, bestVersion) == 0 && score > bestScore) {
			best = asset
			bestScore = score
			bestVersion = assetVersion
		}
	}
	return best, bestScore > 0
}

func codexToolsAssetCompatible(name, goos, goarch string) bool {
	lower := strings.ToLower(name)
	switch goos {
	case "darwin":
		if !strings.Contains(lower, "macos") && !strings.Contains(lower, "darwin") {
			return false
		}
	case "windows":
		if !strings.Contains(lower, "windows") && !strings.Contains(lower, "win") {
			return false
		}
	default:
		if goos != "" && !strings.Contains(lower, goos) {
			return false
		}
	}
	switch goarch {
	case "arm64":
		return strings.Contains(lower, "arm64") || strings.Contains(lower, "aarch64")
	case "amd64":
		return strings.Contains(lower, "x64") || strings.Contains(lower, "amd64") || strings.Contains(lower, "x86_64")
	case "386":
		return strings.Contains(lower, "x86") || strings.Contains(lower, "386") || strings.Contains(lower, "ia32")
	default:
		return true
	}
}

func codexToolsAssetScore(name, goos, goarch string) int {
	lower := strings.ToLower(name)
	score := 0
	if !strings.Contains(lower, "codextools") {
		score -= 20
	}
	switch goos {
	case "darwin":
		if strings.Contains(lower, "macos") || strings.Contains(lower, "darwin") {
			score += 40
		}
		if strings.HasSuffix(lower, ".pkg") {
			score += 80
		}
		if strings.HasSuffix(lower, ".dmg") {
			score += 55
		}
	case "windows":
		if strings.Contains(lower, "windows") || strings.Contains(lower, "win") {
			score += 40
		}
		if strings.Contains(lower, "setup") || strings.Contains(lower, "installer") {
			score += 20
		}
	default:
		if strings.Contains(lower, goos) {
			score += 30
		}
	}
	switch goarch {
	case "arm64":
		if strings.Contains(lower, "arm64") || strings.Contains(lower, "aarch64") {
			score += 25
		}
	case "amd64":
		if strings.Contains(lower, "x64") || strings.Contains(lower, "amd64") || strings.Contains(lower, "x86_64") {
			score += 25
		}
	}
	if strings.HasSuffix(lower, ".zip") {
		if goos == "darwin" {
			score -= 20
		} else {
			score += 12
		}
	}
	if strings.HasSuffix(lower, ".pkg") {
		score += 18
	}
	if strings.HasSuffix(lower, ".dmg") {
		score += 14
	}
	if strings.HasSuffix(lower, ".exe") {
		score += 18
	}
	if strings.Contains(lower, "sha256") || strings.Contains(lower, "manifest") || strings.HasSuffix(lower, ".txt") || strings.HasSuffix(lower, ".json") {
		score -= 100
	}
	return score
}

func annotateCodexToolsAssetPayload(payload map[string]any, name, goos string) {
	lower := strings.ToLower(name)
	kind := "archive"
	if strings.HasSuffix(lower, ".pkg") {
		kind = "pkg"
	} else if strings.HasSuffix(lower, ".dmg") {
		kind = "dmg"
	} else if strings.HasSuffix(lower, ".exe") {
		kind = "installer"
	} else if strings.HasSuffix(lower, ".zip") {
		kind = "portable"
	}
	payload["assetKind"] = kind
	payload["portable"] = kind == "portable"
	if goos == "darwin" {
		payload["installTarget"] = "/Applications"
		payload["installerDefault"] = kind == "pkg" || kind == "dmg"
	}
}

func codexToolsUpdateAvailableMessage(goos, kind string) string {
	if goos == "darwin" {
		if kind == "pkg" {
			return "发现新版本，可下载 macOS pkg 安装器；安装后会覆盖 /Applications 中的 Codex++ 应用。"
		}
		if kind == "dmg" {
			return "发现新版本，可下载 macOS DMG；请拖入 /Applications 覆盖旧版 Codex++ 应用。"
		}
		return "发现新版本，但仅找到便携包；建议到发布页下载 pkg 安装器以覆盖 /Applications。"
	}
	return "发现新版本，可下载更新包。"
}

func compareVersions(a, b string) int {
	left := versionParts(a)
	right := versionParts(b)
	maxLen := len(left)
	if len(right) > maxLen {
		maxLen = len(right)
	}
	for i := 0; i < maxLen; i++ {
		var lv, rv int
		if i < len(left) {
			lv = left[i]
		}
		if i < len(right) {
			rv = right[i]
		}
		if lv > rv {
			return 1
		}
		if lv < rv {
			return -1
		}
	}
	return 0
}

func versionParts(value string) []int {
	value = cleanVersion(value)
	var parts []int
	for _, part := range strings.FieldsFunc(value, func(r rune) bool {
		return r == '.' || r == '-' || r == '_' || r == '+'
	}) {
		if part == "" {
			continue
		}
		number := strings.Builder{}
		for _, r := range part {
			if r < '0' || r > '9' {
				break
			}
			number.WriteRune(r)
		}
		if number.Len() == 0 {
			continue
		}
		parsed, err := strconv.Atoi(number.String())
		if err != nil {
			continue
		}
		parts = append(parts, parsed)
	}
	return parts
}

func cleanVersion(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.TrimPrefix(value, "release ")
	value = strings.TrimPrefix(value, "codextools ")
	value = strings.TrimPrefix(value, "v")
	return strings.TrimSpace(value)
}

func downloadsDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return stateDir()
	}
	path := filepath.Join(home, "Downloads")
	if isDir(path) {
		return path
	}
	return stateDir()
}

func safeUpdateFilename(assetName, latestVersion string) string {
	name := filepath.Base(strings.TrimSpace(assetName))
	if name == "." || name == string(filepath.Separator) || name == "" {
		ext := ".zip"
		if runtime.GOOS == "darwin" {
			ext = ".pkg"
		} else if runtime.GOOS == "windows" {
			ext = ".exe"
		}
		name = fmt.Sprintf("CodexTools-%s-%s-%s%s", cleanVersion(latestVersion), runtime.GOOS, runtime.GOARCH, ext)
	}
	name = strings.ReplaceAll(name, string(filepath.Separator), "-")
	return name
}
