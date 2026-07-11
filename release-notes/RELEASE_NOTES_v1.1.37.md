## CodexTools 1.1.37

### Changes

- Regenerates CodexPlusPlus relay provider configuration when switching mixed API profiles that still contain an old official-only config snapshot.
- Ensures mixed API mode writes the selected relay API key into the generated model provider config instead of leaving the profile on `openai`.
- Restores existing official-mode session history into the mixed provider view by syncing rollout metadata and SQLite thread indexes to `CodexPlusPlus`.
- Restores `thread_source` and `has_user_event` metadata for mixed-mode history after provider sync.
- Adds regression coverage for stale mixed official snapshots, provider sync, rollout metadata rewriting, and history restoration.

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
