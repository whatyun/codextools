# ChatGPT Codex Tools 中文说明

[English](./README.md) · **简体中文** · [日本語](./README.ja.md) · [한국어](./README.ko.md)

ChatGPT Codex Tools 是一个独立的 Go + React 桌面管理器，用来集中处理 ChatGPT 桌面应用内 Codex 的启动、连接模式、界面增强、用户脚本、历史对话修复和诊断流程。

![ChatGPT Codex Tools 首页仪表盘](./docs/assets/screenshot-home.png)

## 主要功能

- 新手引导：检测系统与 ChatGPT 安装状态，导入 CCSwitch Provider，并完成首次启动配置。
- 连接管理：支持官方登录、官方混合 API 和兼容中转 API，可保存多个 Provider 并测试连通性。
- 界面增强：集中管理会话删除、Markdown 导出、项目移动、Timeline 和用户脚本等功能。
- 修复维护：提供历史对话归属修复、路径与快捷方式修复、插件和配置恢复。
- 日志诊断：记录启动状态与 Bridge 事件，并生成便于反馈问题的诊断报告。
- 多平台发布：提供 macOS Apple Silicon、macOS Intel、Windows x64 和 Windows ARM64 安装包及便携版。

## 下载与使用

从 [GitHub Releases](https://github.com/hereww/codextools/releases/latest) 下载与系统架构匹配的版本，也可以查看[下载说明页](https://hereww.github.io/codextools/downloads.html)。安装官方 [ChatGPT 桌面应用](https://chatgpt.com/download/) 后，打开 **ChatGPT Codex 管理工具** 完成引导，并从它创建的 **ChatGPT Codex** 入口启动 ChatGPT。

> [!WARNING]
> 当前 macOS 安装包未签名、未公证，可能被 Gatekeeper 拦截。处理方法和安全提示请查看英文主页的 [macOS troubleshooting](./README.md#macos-blocks-the-app)。

## 本地构建

```bash
npm --prefix web install
npm --prefix web run check
npm --prefix web run vite:build
go test ./...
go build -o codextools .
```

完整的功能说明、架构、常见问题和社区链接请阅读[英文主页](./README.md)。
