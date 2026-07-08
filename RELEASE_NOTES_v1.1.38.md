## CodexTools 1.1.38

### Changes

- Improves the restart flow when Codex is installed in protected Windows package paths that require shell activation.
- Lets restart requests close the existing Codex++ launcher, wait for the guard lock to clear, and relaunch cleanly instead of silently doing nothing.
- Adds clearer restart failure statuses and diagnostics when the old launcher or protected app process cannot exit in time.
- Updates the manager restart UI with a blocking progress state, localized copy, and status details for protected Windows installs.
- Adds regression coverage for launcher restart handoff, protected Windows package activation, and restart status handling.

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
