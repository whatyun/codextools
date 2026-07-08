## CodexTools 1.2.0

### Major changes

- Starts the 1.2.x major line with per-relay HTTP proxy support.
- Adds HTTP proxy settings to relay profiles and uses them for relay API tests, model catalog refreshes, local relay forwarding, Responses/image routes, and aggregate relay member failover.
- Routes Responses providers through the local relay proxy when HTTP proxying is enabled, when image generation is disabled, when a separate image API is configured, or when aggregate relay behavior is needed.
- Improves Windows launch reliability by rejecting protected Store/MSIX `Program Files\WindowsApps` paths as Codex++ launch targets before activation can fail misleadingly.
- Adds automatic Windows mirror package download/extract repair when no directly runnable `Codex.exe` is found, improving offline-style installation and recovery for packaged installs.
- Refreshes the Windows repair and first-run guidance to prefer mirror/unpacked `Codex.exe` paths and to show clearer MSIX unsupported diagnostics.
- Keeps the 1.1.37-1.1.39 fixes for mixed-provider config regeneration, official history restoration, protected MSIX rejection, restart handoff, and clearer restart diagnostics.
- Ships refreshed offline installers and portable packages for macOS and Windows, including Windows x64/arm64 setup installers and zip builds.

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
