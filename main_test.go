package main

import "testing"

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
