## CodexTools 1.2.1

### Changes

- Fixes macOS launcher discovery so the manager opens the sibling `Codex++.app` launcher instead of falling back to a nested launcher inside `Codex++ 管理工具.app`.
- Prefers the injected bridge for backend calls while keeping the HTTP helper fallback for status and repair routes, with clearer diagnostics when either transport is unavailable.
- Downgrades missing Codex service-tier assets to a non-fatal unavailable state instead of breaking the injected menu on unsupported Codex builds.
- Allows the local HTTP helper to answer private-network browser preflight requests and extends thread sort-key requests for larger history sets.
- Improves Windows mirror repair by reusing matching packages already present in Downloads before downloading again.
- Removes stale provider-sync locks automatically while keeping active locks protected, reducing stuck history/provider sync states.
- Keeps the 1.2.x HTTP proxy, Windows launch optimization, and offline installer support from 1.2.0.

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
