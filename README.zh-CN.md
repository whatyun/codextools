# ChatGPT Codex Tools 中文说明

[English](./README.md) · **简体中文** · [日本語](./README.ja.md) · [한국어](./README.ko.md)

ChatGPT Codex Tools 是一个独立的 Go + React 桌面管理器，用来集中处理 ChatGPT 桌面应用内 Codex 的启动、连接模式、界面增强、用户脚本、历史对话修复和诊断流程。

![ChatGPT Codex Tools 首页仪表盘](./docs/assets/screenshot-home.png)

## 项目特点

它不只是一个启动器，而是管理 ChatGPT Codex 本地运行状态的控制中心。切换 Provider 时，工具会同时处理连接配置、模型、会话归属和扩展配置；执行历史修复或 Skill/MCP 恢复前会先检查条件并创建备份，减少手动改配置造成的数据风险。

## 主要功能

- **引导与启动**：检测系统架构、ChatGPT 安装和本地路径；可导入 CCSwitch 中的 Codex Provider；创建专用的 **ChatGPT Codex** 启动入口，并显示运行状态、当前连接和修复入口。
- **三种连接模式**：官方登录保持原生行为；官方混合 API 在保留官方账号、站点和插件能力的同时使用兼容 API；中转 API 可连接代理、聚合服务或自建端点。
- **完整 Provider 配置**：保存多个 Provider，支持排序、连通性测试、协议选择、模型列表、上下文窗口、单 Provider 上游代理，以及多个 API Provider 的轮转聚合。
- **Codex 界面增强**：解锁本地或上游模型、删除会话、带时间戳导出 Markdown、移动项目、显示 Timeline，并可从最新 `upstream/<base-branch>` 创建 Git worktree。
- **MCP / Skills / Plugins 管理**：集中维护 Codex 工具与插件配置；切换 Provider 时合并相关 `config.toml` 配置；可扫描本地缓存并恢复插件、Marketplace 和 `node_repl` MCP 条目。
- **脚本市场**：浏览、安装、更新、启用或停用市场脚本，同时管理本地用户脚本，无需手动修改 ChatGPT 应用文件。
- **历史对话同步**：连接模式变化后更新本地对话的 Provider 归属和索引，让旧会话在新模式下继续可见；操作前自动完整备份，不删除消息正文。
- **精确的历史修复**：针对异常工具调用记录，只移除 payload 顶层不兼容的 `namespace`，保留消息、工具输出和嵌套参数；执行前检查进程状态、候选文件和备份空间。
- **备份与恢复**：创建、标记、查看、恢复和删除 Skill/MCP 快照；恢复前再次备份当前状态，只替换相关 TOML 表，保留无关配置。
- **诊断与维护**：记录启动器、脚本注入、Bridge 请求响应和修复过程；生成包含版本、平台、路径和关键设置的诊断报告，并提供入口、路径、配置和插件状态修复。
- **手机控制入口**：通过本地 Helper 暴露移动端控制入口，不再依赖外部 Relay 房间与密钥。
- **多平台发布**：提供 macOS Apple Silicon、macOS Intel、Windows x64 和 Windows ARM64 安装包及便携版。

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
