## CodexTools 1.2.3

### Changes

- Removes the obsolete plugin/Sites marketplace unlock patch and automatic expansion scanner. Recent Codex builds provide the marketplace natively; avoiding request/response rewrites, global `Array.prototype.filter` replacement, local payload injection, and repeated DOM scans prevents the marketplace renderer crash seen after several seconds.
- Removes the corresponding settings and injected marketplace payload while retaining native marketplace access, plugin configuration repair, legacy entry compatibility, and the separate force-install control.
- Adds a conversation-history compatibility repair for legacy tool-call records containing unsupported top-level `namespace` fields, with process and disk-space preflight checks, automatic backups, progress reporting, cancellation, and concurrent-write protection.
- Improves provider-sync lock ownership checks across macOS, Linux, and Windows so active maintenance locks stay protected while locks from missing processes can be recovered safely.
- Improves the native macOS manager window with working JavaScript alert/confirm dialogs and orderly shutdown of background history-repair work before the local server exits.
- Updates Vite, Babel, and Tailwind build dependencies to patched versions with a clean npm audit, and splits React, drag-and-drop, UI, and Tauri dependencies into cacheable chunks so production builds complete without deprecation or oversized-chunk warnings.
- Expands automated coverage for history scanning and repair, backup safety, cancellation, disk-space validation, process guards, and the absence of retired marketplace/auto-expand runtime patches.

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
