## CodexTools 1.1.33

### Changes

- Added mobile control relay hosting so CodexTools can proxy local helper and managed app traffic through an encrypted relay room.
- Added local `openai-curated` plugin marketplace initialization and repair from `openai/plugins`, including config registration in `config.toml`.
- Added aggregate relay profiles with failover, request round-robin, conversation round-robin, and weighted round-robin selection strategies.
- Expanded the manager UI for mobile control, plugin marketplace repair, aggregate relay membership, and related workflow settings.
- Improved Codex plugin marketplace injection for newer Codex builds, including bridge/fetch request patching, local marketplace merge support, and plugin query cache invalidation.
- Added backend and bridge tests covering marketplace, relay rotation, workflow, and mobile-control configuration behavior.

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
