## CodexTools 1.1.36

### Changes

- Prevents macOS app path selection from accepting CodexTools, Codex++, or renamed CodexTools manager bundles as the upstream Codex app.
- Detects CodexTools bundle identifiers and executables in `Info.plist` so renamed manager apps are filtered safely.
- When switching back to official mode, explicitly restores `model_provider = "openai"` after removing relay provider settings.
- Runs provider synchronization back to `openai` for existing session indexes when returning to official mode.
- Adds regression tests for CodexTools app filtering, official provider restoration, and official-mode provider sync.

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
