## CodexTools 1.1.39

### Changes

- Reworks the Windows Codex launch selection introduced around the 1.1.37/1.1.38 line so protected Store/MSIX package paths are no longer treated as valid Codex++ launch targets.
- Rejects `Program Files\WindowsApps` Codex paths before attempting MSIX activation, avoiding misleading activation failures when the package cannot accept debug-port launch arguments.
- Updates the repair flow and first-run guide to point Windows users at a directly runnable mirror/unpacked `Codex.exe` instead of the protected package directory.
- Shows an explicit unsupported MSIX launch status in the manager UI with clearer localized guidance.
- Adds regression coverage for protected MSIX rejection, fallback launch path selection, and Windows guide copy.

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
