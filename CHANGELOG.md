# Changelog

本文件记录 aidev-clis 的所有显著变更。
格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，版本号遵循 [Semantic Versioning](https://semver.org/lang/zh-CN/)。

## [Unreleased]

## [0.1.0] - 2026-07-12

首个版本：六个原子能力 CLI，为无法建立本地验证闭环的老系统补上一条 AI 可执行的旁路验证链。

### 六个 CLI

- `aidev`：工作区能力发现，读取最近的 `.aidev.yaml`，按场景展示可用的 target 和 app；支持本机配置备份/恢复。
- `dbcli`：数据库查询，写入必须显式 `--allow-write`；适配 mysql、postgres、kingbase、redis、sqlite、mongo，以及 dataease 只读 HTTP 旁路。
- `logcli`：日志读取——有界切片、tail/follow、搜索、跨服务 trace；适配 local-file、kubectl、docker、ssh-file、ssh-docker、aliyun-sls。
- `apicli`：以本机会话/凭据调用内部 HTTP app；支持多步登录 flow、跨步捕获、会话保存与自动重登、actor 体系。
- `jcli`：触发和观察 Jenkins 构建/部署；服务为中心的 job 发现、参数解析与可配置 deploy flow。
- `tcli`：以 YAML case 编排 apicli/dbcli/logcli，输出 conclusion-first 的 PASS/FAIL 裁决和 CI 退出码；既做一次性发布验收门禁，也做跨系统不变量的长期回归。

### 契约与基础设施

- AI-first 输出契约：所有命令默认 JSON envelope，`--output raw` 为可选的人类可读形式；NDJSON 流、diagnostics 和统一退出码。
- 安全模型：凭据只存本机、对 agent 不可见；写操作守卫、二级 allowlist 和本地审计日志。
- Adapter 隔离：db/log 后端自包含，新增注册一行、删除移除一个目录；`make check` 内置隔离守卫。
- 跨平台：Linux / macOS / Windows；bash 与 PowerShell 安装脚本对等；goreleaser 发布六平台预编译包。
- 随安装分发 agent skills 和 shell completion。
