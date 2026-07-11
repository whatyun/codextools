## CodexTools 1.1.30

### Changes

- Fixed Windows plugin marketplace refresh so it skips unusable WindowsApps Codex CLI aliases.
- Prefer the real user-local Codex runtime CLI when refreshing plugins on Windows.
- Only write `CODEX_CLI_PATH` into the Node REPL MCP config when a usable Codex CLI is found.
- Hardened repository attributes by marking macOS `.pkg` installers as binary release assets.

### macOS unsigned build notice

The macOS packages are unsigned community builds, including the pkg installers. If macOS blocks the first launch, run:

```bash
xattr -cr "/Applications/Codex++ 管理工具.app"
xattr -cr "/Applications/Codex++.app"
```

You can also right-click each app and choose **Open**.
