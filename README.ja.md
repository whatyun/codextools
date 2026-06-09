# CodexTools

[![中文](https://img.shields.io/badge/%F0%9F%87%A8%F0%9F%87%B3-%E4%B8%AD%E6%96%87-0f766e)](./README.md)
[![English](https://img.shields.io/badge/%F0%9F%87%BA%F0%9F%87%B8-English-2563eb)](./README.en.md)
![日本語](https://img.shields.io/badge/%F0%9F%87%AF%F0%9F%87%B5-%E6%97%A5%E6%9C%AC%E8%AA%9E-d97706)
[![한국어](https://img.shields.io/badge/%F0%9F%87%B0%F0%9F%87%B7-%ED%95%9C%EA%B5%AD%EC%96%B4-7c3aed)](./README.ko.md)

CodexTools は、Codex のセットアップ、起動、接続モード、UI 拡張、スクリプト、診断、修復をまとめて扱う独立型の Go + React デスクトップマネージャーです。
タスク中心の管理 UI、Relay と Provider 設定、スクリプト管理、ローカル修復、デスクトップ配布、サポート用診断を、単独でビルドできるリポジトリにまとめています。

## 含まれるもの

- ローカルコマンド、静的 UI 配信、デスクトップ起動体験を担う Go バックエンド。
- 非技術ユーザー向けに整理した React 管理 UI。
- 初回セットアップ、起動状態、接続モード選択、Relay 設定、会話修復、スクリプト、ログ、診断、メンテナンスツール。
- このリポジトリから公開される macOS / Windows デスクトップパッケージ。

## 画面

### ホームダッシュボード

![起動状態、接続モード、UI 拡張モード、修復入口を表示する CodexTools ホームダッシュボード](./docs/assets/screenshot-home.png)

最初の画面でローカル環境の準備状態を確認し、主要な起動ボタン、接続サービス、UI 機能、修復入口、重要な状態をまとめて扱えます。

### 初回セットアップガイド

![システム検出、Codex インストール状態、CCSwitch インポート、モード選択、起動手順を表示する CodexTools 初回ガイド](./docs/assets/screenshot-onboarding.png)

システム確認、Codex インストール検出、CCSwitch Provider のインポート、接続モード選択、起動までを順番に進めます。

## よくある質問

この内容は [BigPizzaV3/CodexPlusPlus](https://github.com/BigPizzaV3/CodexPlusPlus) の FAQ をもとに、CodexTools 向けに整理したものです。

### CodexTools メニューが表示されない

元の Codex 入口ではなく、`Codex++` 入口から起動していることを確認してください。管理ツールの「診断」と「ログ」ページで注入状態を確認することもできます。

### プラグイン内でバックエンドに接続できないと表示される

まずブラウザまたは PowerShell で確認します。

```powershell
Invoke-RestMethod -Method Post -Uri http://127.0.0.1:57321/backend/status -Body "{}" -ContentType "application/json"
```

API が正常でもプラグインがタイムアウトする場合、Codex ページ内の CDP bridge またはスクリプトキャッシュが原因であることが多いです。CodexTools から Codex を再起動するか、管理ツールのログで `renderer.script_loaded`、`bridge.request`、`bridge.response` を確認してください。

### Upstream worktree と Codex 標準の worktree 作成の違い

CodexTools の Upstream worktree は、先にリモートブランチを更新してから次のコマンドを実行する動作に相当します。

```bash
git worktree add -b <new-branch> <worktree-path> upstream/<base-branch>
```

そのため、新しい worktree は現在のセッションのローカル HEAD ではなく、最新のリモート追跡ブランチから開始されます。現在の Codex バージョンの標準 worktree 作成フォームを安全に識別できない場合は、CodexTools メニューからリポジトリパス、ブランチ名、worktree パス、remote、base branch を手動で入力してください。

### macOS で開けない、または壊れていると表示される

パッケージが未署名または未公証の場合、macOS Gatekeeper が `.pkg` インストーラーまたはインストール後の App をブロックし、壊れていると表示することがあります。

![Codex++ 管理ツールが壊れていると表示する macOS 警告](./docs/assets/macos-damaged-warning.png)

次のコマンドで隔離属性を削除してください。

```bash
sudo xattr -rd com.apple.quarantine ~/Downloads/CodexTools-*-macos-*.pkg
sudo xattr -rd com.apple.quarantine "/Applications/Codex++ 管理工具.app"
sudo xattr -rd com.apple.quarantine "/Applications/Codex++.app"
```

インストール時にブロックされる場合は、ダウンロードした `.pkg` に対して最初のコマンドを実行してから再インストールしてください。インストール後の起動時にブロックされる場合は、`/Applications` 内の 2 つの App に対して後半 2 つのコマンドを実行し、`Codex++` または `Codex++ 管理工具` を再度開いてください。

### macOS Intel でも使えますか

使えます。Release では `macos-x64` と `macos-arm64` のパッケージを分けて提供します。Intel Mac は x64、Apple Silicon は arm64 パッケージを使ってください。

## ローカル開発

```bash
npm --prefix web install
npm --prefix web run vite:build
go run .
```

## ビルド

```bash
npm --prefix web run check
npm --prefix web run vite:build
go build -o codextools .
```

## ダウンロード

- ダウンロードページ: [docs/downloads.html](./docs/downloads.html)
- Windows は x64 と arm64 のインストーラーを提供し、両方のポータブル zip も残しています。
- macOS は Apple Silicon (`macos-arm64`) と Intel (`macos-x64`) のパッケージを提供します。

## コミュニティ

- Telegram: `https://t.me/wanai8`
- LINUX DO: <https://linux.do/>

## 由来と謝辞

CodexTools は、初期の Codex++ の取り組みをもとに独立した Go リファクタリングと管理 UI として分離したプロジェクトです。
基礎機能、ワークフローの考え方、ユーザー向けツールの方向性を作った元の Codex++ プロジェクトに感謝します。

- 元プロジェクト: <https://github.com/BigPizzaV3/CodexPlusPlus>
- リファクタリング元: <https://github.com/hereww/CodexPlusPlus>
- 独立プロジェクト: <https://github.com/hereww/codextools>
