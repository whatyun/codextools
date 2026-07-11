## CodexTools 1.1.31

### Changes

- Split macOS and Windows install and repair guidance so each platform exposes the right Codex app selection and recovery actions.
- Added multilingual README pages in English, Japanese, and Korean, plus a clearer Chinese landing README.
- Added a macOS Gatekeeper warning screenshot and expanded FAQ guidance for unsigned pkg installers and installed app bundles.
- Added dashboard and analytics workflow updates, including image overlay controls, provider presets, onboarding completion state, and richer update metadata.
- Expanded Zed Remote support with remembered remote projects, configurable open strategies, project registry controls, and optional Zed settings sync.
- Hardened Computer Use setup with guard configuration for bundled browser/chrome plugins, js_repl, notify hooks, and Codex/CodexBeta package discovery.
- Added macOS package payload verification so release builds fail if the pkg root does not contain both app bundles under `/Applications`.

### macOS unsigned build notice

The macOS packages are unsigned community builds, including the pkg installers. If macOS blocks the first launch, run:

```bash
xattr -cr "/Applications/Codex++ 管理工具.app"
xattr -cr "/Applications/Codex++.app"
```

You can also right-click each app and choose **Open**.
