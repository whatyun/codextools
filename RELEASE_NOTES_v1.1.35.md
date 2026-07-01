## CodexTools 1.1.35

### Changes

- Replaces the old forced plugin entry unlock with plugin auto-expand for the current upstream plugin marketplace flow.
- Adds enhancement toggles for paste fixes, fast startup arguments, forced Chinese locale, and native menu localization.
- Adds a local helper mobile entry at `/mobile` with app-server status and WebSocket proxy endpoints.
- Adds pending provider import confirmation so external tools can create relay profiles after user approval.
- Supports model context-window metadata through model suffixes like `deepseek-v4[1M]` and explicit model window JSON.
- Adds tests for v1.2.24-style runtime injection, default enhancement settings, provider import confirmation, model window catalog output, and deprecated mobile relay behavior.

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
