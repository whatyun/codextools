package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	computerUsePluginName    = "computer-use"
	computerUseMarketplace   = "openai-bundled"
	computerUsePluginVersion = "0.1.0-local"
)

func (s *server) loadComputerUseStatus() commandResult {
	status := loadComputerUseStatus(codexHomeDir())
	message := "Computer Use 状态已读取。"
	resultStatus := "ok"
	if !status.Supported {
		resultStatus = "unsupported"
		message = "Computer Use 修复目前只支持 Windows。"
	} else if !status.AllReady {
		resultStatus = "not_checked"
		message = "Computer Use 尚未完全可用，可以点击一键修复。"
	}
	payload, _ := structToMap(status)
	return commandResultWithStatus(resultStatus, message, payload)
}

func (s *server) repairComputerUse() commandResult {
	status, err := repairComputerUse(codexHomeDir(), runtime.GOOS, true)
	payload, _ := structToMap(status)
	if err != nil {
		resultStatus := "failed"
		if !status.Supported {
			resultStatus = "unsupported"
		}
		return commandResultWithStatus(resultStatus, "Computer Use 修复失败："+err.Error(), payload)
	}
	return commandResultWithStatus("ok", "Computer Use 已修复；请重启 Codex 让环境变量和插件缓存生效。", payload)
}

func commandResultWithStatus(status, message string, payload map[string]any) commandResult {
	result := commandResult{"status": status, "message": message}
	for key, value := range payload {
		result[key] = value
	}
	return result
}

func structToMap(value any) (map[string]any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}, err
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return map[string]any{}, err
	}
	return out, nil
}

func loadComputerUseStatus(home string) computerUseStatus {
	status := computerUseStatus{
		Platform:        runtime.GOOS,
		Supported:       runtime.GOOS == "windows",
		ProcessEnv:      os.Getenv("CODEX_ELECTRON_ENABLE_WINDOWS_COMPUTER_USE"),
		UserEnv:         computerUseUserEnv(),
		MarketplaceRoot: computerUseMarketplaceRoot(home),
		ConfigPath:      filepath.Join(home, "config.toml"),
	}
	fillComputerUseFileStatus(home, &status)
	return status
}

func fillComputerUseFileStatus(home string, status *computerUseStatus) {
	marketplaceRoot := computerUseMarketplaceRoot(home)
	marketplacePlugin := filepath.Join(marketplaceRoot, "plugins", computerUsePluginName)
	cacheRoot := filepath.Join(home, "plugins", "cache", computerUseMarketplace, computerUsePluginName)
	cacheLatest := filepath.Join(cacheRoot, "latest")
	cacheVersion := filepath.Join(cacheRoot, computerUsePluginVersion)
	helperTransport := filepath.Join(cacheLatest, "node_modules", "@oai", "sky", "dist", "project", "cua", "sky_js", "src", "targets", "windows", "internal", "helper_transport.js")
	if !isDir(cacheLatest) && isDir(cacheVersion) {
		helperTransport = filepath.Join(cacheVersion, "node_modules", "@oai", "sky", "dist", "project", "cua", "sky_js", "src", "targets", "windows", "internal", "helper_transport.js")
	}

	status.MarketplaceRoot = marketplaceRoot
	status.MarketplaceReady = isDir(marketplaceRoot)
	status.MarketplaceManifest = fileExists(filepath.Join(marketplaceRoot, ".agents", "plugins", "marketplace.json"))
	status.MarketplacePlugin = fileExists(filepath.Join(marketplacePlugin, ".codex-plugin", "plugin.json"))
	status.CacheLatest = fileExists(filepath.Join(cacheLatest, ".codex-plugin", "plugin.json")) || fileExists(filepath.Join(cacheVersion, ".codex-plugin", "plugin.json"))
	status.CacheVersion = computerUsePluginVersion
	status.HelperTransport = fileExists(helperTransport)
	status.EnvEnabled = status.ProcessEnv == "1" || status.UserEnv == "1"

	config, _ := os.ReadFile(filepath.Join(home, "config.toml"))
	contents := string(config)
	marketplace := tableValues(contents, "marketplaces."+computerUseMarketplace)
	plugin := tableValues(contents, fmt.Sprintf("plugins.%s", quoteToml(computerUsePluginName+"@"+computerUseMarketplace)))
	windows := tableValues(contents, "windows")
	status.ConfigMarketplace = strings.TrimSpace(unquoteToml(marketplace["source_type"])) == "local" && strings.TrimSpace(unquoteToml(marketplace["source"])) != ""
	status.ConfigPlugin = strings.TrimSpace(plugin["enabled"]) == "true"
	status.ConfigWindows = strings.TrimSpace(unquoteToml(windows["sandbox"])) == "unelevated"
	status.ConfigReady = status.ConfigMarketplace && status.ConfigPlugin && status.ConfigWindows
	status.AllReady = status.Supported && status.EnvEnabled && status.MarketplaceManifest && status.MarketplacePlugin && status.CacheLatest && status.HelperTransport && status.ConfigReady
}

func repairComputerUse(home, platform string, setUserEnv bool) (computerUseStatus, error) {
	status := loadComputerUseStatus(home)
	status.Platform = platform
	status.Supported = platform == "windows"
	if !status.Supported {
		return status, fmt.Errorf("当前平台是 %s，只支持 Windows", platform)
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		return status, err
	}
	marketplaceRoot := computerUseMarketplaceRoot(home)
	marketplacePlugin := filepath.Join(marketplaceRoot, "plugins", computerUsePluginName)
	cacheRoot := filepath.Join(home, "plugins", "cache", computerUseMarketplace, computerUsePluginName)
	cacheVersion := filepath.Join(cacheRoot, computerUsePluginVersion)
	cacheLatest := filepath.Join(cacheRoot, "latest")
	for _, dir := range []string{marketplaceRoot, marketplacePlugin, cacheVersion} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return status, err
		}
	}
	if err := writeComputerUsePluginTree(marketplacePlugin); err != nil {
		return status, err
	}
	if err := writeComputerUsePluginTree(cacheVersion); err != nil {
		return status, err
	}
	if err := replaceDirectory(cacheLatest, cacheVersion); err != nil {
		return status, err
	}
	if err := updateComputerUseMarketplaceManifest(marketplaceRoot); err != nil {
		return status, err
	}
	backupPath, err := updateComputerUseCodexConfig(home, marketplaceRoot)
	if err != nil {
		return status, err
	}
	if setUserEnv {
		if err := setComputerUseUserEnv(); err != nil {
			return status, err
		}
	}
	status = loadComputerUseStatus(home)
	status.Platform = platform
	status.Supported = true
	status.BackupPath = backupPath
	status.AllReady = status.Supported && status.EnvEnabled && status.MarketplaceManifest && status.MarketplacePlugin && status.CacheLatest && status.HelperTransport && status.ConfigReady
	return status, nil
}

func computerUseMarketplaceRoot(home string) string {
	return filepath.Join(home, ".tmp", "bundled-marketplaces", computerUseMarketplace)
}

func writeComputerUsePluginTree(root string) error {
	files := map[string]string{
		filepath.Join(root, ".codex-plugin", "plugin.json"):       computerUsePluginJSON(),
		filepath.Join(root, "skills", "computer-use", "SKILL.md"): computerUseSkillMarkdown(),
		filepath.Join(root, "node_modules", "@oai", "sky", "package.json"): `{
  "name": "@oai/sky",
  "version": "` + computerUsePluginVersion + `",
  "type": "module",
  "private": true
}
`,
		filepath.Join(root, "node_modules", "@oai", "sky", "bin", "windows", "codex-computer-use.exe"):                                                         "# Placeholder executable path for Codex Desktop Windows Computer Use resolution.\n# The local helper transport module implements the actual request handling.\n",
		filepath.Join(root, "node_modules", "@oai", "sky", "dist", "project", "cua", "sky_js", "src", "targets", "windows", "internal", "helper_transport.js"): computerUseHelperTransportJS(),
	}
	for path, contents := range files {
		if err := atomicWrite(path, []byte(contents)); err != nil {
			return err
		}
	}
	return nil
}

func computerUsePluginJSON() string {
	return `{
  "name": "computer-use",
  "version": "` + computerUsePluginVersion + `",
  "description": "Local Windows Computer Use compatibility helper for Codex Desktop.",
  "author": {
    "name": "Local"
  },
  "homepage": "https://openai.com/",
  "repository": "https://openai.com/",
  "license": "Proprietary",
  "keywords": ["computer-use", "windows", "desktop"],
  "skills": "./skills/",
  "interface": {
    "displayName": "Computer Use",
    "shortDescription": "Control this Windows desktop from Codex",
    "longDescription": "Local compatibility plugin that provides the Windows helper paths expected by Codex Desktop Computer Use.",
    "developerName": "Local",
    "category": "Productivity",
    "capabilities": ["Interactive", "Read", "Write"],
    "websiteURL": "https://openai.com/",
    "privacyPolicyURL": "https://openai.com/policies/row-privacy-policy/",
    "termsOfServiceURL": "https://openai.com/policies/row-terms-of-use/",
    "defaultPrompt": ["Look at my screen and help me navigate"],
    "brandColor": "#10A37F",
    "screenshots": []
  }
}
`
}

func computerUseSkillMarkdown() string {
	return `---
name: computer-use
description: Local Windows Computer Use compatibility helper for Codex Desktop. Provides the @oai/sky paths that the Desktop app expects when CODEX_ELECTRON_ENABLE_WINDOWS_COMPUTER_USE=1.
---

# Computer Use

This local compatibility plugin is installed by CodexTools. It supplies the Windows helper transport paths that Codex Desktop resolves for Computer Use.

The Desktop app must be launched with ` + "`CODEX_ELECTRON_ENABLE_WINDOWS_COMPUTER_USE=1`" + `. CodexTools writes that as a user environment variable, so restart Codex after repair.
`
}

func updateComputerUseMarketplaceManifest(marketplaceRoot string) error {
	manifestPath := filepath.Join(marketplaceRoot, ".agents", "plugins", "marketplace.json")
	var manifest map[string]any
	if err := readJSON(manifestPath, &manifest); err != nil || manifest == nil {
		manifest = map[string]any{
			"name":      computerUseMarketplace,
			"interface": map[string]any{"displayName": "OpenAI Bundled"},
			"plugins":   []any{},
		}
	}
	manifest["name"] = computerUseMarketplace
	if _, ok := manifest["interface"].(map[string]any); !ok {
		manifest["interface"] = map[string]any{"displayName": "OpenAI Bundled"}
	}
	entry := map[string]any{
		"name": computerUsePluginName,
		"source": map[string]any{
			"source": "local",
			"path":   "./plugins/computer-use",
		},
		"policy": map[string]any{
			"installation":   "INSTALLED_BY_DEFAULT",
			"authentication": "ON_INSTALL",
		},
		"category": "Productivity",
	}
	var plugins []any
	if existing, ok := manifest["plugins"].([]any); ok {
		for _, item := range existing {
			itemMap, _ := item.(map[string]any)
			if stringFromAny(itemMap["name"]) == computerUsePluginName {
				continue
			}
			plugins = append(plugins, item)
		}
	}
	manifest["plugins"] = append([]any{entry}, plugins...)
	return atomicWriteJSON(manifestPath, manifest)
}

func updateComputerUseCodexConfig(home, marketplaceRoot string) (*string, error) {
	configPath := filepath.Join(home, "config.toml")
	contents := readFile(configPath)
	updated := upsertTomlTable(contents, "marketplaces."+computerUseMarketplace, []string{
		"last_updated = " + quoteToml(time.Now().UTC().Format(time.RFC3339)),
		`source_type = "local"`,
		"source = " + quoteToml(computerUseConfigSourcePath(marketplaceRoot)),
	})
	updated = upsertTomlTable(updated, fmt.Sprintf("plugins.%s", quoteToml(computerUsePluginName+"@"+computerUseMarketplace)), []string{
		"enabled = true",
	})
	updated = upsertTomlTable(updated, "windows", []string{
		`sandbox = "unelevated"`,
	})
	return writeCodexConfigWithBackup(configPath, updated, "computer-use")
}

func computerUseConfigSourcePath(path string) string {
	if runtime.GOOS == "windows" {
		if strings.HasPrefix(path, `\\?\`) {
			return path
		}
		return `\\?\` + path
	}
	return path
}

func setComputerUseUserEnv() error {
	_ = os.Setenv("CODEX_ELECTRON_ENABLE_WINDOWS_COMPUTER_USE", "1")
	if runtime.GOOS != "windows" {
		return nil
	}
	script := `[Environment]::SetEnvironmentVariable('CODEX_ELECTRON_ENABLE_WINDOWS_COMPUTER_USE','1','User'); $env:CODEX_ELECTRON_ENABLE_WINDOWS_COMPUTER_USE='1'; try { $signature = @'
using System;
using System.Runtime.InteropServices;
public static class CodexEnvBroadcast {
  [DllImport("user32.dll", SetLastError=true, CharSet=CharSet.Auto)]
  public static extern IntPtr SendMessageTimeout(IntPtr hWnd, uint Msg, UIntPtr wParam, string lParam, uint fuFlags, uint uTimeout, out UIntPtr lpdwResult);
}
'@; if (-not ('CodexEnvBroadcast' -as [type])) { Add-Type -TypeDefinition $signature }; $result = [UIntPtr]::Zero; [CodexEnvBroadcast]::SendMessageTimeout([IntPtr]0xffff, 0x001A, [UIntPtr]::Zero, 'Environment', 0x0002, 5000, [ref]$result) | Out-Null } catch { }`
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	hideSubprocessWindow(cmd)
	return cmd.Run()
}

func computerUseUserEnv() string {
	if runtime.GOOS != "windows" {
		return ""
	}
	return windowsRegistryString(`HKEY_CURRENT_USER\Environment`, "CODEX_ELECTRON_ENABLE_WINDOWS_COMPUTER_USE")
}

func computerUseHelperTransportJS() string {
	return "import { execFile } from \"node:child_process\";\n" +
		"import { appendFile, mkdir } from \"node:fs/promises\";\n" +
		"import { dirname, join } from \"node:path\";\n" +
		"import { promisify } from \"node:util\";\n\n" +
		"const execFileAsync = promisify(execFile);\n" +
		"const logPath = join(process.env.LOCALAPPDATA || process.env.TEMP || \".\", \"OpenAI\", \"Codex\", \"computer-use-local-helper.log\");\n\n" +
		"async function log(entry) {\n" +
		"  try {\n" +
		"    await mkdir(dirname(logPath), { recursive: true });\n" +
		"    await appendFile(logPath, new Date().toISOString() + \" \" + JSON.stringify(entry) + \"\\n\", \"utf8\");\n" +
		"  } catch {\n" +
		"  }\n" +
		"}\n\n" +
		"function encodePowerShell(script) {\n" +
		"  return Buffer.from(script, \"utf16le\").toString(\"base64\");\n" +
		"}\n\n" +
		"async function runPowerShell(script, timeout = 30000) {\n" +
		"  const { stdout } = await execFileAsync(\"powershell.exe\", [\"-NoProfile\", \"-NonInteractive\", \"-ExecutionPolicy\", \"Bypass\", \"-EncodedCommand\", encodePowerShell(script)], {\n" +
		"    encoding: \"utf8\",\n" +
		"    env: process.env,\n" +
		"    timeout,\n" +
		"    windowsHide: true,\n" +
		"    maxBuffer: 64 * 1024 * 1024,\n" +
		"  });\n" +
		"  const text = stdout.trim();\n" +
		"  return text.length === 0 ? null : JSON.parse(text);\n" +
		"}\n\n" +
		"function ps(strings) {\n" +
		"  return strings.join(\"\\n\");\n" +
		"}\n\n" +
		"function numberFrom(params, names, fallback = 0) {\n" +
		"  for (const name of names) {\n" +
		"    const value = params?.[name];\n" +
		"    if (typeof value === \"number\" && Number.isFinite(value)) return value;\n" +
		"    if (typeof value === \"string\" && value.trim() !== \"\" && Number.isFinite(Number(value))) return Number(value);\n" +
		"  }\n" +
		"  return fallback;\n" +
		"}\n\n" +
		"function buttonFrom(params) {\n" +
		"  const raw = String(params?.button || params?.mouseButton || \"left\").toLowerCase();\n" +
		"  if (raw.includes(\"right\")) return \"right\";\n" +
		"  if (raw.includes(\"middle\")) return \"middle\";\n" +
		"  return \"left\";\n" +
		"}\n\n" +
		"function keyFrom(params) {\n" +
		"  return String(params?.key || params?.keys || params?.text || params?.value || \"\");\n" +
		"}\n\n" +
		"function textFrom(params) {\n" +
		"  return String(params?.text ?? params?.value ?? params?.input ?? \"\");\n" +
		"}\n\n" +
		"const user32Script = ps([\n" +
		"  \"Add-Type -TypeDefinition @\\\"\",\n" +
		"  \"using System;\",\n" +
		"  \"using System.Runtime.InteropServices;\",\n" +
		"  \"public static class CodexUser32 {\",\n" +
		"  \"  [DllImport(\\\"user32.dll\\\")] public static extern bool SetCursorPos(int X, int Y);\",\n" +
		"  \"  [DllImport(\\\"user32.dll\\\")] public static extern void mouse_event(uint dwFlags, uint dx, uint dy, int dwData, UIntPtr dwExtraInfo);\",\n" +
		"  \"}\",\n" +
		"  \"\\\"@\",\n" +
		"]);\n\n" +
		"function mouseFlags(button, action) {\n" +
		"  if (button === \"right\") return action === \"down\" ? \"0x0008\" : \"0x0010\";\n" +
		"  if (button === \"middle\") return action === \"down\" ? \"0x0020\" : \"0x0040\";\n" +
		"  return action === \"down\" ? \"0x0002\" : \"0x0004\";\n" +
		"}\n\n" +
		"async function screenshot() {\n" +
		"  return await runPowerShell(ps([\n" +
		"    \"Add-Type -AssemblyName System.Windows.Forms\",\n" +
		"    \"Add-Type -AssemblyName System.Drawing\",\n" +
		"    \"$bounds = [System.Windows.Forms.SystemInformation]::VirtualScreen\",\n" +
		"    \"$bitmap = New-Object System.Drawing.Bitmap $bounds.Width, $bounds.Height\",\n" +
		"    \"$graphics = [System.Drawing.Graphics]::FromImage($bitmap)\",\n" +
		"    \"$graphics.CopyFromScreen($bounds.Left, $bounds.Top, 0, 0, $bounds.Size)\",\n" +
		"    \"$stream = New-Object System.IO.MemoryStream\",\n" +
		"    \"$bitmap.Save($stream, [System.Drawing.Imaging.ImageFormat]::Png)\",\n" +
		"    \"$graphics.Dispose()\",\n" +
		"    \"$bitmap.Dispose()\",\n" +
		"    \"$bytes = $stream.ToArray()\",\n" +
		"    \"$stream.Dispose()\",\n" +
		"    \"[Console]::OutputEncoding = [System.Text.Encoding]::UTF8\",\n" +
		"    \"[Console]::Write((ConvertTo-Json -Compress @{ mimeType = 'image/png'; data = [Convert]::ToBase64String($bytes); width = $bounds.Width; height = $bounds.Height; left = $bounds.Left; top = $bounds.Top }))\",\n" +
		"  ]), 30000);\n" +
		"}\n\n" +
		"async function screenInfo() {\n" +
		"  return await runPowerShell(ps([\n" +
		"    \"Add-Type -AssemblyName System.Windows.Forms\",\n" +
		"    \"$bounds = [System.Windows.Forms.SystemInformation]::VirtualScreen\",\n" +
		"    \"[Console]::OutputEncoding = [System.Text.Encoding]::UTF8\",\n" +
		"    \"[Console]::Write((ConvertTo-Json -Compress @{ width = $bounds.Width; height = $bounds.Height; left = $bounds.Left; top = $bounds.Top }))\",\n" +
		"  ]));\n" +
		"}\n\n" +
		"async function moveMouse(params) {\n" +
		"  const x = Math.round(numberFrom(params, [\"x\", \"X\", \"left\"]));\n" +
		"  const y = Math.round(numberFrom(params, [\"y\", \"Y\", \"top\"]));\n" +
		"  return await runPowerShell(ps([user32Script, \"[CodexUser32]::SetCursorPos(\" + x + \", \" + y + \") | Out-Null\", \"[Console]::Write('{\\\"ok\\\":true}')\"]));\n" +
		"}\n\n" +
		"async function clickMouse(params, count = 1) {\n" +
		"  const x = Math.round(numberFrom(params, [\"x\", \"X\", \"left\"], Number.NaN));\n" +
		"  const y = Math.round(numberFrom(params, [\"y\", \"Y\", \"top\"], Number.NaN));\n" +
		"  const button = buttonFrom(params);\n" +
		"  const down = mouseFlags(button, \"down\");\n" +
		"  const up = mouseFlags(button, \"up\");\n" +
		"  const lines = [user32Script];\n" +
		"  if (Number.isFinite(x) && Number.isFinite(y)) lines.push(\"[CodexUser32]::SetCursorPos(\" + x + \", \" + y + \") | Out-Null\");\n" +
		"  lines.push(\"for ($i = 0; $i -lt \" + count + \"; $i++) {\", \"  [CodexUser32]::mouse_event(\" + down + \", 0, 0, 0, [UIntPtr]::Zero)\", \"  Start-Sleep -Milliseconds 35\", \"  [CodexUser32]::mouse_event(\" + up + \", 0, 0, 0, [UIntPtr]::Zero)\", \"  Start-Sleep -Milliseconds 70\", \"}\", \"[Console]::Write('{\\\"ok\\\":true}')\");\n" +
		"  return await runPowerShell(ps(lines));\n" +
		"}\n\n" +
		"async function dragMouse(params) {\n" +
		"  const fromX = Math.round(numberFrom(params, [\"fromX\", \"startX\", \"x1\", \"x\"]));\n" +
		"  const fromY = Math.round(numberFrom(params, [\"fromY\", \"startY\", \"y1\", \"y\"]));\n" +
		"  const toX = Math.round(numberFrom(params, [\"toX\", \"endX\", \"x2\"]));\n" +
		"  const toY = Math.round(numberFrom(params, [\"toY\", \"endY\", \"y2\"]));\n" +
		"  return await runPowerShell(ps([user32Script, \"[CodexUser32]::SetCursorPos(\" + fromX + \", \" + fromY + \") | Out-Null\", \"Start-Sleep -Milliseconds 80\", \"[CodexUser32]::mouse_event(0x0002, 0, 0, 0, [UIntPtr]::Zero)\", \"Start-Sleep -Milliseconds 120\", \"[CodexUser32]::SetCursorPos(\" + toX + \", \" + toY + \") | Out-Null\", \"Start-Sleep -Milliseconds 120\", \"[CodexUser32]::mouse_event(0x0004, 0, 0, 0, [UIntPtr]::Zero)\", \"[Console]::Write('{\\\"ok\\\":true}')\"]));\n" +
		"}\n\n" +
		"async function scrollMouse(params) {\n" +
		"  const delta = Math.round(numberFrom(params, [\"delta\", \"wheelDelta\"], 0) || -120 * numberFrom(params, [\"amount\", \"clicks\"], 1));\n" +
		"  return await runPowerShell(ps([user32Script, \"[CodexUser32]::mouse_event(0x0800, 0, 0, \" + delta + \", [UIntPtr]::Zero)\", \"[Console]::Write('{\\\"ok\\\":true}')\"]));\n" +
		"}\n\n" +
		"function sendKeysLiteral(text) {\n" +
		"  return text.replaceAll(\"{\", \"{{}\").replaceAll(\"}\", \"{}}\").replaceAll(\"+\", \"{+}\").replaceAll(\"^\", \"{^}\").replaceAll(\"%\", \"{%}\").replaceAll(\"~\", \"{~}\").replaceAll(\"(\", \"{(}\").replaceAll(\")\", \"{)}\").replaceAll(\"[\", \"{[}\").replaceAll(\"]\", \"{]}\").replaceAll(\"\\n\", \"{ENTER}\");\n" +
		"}\n\n" +
		"function normalizeKey(key) {\n" +
		"  const value = String(key).trim();\n" +
		"  const upper = value.toUpperCase();\n" +
		"  const aliases = { ENTER: \"{ENTER}\", RETURN: \"{ENTER}\", ESC: \"{ESC}\", ESCAPE: \"{ESC}\", TAB: \"{TAB}\", BACKSPACE: \"{BACKSPACE}\", DELETE: \"{DELETE}\", DEL: \"{DELETE}\", SPACE: \" \", UP: \"{UP}\", DOWN: \"{DOWN}\", LEFT: \"{LEFT}\", RIGHT: \"{RIGHT}\", HOME: \"{HOME}\", END: \"{END}\", PAGEUP: \"{PGUP}\", PAGEDOWN: \"{PGDN}\" };\n" +
		"  if (aliases[upper]) return aliases[upper];\n" +
		"  if (/^F([1-9]|1[0-2])$/.test(upper)) return \"{\" + upper + \"}\";\n" +
		"  return sendKeysLiteral(value);\n" +
		"}\n\n" +
		"async function sendKeys(keys) {\n" +
		"  const encoded = Buffer.from(keys, \"utf8\").toString(\"base64\");\n" +
		"  return await runPowerShell(ps([\"Add-Type -AssemblyName System.Windows.Forms\", \"$keys = [System.Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('\" + encoded + \"'))\", \"[System.Windows.Forms.SendKeys]::SendWait($keys)\", \"[Console]::Write('{\\\"ok\\\":true}')\"]));\n" +
		"}\n\n" +
		"async function typeText(params) {\n" +
		"  return await sendKeys(sendKeysLiteral(textFrom(params)));\n" +
		"}\n\n" +
		"async function keypress(params) {\n" +
		"  return await sendKeys(normalizeKey(keyFrom(params)));\n" +
		"}\n\n" +
		"export class WindowsHelperTransport {\n" +
		"  constructor({ helperArgs = [], helperCommand = null } = {}) {\n" +
		"    this.helperArgs = helperArgs;\n" +
		"    this.helperCommand = helperCommand;\n" +
		"    log({ event: \"transport-created\", helperCommand, helperArgs }).catch(() => {});\n" +
		"  }\n\n" +
		"  async request(method, params = {}, options = {}) {\n" +
		"    await log({ event: \"request\", method, params, hasTurnMetadata: !!options?.codexTurnMetadata });\n" +
		"    const name = String(method || \"\").replace(/[-_]/g, \"\").toLowerCase();\n" +
		"    if (name === \"ping\") return \"pong\";\n" +
		"    if ([\"screenshot\", \"takescreenshot\", \"capture\", \"captureimage\", \"capturescreen\", \"screencapture\"].includes(name)) return await screenshot(params);\n" +
		"    if ([\"screeninfo\", \"getscreeninfo\", \"displays\", \"getdisplays\", \"screenstate\"].includes(name)) return await screenInfo(params);\n" +
		"    if ([\"movemouse\", \"mousemove\", \"move\"].includes(name)) return await moveMouse(params);\n" +
		"    if ([\"click\", \"mouseclick\", \"clickmouse\"].includes(name)) return await clickMouse(params, 1);\n" +
		"    if ([\"doubleclick\", \"mousedoubleclick\"].includes(name)) return await clickMouse(params, 2);\n" +
		"    if ([\"drag\", \"mousedrag\", \"dragmouse\"].includes(name)) return await dragMouse(params);\n" +
		"    if ([\"scroll\", \"mousescroll\", \"scrollmouse\"].includes(name)) return await scrollMouse(params);\n" +
		"    if ([\"type\", \"typetext\", \"text\"].includes(name)) return await typeText(params);\n" +
		"    if ([\"keypress\", \"presskey\", \"key\", \"sendkey\"].includes(name)) return await keypress(params);\n" +
		"    if ([\"close\", \"shutdown\"].includes(name)) return { ok: true };\n" +
		"    await log({ event: \"unknown-method\", method, params });\n" +
		"    throw new Error(\"Unsupported local Computer Use helper method: \" + method);\n" +
		"  }\n\n" +
		"  async close() {\n" +
		"    await log({ event: \"transport-closed\" });\n" +
		"  }\n" +
		"}\n"
}
