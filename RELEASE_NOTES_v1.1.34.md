## CodexTools 1.1.34

### Changes

- Preserves the current ChatGPT login when switching into pure API relay mode, instead of restoring stale provider auth snapshots.
- Refreshes bound official auth snapshots from the current login when switching mixed API or official-login provider modes.
- Runs provider-state synchronization after relay switches so historical sessions and SQLite thread indexes follow the selected provider mode.
- Returns provider sync details in relay switch responses and refreshes the manager form after applying a provider switch.
- Adds tests for pure API auth preservation, mixed-mode auth refresh, and provider sync during relay switching.

### macOS unsigned build notice

The macOS packages are unsigned community builds, including the pkg installers. If macOS blocks the first launch, run:

```bash
xattr -cr "/Applications/Codex++ 管理工具.app"
xattr -cr "/Applications/Codex++.app"
```

### macOS 首次启动提醒

macOS 包是未签名的社区构建，pkg 安装包也一样。如果 macOS 阻止首次启动，请执行：

```bash
xattr -cr "/Applications/Codex++ 管理工具.app"
xattr -cr "/Applications/Codex++.app"
```
