# ChatGPT Codex Tools

[![中文](https://img.shields.io/badge/%F0%9F%87%A8%F0%9F%87%B3-%E4%B8%AD%E6%96%87-0f766e)](./README.md)
[![English](https://img.shields.io/badge/%F0%9F%87%BA%F0%9F%87%B8-English-2563eb)](./README.en.md)
[![日本語](https://img.shields.io/badge/%F0%9F%87%AF%F0%9F%87%B5-%E6%97%A5%E6%9C%AC%E8%AA%9E-d97706)](./README.ja.md)
![한국어](https://img.shields.io/badge/%F0%9F%87%B0%F0%9F%87%B7-%ED%95%9C%EA%B5%AD%EC%96%B4-7c3aed)

ChatGPT Codex Tools는 ChatGPT 데스크톱 앱 안의 Codex 실행, 연결 모드, UI 확장, 스크립트, 진단, 복구 흐름을 한곳에서 관리하는 독립 Go + React 데스크톱 매니저입니다.
작업 중심의 매니저 UI, Relay와 Provider 설정, 스크립트 관리, 로컬 복구, 데스크톱 다운로드, 지원용 진단을 독립적으로 빌드할 수 있는 저장소에 담았습니다.

## 포함 내용

- 로컬 명령 분배, 정적 UI 제공, 데스크톱 실행 경험을 담당하는 Go 백엔드.
- 비기술 사용자를 위해 정리한 React 매니저 UI.
- 첫 실행 설정, 실행 상태, 연결 모드 선택, Relay 설정, 대화 복구, 스크립트, 로그, 진단, 유지보수 도구.
- 이 저장소에서 배포되는 macOS와 Windows 데스크톱 패키지.

## 화면

### 홈 대시보드

![실행 상태, 연결 모드, UI 확장 모드, 복구 진입점을 보여주는 ChatGPT Codex Tools 홈 대시보드](./docs/assets/screenshot-home.png)

첫 화면에서 로컬 환경 준비 상태를 확인하고, 주요 실행 버튼과 연결 서비스, UI 기능, 복구 진입점, 핵심 상태를 한곳에서 볼 수 있습니다.

### 초보자 설치 가이드

![시스템 감지, ChatGPT 설치 상태, CCSwitch 가져오기, 모드 선택, 실행 단계를 보여주는 ChatGPT Codex Tools 초보자 가이드](./docs/assets/screenshot-onboarding.png)

시스템 확인, ChatGPT 설치 검사, CCSwitch Provider 가져오기, 연결 모드 선택, 실행까지 순서대로 진행합니다.

## 자주 묻는 질문

이 내용은 현재 ChatGPT Codex Tools 흐름에 맞게 정리한 것입니다.

### ChatGPT Codex Tools 메뉴가 보이지 않습니다

`ChatGPT Codex` 진입점으로 ChatGPT를 실행했는지 확인하세요. 관리 도구의 진단과 로그 페이지에서 주입 상태도 확인할 수 있습니다.

### 플러그인에서 백엔드에 연결할 수 없다고 표시됩니다

먼저 브라우저나 PowerShell에서 테스트하세요.

```powershell
Invoke-RestMethod -Method Post -Uri http://127.0.0.1:57321/backend/status -Body "{}" -ContentType "application/json"
```

API가 정상인데도 플러그인이 계속 시간 초과를 표시한다면 ChatGPT 페이지 안의 CDP bridge 또는 스크립트 캐시 문제인 경우가 많습니다. ChatGPT Codex에서 ChatGPT를 다시 시작하거나 관리 도구 로그에서 `renderer.script_loaded`, `bridge.request`, `bridge.response`를 확인하세요.

### Upstream worktree와 Codex 기본 worktree 생성은 무엇이 다른가요

ChatGPT Codex Tools의 Upstream worktree는 먼저 원격 브랜치를 업데이트한 뒤 다음 명령을 실행하는 것과 같습니다.

```bash
git worktree add -b <new-branch> <worktree-path> upstream/<base-branch>
```

이렇게 하면 새 worktree가 현재 세션의 로컬 HEAD가 아니라 최신 원격 추적 브랜치에서 시작됩니다. 현재 ChatGPT Codex 버전의 기본 worktree 생성 폼을 안전하게 식별할 수 없다면 ChatGPT Codex Tools 메뉴에서 저장소 경로, 브랜치 이름, worktree 경로, remote, base branch를 직접 입력하세요.

### macOS에서 열 수 없거나 손상되었다고 표시됩니다

패키지가 서명 또는 공증되지 않은 경우 macOS Gatekeeper가 `.pkg` 설치 파일이나 설치된 App을 차단하며 손상되었다고 표시할 수 있습니다.

![ChatGPT Codex 관리 도구가 손상되었다고 표시하는 macOS 경고](./docs/assets/macos-damaged-warning.png)

터미널에서 아래 명령으로 격리 속성을 제거하세요.

```bash
sudo xattr -rd com.apple.quarantine ~/Downloads/ChatGPT-Codex-Tools-*-macos-*.pkg
sudo xattr -rd com.apple.quarantine "/Applications/ChatGPT Codex 管理工具.app"
sudo xattr -rd com.apple.quarantine "/Applications/ChatGPT Codex.app"
```

설치 단계에서 차단되면 다운로드한 `.pkg`에 첫 번째 명령을 실행한 뒤 다시 설치하세요. 설치 후 실행 단계에서 차단되면 `/Applications`의 두 App에 나머지 두 명령을 실행한 뒤 `ChatGPT Codex` 또는 `ChatGPT Codex 管理工具`를 다시 열면 됩니다.

### macOS Intel에서도 사용할 수 있나요

사용할 수 있습니다. Release는 `macos-x64`와 `macos-arm64` 패키지를 따로 제공합니다. Intel Mac은 x64 패키지를, Apple Silicon은 arm64 패키지를 사용하세요.

## 로컬 개발

```bash
npm --prefix web install
npm --prefix web run vite:build
go run .
```

## 빌드

```bash
npm --prefix web run check
npm --prefix web run vite:build
go build -o codextools .
```

## 다운로드

- 다운로드 페이지: [docs/downloads.html](./docs/downloads.html)
- Windows는 x64와 arm64 설치 프로그램을 제공하며 두 아키텍처의 포터블 zip도 유지합니다.
- macOS는 Apple Silicon(`macos-arm64`)과 Intel(`macos-x64`) 패키지를 제공합니다.

## 커뮤니티

- Telegram: `https://t.me/wanai8`
- LINUX DO: <https://linux.do/>

## 출처와 감사

ChatGPT Codex Tools는 Codex가 내장된 현재 ChatGPT 데스크톱 앱을 위한 독립 Go 리팩터링 및 매니저 UI 프로젝트입니다.
기반 기능, 워크플로 아이디어, 사용자 도구 방향을 제공한 초기 커뮤니티 작업에 감사드립니다.

- ChatGPT 다운로드: <https://chatgpt.com/download/>
- Codex 문서: <https://developers.openai.com/codex/>
- 독립 프로젝트: <https://github.com/hereww/codextools>
