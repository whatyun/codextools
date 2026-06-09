# CodexTools

[![中文](https://img.shields.io/badge/%F0%9F%87%A8%F0%9F%87%B3-%E4%B8%AD%E6%96%87-0f766e)](./README.md)
![English](https://img.shields.io/badge/%F0%9F%87%BA%F0%9F%87%B8-English-2563eb)
[![日本語](https://img.shields.io/badge/%F0%9F%87%AF%F0%9F%87%B5-%E6%97%A5%E6%9C%AC%E8%AA%9E-d97706)](./README.ja.md)
[![한국어](https://img.shields.io/badge/%F0%9F%87%B0%F0%9F%87%B7-%ED%95%9C%EA%B5%AD%EC%96%B4-7c3aed)](./README.ko.md)

CodexTools is a standalone Go + React desktop manager for Codex setup, launch, connection modes, UI enhancements, scripts, diagnostics, and repair workflows.
It keeps the task-oriented manager UI, relay and provider tooling, script management, local repair actions, packaged downloads, and support diagnostics in one independently buildable repository.

## What It Includes

- A Go backend for local command dispatch, static UI serving, and desktop launch behavior.
- A React manager UI redesigned for non-technical users.
- First-run setup, launch status, connection mode selection, relay configuration, conversation repair, scripts, logs, diagnostics, and maintenance tools.
- macOS and Windows desktop downloads published from this repository.
- A standalone repository layout that no longer depends on the original monorepo path.

## Screenshots

### Home Dashboard

![CodexTools home dashboard showing launch status, connection mode, UI enhancement mode, and repair entry points](./docs/assets/screenshot-home.png)

The first screen shows whether the local setup is ready, exposes the primary launch button, and keeps connection, UI features, repair entry points, and key status together.

### Beginner Guide

![CodexTools beginner guide showing system detection, Codex install status, CCSwitch import, mode selection, and launch steps](./docs/assets/screenshot-onboarding.png)

The guided flow checks the system, verifies Codex installation, imports CCSwitch providers, selects a connection mode, and ends at launch.

## FAQ

This section is adapted from the [BigPizzaV3/CodexPlusPlus](https://github.com/BigPizzaV3/CodexPlusPlus) FAQ and rewritten for CodexTools.

### The CodexTools Menu Does Not Appear

Make sure Codex was launched from the `Codex++` entry point instead of the original Codex entry. You can also open the manager's Diagnostics and Logs pages to inspect injection status.

### The Plugin Says the Backend Cannot Be Reached

First test the local backend in a browser or PowerShell:

```powershell
Invoke-RestMethod -Method Post -Uri http://127.0.0.1:57321/backend/status -Body "{}" -ContentType "application/json"
```

If the endpoint works but the plugin still times out, the issue is usually the CDP bridge or script cache inside the Codex page. Restart Codex through CodexTools, or check manager logs for `renderer.script_loaded`, `bridge.request`, and `bridge.response`.

### How Is Upstream Worktree Different From Codex Native Worktree Creation?

CodexTools Upstream worktree is equivalent to updating the remote branch first, then running:

```bash
git worktree add -b <new-branch> <worktree-path> upstream/<base-branch>
```

This makes the new worktree start from the latest remote-tracking branch instead of the current local HEAD. If CodexTools cannot safely detect the native worktree form for the current Codex version, manually fill in repository path, branch name, worktree path, remote, and base branch from the CodexTools menu.

### macOS Says the App Cannot Be Opened or Is Damaged

Unsigned or non-notarized builds may be blocked by macOS Gatekeeper, either at the `.pkg` installer stage or after the apps are installed.

![macOS warning that Codex++ Manager is damaged](./docs/assets/macos-damaged-warning.png)

Run these commands in Terminal to remove the quarantine attribute:

```bash
sudo xattr -rd com.apple.quarantine ~/Downloads/CodexTools-*-macos-*.pkg
sudo xattr -rd com.apple.quarantine "/Applications/Codex++ 管理工具.app"
sudo xattr -rd com.apple.quarantine "/Applications/Codex++.app"
```

If the installer is blocked, run the first command on the downloaded `.pkg` and install again. If launch is blocked after installation, run the two `/Applications` commands, then reopen `Codex++` or `Codex++ 管理工具`.

### Can macOS Intel Use It?

Yes. Releases provide separate `macos-x64` and `macos-arm64` packages. Intel Macs should use x64; Apple Silicon Macs should use arm64.

## Local Development

```bash
npm --prefix web install
npm --prefix web run vite:build
go run .
```

## Build

```bash
npm --prefix web run check
npm --prefix web run vite:build
go build -o codextools .
```

## Downloads

- Download page: [docs/downloads.html](./docs/downloads.html)
- Windows ships x64 and arm64 installers, with portable zip builds retained for both architectures.
- macOS ships Apple Silicon (`macos-arm64`) and Intel (`macos-x64`) packages, with portable zip builds retained.

## Community

- Telegram: `https://t.me/wanai8`
- LINUX DO: <https://linux.do/>

## Origin and Thanks

CodexTools is a standalone Go refactor and manager UI project based on earlier Codex++ work.
Thanks to the original Codex++ project for the foundation, workflow ideas, and user-facing tool direction.

- Original project: <https://github.com/BigPizzaV3/CodexPlusPlus>
- Refactor source: <https://github.com/hereww/CodexPlusPlus>
- Standalone project: <https://github.com/hereww/codextools>
