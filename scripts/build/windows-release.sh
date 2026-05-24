#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DIST="$ROOT/dist/releases"
BUILD="$ROOT/dist/build/windows"
VERSION="${VERSION:-1.1.6}"
TARGET="${TARGET:-windows}"
ZIP_PATH="$DIST/CodexTools-${VERSION}-windows-x64.zip"

rm -rf "$BUILD"
mkdir -p "$BUILD" "$DIST"

pushd "$ROOT/web" >/dev/null
npm install
npm run check
npm run vite:build
popd >/dev/null

pushd "$ROOT" >/dev/null
GOOS=windows GOARCH=amd64 go build -ldflags "-X main.binaryRole=manager" -o "$BUILD/codextools.exe" .
GOOS=windows GOARCH=amd64 go build -ldflags "-X main.binaryRole=launcher" -o "$BUILD/codextools-launcher.exe" .
popd >/dev/null

cp "$ROOT/assets/icons/codextools-1024.png" "$BUILD/codextools-icon.png"
cp "$ROOT/README.md" "$BUILD/README.md"
cp "$ROOT/README.zh-CN.md" "$BUILD/README.zh-CN.md"

cat > "$BUILD/START-HERE.txt" <<TXT
CodexTools for Windows

1. Double-click codextools.exe to open the manager.
2. Set your Codex installation path in the app if it is not detected automatically.
3. The package also includes codextools-launcher.exe for direct launch workflows used by the manager.
TXT

rm -f "$ZIP_PATH"
ditto -c -k --sequesterRsrc --keepParent "$BUILD" "$ZIP_PATH"
echo "$ZIP_PATH"
