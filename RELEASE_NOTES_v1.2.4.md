## CodexTools 1.2.4

### Changes

- Adds reliable support for the unified Windows ChatGPT/Codex Microsoft Store app by recognizing official `OpenAI.Codex` and `OpenAI.ChatGPT` MSIX identities, resolving manifest-declared App Execution Alias or FullTrust entrypoints, and verifying the package, process, and debugging connection before accepting a launch.
- Synchronizes conversation-history ownership when switching between official, mixed API, and relay modes. The manager now shows the applied mode, synchronization status, backup location, manual retry action, and a separate action to reapply the current provider without deleting message content.
- Makes provider and history synchronization failure-safe with shared maintenance locks, complete backups of session, SQLite, global state, and configuration files, atomic writes, concurrent-change checks, one-transaction SQLite updates, and rollback with explicit partial-recovery reporting.
- Separates saving a relay profile from applying it, reloads current Windows relay settings without rewriting mode or history files, canonicalizes legacy profile data, prevents stale asynchronous UI refreshes from overwriting newer changes, and explains upstream disconnect and failover outcomes clearly.
- Expands privacy-safe diagnostics with stronger redaction for keys, tokens, authentication data, cookies, passwords, URL credentials, queries, and fragments, plus sanitized app references, structured Windows activation attempts, and clearer launch and relay failure categories.
- Waits for backend confirmation before committing injected setting changes, restores the previous UI state when a save fails or times out, and preserves TOML byte-order marks and line endings when updating applied-mode markers.
- Adds extensive regression coverage for unified Windows app activation, mode round trips, provider synchronization, backups and rollback, concurrent writes, relay settings, diagnostics, and asynchronous UI state.

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
