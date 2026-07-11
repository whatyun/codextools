<div align="center">

# ChatGPT Codex Tools

**A desktop control center for running, configuring, extending, and repairing Codex in the ChatGPT desktop app.**

[![Latest release](https://img.shields.io/github/v/release/hereww/codextools?display_name=tag&style=flat-square)](https://github.com/hereww/codextools/releases/latest)
[![Windows](https://img.shields.io/badge/Windows-x64%20%7C%20ARM64-0078D4?style=flat-square&logo=windows)](https://github.com/hereww/codextools/releases/latest)
[![macOS](https://img.shields.io/badge/macOS-Apple%20Silicon%20%7C%20Intel-000000?style=flat-square&logo=apple)](https://github.com/hereww/codextools/releases/latest)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?style=flat-square&logo=go&logoColor=white)](./go.mod)
[![React](https://img.shields.io/badge/React-19-61DAFB?style=flat-square&logo=react&logoColor=111827)](./web/package.json)

[Download](https://github.com/hereww/codextools/releases/latest) · [Download guide](https://hereww.github.io/codextools/downloads.html) · [Build from source](#build-from-source) · [Troubleshooting](#troubleshooting)

**English** · [简体中文](./README.zh-CN.md) · [日本語](./README.ja.md) · [한국어](./README.ko.md)

</div>

ChatGPT Codex Tools brings the scattered parts of a local Codex setup into one approachable desktop manager. Launch ChatGPT with the right connection mode, switch API providers, manage enhancements and scripts, repair missing conversation history, and collect useful diagnostics without editing configuration files by hand.

> [!IMPORTANT]
> This is an independent community project. It is not affiliated with or endorsed by OpenAI. You still need the official [ChatGPT desktop app](https://chatgpt.com/download/) and must follow the terms of any service or API provider you use.

## Preview

![ChatGPT Codex Tools dashboard showing environment status, launch controls, connection mode, enhancements, and repair shortcuts](./docs/assets/screenshot-home.png)

<details>
<summary><strong>See the guided first-run setup</strong></summary>

![First-run guide showing system detection, ChatGPT installation status, CCSwitch import, connection selection, and launch](./docs/assets/screenshot-onboarding.png)

</details>

## What Makes It Different

ChatGPT Codex Tools is more than an alternative launcher. It manages the local state around ChatGPT Codex and keeps connection settings, conversation ownership, extensions, and recovery workflows consistent when you change providers or desktop versions.

- **One control plane instead of scattered config files.** Change providers, models, MCP servers, Skills, Plugins, enhancements, and scripts from a desktop UI. The manager applies the related local configuration together instead of requiring manual edits across several files.
- **Official access and custom APIs can coexist.** Hybrid API mode keeps an official ChatGPT account bound for native capabilities while sending supported model requests through a configured API endpoint. Pure relay mode is also available when official sign-in is not required.
- **Provider switching includes state migration.** When the active mode changes, the manager can synchronize provider attribution in local conversation history so existing threads remain visible under the new setup.
- **Risky maintenance actions are backup-first.** Conversation repair and Skill/MCP restoration validate prerequisites, create recoverable snapshots, and limit changes to the relevant records or TOML tables.
- **Enhancements live inside the Codex experience.** A local renderer bridge adds practical actions to the ChatGPT Codex interface rather than forcing every workflow back into a separate utility window.

## Features

### Guided setup and reliable launching

- Detects the operating system, CPU architecture, ChatGPT installation, manager runtime, and required local paths.
- Finds CCSwitch when installed and imports its Codex provider profiles; CCSwitch is optional and can be skipped.
- Creates the dedicated **ChatGPT Codex** entry point and launches ChatGPT with the selected helper, bridge, and connection mode.
- Shows readiness, active provider, enhancement mode, entry-point status, and common repairs on the home dashboard.
- Handles restarts and single-instance coordination so stale helper processes are less likely to break a launch.

### Three connection modes

| Mode | Best for | Behavior |
| --- | --- | --- |
| **Official sign-in** | Using the standard ChatGPT account and native Codex behavior | Keeps requests and account state on the official path. |
| **Official hybrid API** | Keeping official login-dependent features while using another compatible model endpoint | Binds a provider to an official account, preserves native site/plugin capabilities, and routes supported API traffic through the configured Base URL and key. |
| **Relay API** | Using a compatible proxy, aggregator, or self-hosted endpoint without official login | Applies the selected provider, protocol, model list, context window, optional upstream proxy, and local relay settings. |

Provider management includes reusable profiles, drag-to-reorder, connectivity tests with a chosen model, model-list and context-window overrides, protocol selection, optional per-provider proxies, and imported CCSwitch metadata. Aggregated profiles can rotate requests across multiple API providers according to the configured strategy.

### Codex interface enhancements

Enhancements can be enabled individually and are applied to the ChatGPT Codex renderer through the local bridge:

- Unlock locally configured and upstream-discovered models in the model picker.
- Delete conversations and export them as timestamped Markdown.
- Move projects and add a conversation Timeline for easier navigation.
- Create a Git worktree from the latest `upstream/<base-branch>` instead of the current local `HEAD`.
- Load local user scripts and marketplace scripts without manually editing the desktop application bundle.
- Expose a local mobile-control entry point through the built-in helper.

The manager distinguishes full, hybrid, and compatible enhancement modes. In particular, hybrid mode preserves official account-dependent site and plugin behavior, while pure relay mode avoids enabling features that require official access.

### MCP, Skills, Plugins, and scripts

- Manage Codex MCP servers, Skills, Plugins, marketplaces, and related feature flags from one screen.
- Merge provider-specific configuration into `config.toml` during provider changes while preserving unrelated common configuration.
- Scan locally cached plugins and restore missing plugin, marketplace, and `node_repl` MCP entries.
- Create, label, list, restore, and delete Skill/MCP snapshots stored under `~/.codex/backups_state/skill-mcp`.
- Browse the script marketplace, install or update scripts, toggle them individually, and manage local scripts alongside marketplace items.

### Conversation history repair

Two focused repair flows address common cases where older Codex threads disappear or fail to load:

- **Mode-history synchronization** updates provider ownership and local indexes after a connection-mode change. It creates a full backup first and does not delete message content.
- **Response-item repair** removes only the incompatible top-level `namespace` field from affected historical tool-call payloads. It leaves message text, tool output, and nested arguments untouched.

Before writing, the repair flow requires ChatGPT and Codex to be closed, scans candidate files, calculates the required backup space, and stops safely when prerequisites are not met. Progress, changed-file counts, repaired-record counts, and the backup directory are shown in the UI.

### Diagnostics and recovery

- Live logs cover launcher activity, renderer script loading, bridge requests, bridge responses, and repair operations.
- The generated diagnostic report includes application version, platform, important paths, and relevant settings for support requests.
- Maintenance actions repair desktop entry points, configuration paths, provider synchronization, plugin state, and other local integration problems.
- Backups are surfaced in the UI so a repair or restore operation has a clear recovery path rather than being a one-way mutation.

## Download

Use the [latest GitHub release](https://github.com/hereww/codextools/releases/latest) or the friendlier [download guide](https://hereww.github.io/codextools/downloads.html).

| Platform | Installer | Portable build |
| --- | --- | --- |
| macOS Apple Silicon | `macos-arm64` | ARM64 zip |
| macOS Intel | `macos-x64` | x64 zip |
| Windows PC | `windows-x64` | x64 zip |
| Windows ARM | `windows-arm64` | ARM64 zip |

> [!WARNING]
> Current macOS packages are not signed or notarized. Gatekeeper may report that the installer or app cannot be opened. See [macOS blocks the app](#macos-blocks-the-app) before changing any system security setting.

## Getting Started

1. Install the official [ChatGPT desktop app](https://chatgpt.com/download/).
2. Download the package that matches your operating system and CPU architecture.
3. Open **ChatGPT Codex Manager** and complete the guided setup.
4. Choose an official or compatible API connection.
5. Launch ChatGPT from the **ChatGPT Codex** entry created by the manager.

The manager keeps the primary launch action, connection state, enhancements, and repair shortcuts on its home screen. Advanced configuration remains available when you need it, but is not required for a normal first launch.

## Build From Source

### Prerequisites

- [Go 1.26](https://go.dev/dl/)
- A current [Node.js](https://nodejs.org/) and npm installation
- Platform build tools required by Go and the desktop webview

### Run locally

```bash
npm --prefix web install
npm --prefix web run vite:build
go run .
```

### Validate and build

```bash
npm --prefix web run check
npm --prefix web run vite:build
go test ./...
go build -o codextools .
```

## How It Works

The project uses a Go backend and a React 19 + Vite frontend. The backend discovers the local ChatGPT installation, manages settings and providers, launches the appropriate desktop processes, and exposes a loopback-only manager service. A Chrome DevTools Protocol bridge connects approved UI actions inside ChatGPT to the local helper.

```text
React manager UI
       |
Local Go manager  ── settings, providers, repair, scripts, diagnostics
       |
Desktop launcher ── ChatGPT process + renderer bridge
       |
ChatGPT Codex UI enhancements
```

The Go backend remains in a single `main` package so release builds can select binary roles through `-ldflags "-X main.binaryRole=..."`. Responsibilities are separated across focused files such as `manager.go`, `launcher.go`, `bridge.go`, `relay.go`, `repair.go`, and `diagnostics.go`.

## Troubleshooting

### The ChatGPT Codex menu does not appear

Make sure ChatGPT was opened through the **ChatGPT Codex** entry rather than its normal application shortcut. Open **Diagnostics** and **Logs** in the manager to check the launch and injection state.

### The plugin cannot reach the backend

Test the local endpoint from PowerShell:

```powershell
Invoke-RestMethod -Method Post -Uri http://127.0.0.1:57321/backend/status -Body "{}" -ContentType "application/json"
```

If the endpoint responds but the plugin still times out, restart ChatGPT through ChatGPT Codex. In the logs, look for `renderer.script_loaded`, `bridge.request`, and `bridge.response`; missing events usually point to the renderer bridge or a stale script cache.

### macOS blocks the app

If Gatekeeper blocks the downloaded package, remove quarantine from that package and retry the installation:

```bash
sudo xattr -rd com.apple.quarantine ~/Downloads/ChatGPT-Codex-Tools-*-macos-*.pkg
```

If the installed apps are blocked at launch, remove quarantine from those apps:

```bash
sudo xattr -rd com.apple.quarantine "/Applications/ChatGPT Codex 管理工具.app"
sudo xattr -rd com.apple.quarantine "/Applications/ChatGPT Codex.app"
```

Only run these commands for files you downloaded from this project's official GitHub releases and understand the security implications of bypassing quarantine.

### How is Upstream worktree different from native Codex worktree creation?

The Upstream worktree action updates the remote branch first and then performs the equivalent of:

```bash
git worktree add -b <new-branch> <worktree-path> upstream/<base-branch>
```

The new worktree therefore starts from the latest remote-tracking branch instead of the current local `HEAD`. If the manager cannot safely detect the native worktree form in your ChatGPT version, enter the repository, branch, worktree path, remote, and base branch manually from the ChatGPT Codex Tools menu.

## Project Status

ChatGPT Codex Tools is actively developed as an open community project. It does not accept sponsorships or donations. Source, product direction, and release history remain public so the project can be studied, discussed, and forked.

No license file is currently included in the repository. Do not assume redistribution rights beyond those explicitly granted by the copyright holder.

## Community and Resources

- [GitHub Releases](https://github.com/hereww/codextools/releases)
- [Codex documentation](https://developers.openai.com/codex/)
- [Telegram community](https://t.me/wanai8)
- [LINUX DO](https://linux.do/)

## Acknowledgements

Thanks to the earlier community projects that established the foundation, workflows, and user-facing direction behind this independent Go refactor, and to everyone reporting compatibility issues across ChatGPT desktop releases.
