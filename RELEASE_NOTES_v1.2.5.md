## CodexTools 1.2.5

### Changes

- Makes Windows full restart safer by coalescing duplicate restart requests, releasing the restart lock once the new launcher is ready, and avoiding parallel restarts that could fight over the same ChatGPT/Codex instance.
- Only closes a Windows debugging port through CDP when the target really belongs to the selected ChatGPT/Codex app, and records an explicit diagnostic when an unrelated process owns the port.
- Adds Windows-specific restart cleanup that identifies the target packaged app or executable, waits briefly for a graceful exit, then stops remaining ChatGPT/Codex process trees before relaunch when needed.
- Replaces the generic debug-port wait with a Windows-aware release check that verifies bindability, live TCP acceptance, LISTENING owners, and pre-LISTEN bound owners before reusing the port.
- Improves diagnostics for restart failures by reporting coalesced retries, skipped unowned CDP shutdown, stopped Windows target PIDs, unavailable debug ports, and temporary CDP outages without flooding retry logs.
- Keeps the bridge watchdog calmer on Windows by suppressing repetitive retry noise and only logging the first and last injection retry unless the platform can safely tolerate full retry traces.
- Adds regression coverage for the new Windows restart guard and debug-port release logic so restart behavior stays predictable across packaged and unpackaged desktop installs.

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
