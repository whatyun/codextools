## CodexTools 1.2.2

### Changes

- Renames the desktop entry, app bundles, installers, and in-app copy to ChatGPT Codex Tools while cleaning up legacy Codex++ shortcuts and uninstall records.
- Refactors the manager UI and backend payload handling for clearer status cards, relay/provider settings, marketplace actions, and diagnostics.
- Keeps plugin and site marketplace enhancements available for mixed API relay mode while disabling only the paths that are unsafe for pure relay/aggregate mode.
- Improves ChatGPT desktop target detection, CDP page selection, launcher errors, and Windows startup/uninstall behavior.
- Refreshes macOS pkg/zip and Windows setup/portable build metadata, including offline installers and migration-friendly package names.
- Updates README and localized UI text across Chinese, English, Japanese, and Korean documentation.

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
