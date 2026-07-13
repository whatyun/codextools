## CodexTools 1.2.6

### Changes

- Refines relay routing for image-generation endpoints so image creation and image edit requests follow the intended provider path without being mistaken for text-only model calls.
- Adds regression coverage for image relay routing to keep OpenAI-compatible image endpoints stable across official, hybrid, and relay modes.
- Refreshes the README feature overview and promotes the English homepage as the main repository entry point.
- Moves historical release notes into a dedicated `release-notes/` directory and keeps the current GitHub Release description focused on this patch.
- Removes duplicated release assets from GitHub Pages so the project page stays lighter while GitHub Releases remain the source for installers.

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
