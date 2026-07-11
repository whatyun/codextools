## CodexTools 1.1.32

### Changes

- Added optional thread ID badges in the Codex sidebar, showing a short session ID and UUIDv7-derived creation time for easier history lookup.
- Improved Markdown export by using the browser save-file picker when available, with fallback download behavior and explicit cancellation handling.
- Added detection and cleanup for conflicting `OPENAI_*` environment variables, including process/user scope reporting and platform-specific removal support.
- Hardened session export and injection behavior with additional tests, safer workspace sidebar patching, and backend support for environment conflict status and removal.

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
