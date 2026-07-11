## CodexTools 1.2.3

### Changes

- Fixes plugin cards failing when opened on recent Codex builds by converting injected local marketplace entries to the current `PluginSummary` format, including local source paths, version state, policy fields, and normalized interface metadata.
- Tightens plugin marketplace response correlation so only responses belonging to tracked plugin-list requests are modified, preventing unrelated app-server messages from being patched.
- Adds a conversation-history compatibility repair for legacy tool-call records containing unsupported top-level `namespace` fields, with process and disk-space preflight checks, automatic backups, progress reporting, cancellation, and concurrent-write protection.
- Improves provider-sync lock ownership checks across macOS, Linux, and Windows so active maintenance locks stay protected while locks from missing processes can be recovered safely.
- Improves the native macOS manager window with working JavaScript alert/confirm dialogs and orderly shutdown of background history-repair work before the local server exits.
- Expands automated coverage for history scanning and repair, backup safety, cancellation, disk-space validation, process guards, plugin response matching, and modern local plugin payloads.

### macOS unsigned build notice

The macOS packages are unsigned community builds, including the pkg installers. If macOS blocks the first launch, run:

```bash
xattr -cr "/Applications/ChatGPT Codex 管理工具.app"
xattr -cr "/Applications/ChatGPT Codex.app"
xattr -cr "/Applications/Codex++ 管理工具.app"
xattr -cr "/Applications/Codex++.app"
```

### macOS 首次启动提醒

macOS 包是未签名的社区构建，pkg 安装包也一样。如果 macOS 阻止首次启动，请执行：

```bash
xattr -cr "/Applications/ChatGPT Codex 管理工具.app"
xattr -cr "/Applications/ChatGPT Codex.app"
xattr -cr "/Applications/Codex++ 管理工具.app"
xattr -cr "/Applications/Codex++.app"
```
