# Changelog

本文件记录 aidev-clis 的所有显著变更。
格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，版本号遵循 [Semantic Versioning](https://semver.org/lang/zh-CN/)。

## [Unreleased]

### 变更

- `dbcli` / `logcli` 收到 `--env` 时的错误码改为 `ENV_FLAG_UNSUPPORTED`（原 `LEGACY_FLAG`），提示改用 `--target`。仍然拒绝执行：`--env` 是 httpcli 的参数，容易误用，放行会让查询悄悄落到默认 target。
- `examples/tcli-case.yaml`：创建订单后的断言改为订单数大于基线（原来写成等于，示例必然失败）。

### 修复

- `httpcli call --output-file`：`body_bytes`、`content_length`、`stream_complete` 在下载时总是输出（含 0 / false），空导出与复制失败可以区分；内联响应不再带这些字段。
- 文档：输出契约补全"非零退出仍返回 `data`"的情形（httpcli 业务失败、tcli 裁决）；安全模型写明 tcli 自身不写审计、由其调起的子 CLI 各自审计。

## [0.1.0] - 2026-11-19

首个开源版本：六个原子能力 CLI，在代码权限之外，为 AI agent 补齐发布、调用、日志、数据库四种权限，让验证改动在真实环境里闭环。

### 六个 CLI

- `aidev`：工作区能力发现，读取最近的 `.aidev.yaml`，按场景展示可用的 target 和 app；支持本机配置备份/恢复。
- `dbcli`：数据库查询，写入必须显式 `--allow-write`；适配 mysql、postgres、kingbase、redis、sqlite、mongo。
- `logcli`：日志读取——有界切片、tail/follow、搜索、跨服务 trace；适配 local-file、kubectl、docker、ssh-file、ssh-docker、sls。
- `httpcli`：以本机会话/凭据调用 HTTP app；支持多步登录 flow、跨步捕获、会话保存与自动重登、actor 体系、二进制安全的上传下载。
- `jcli`：触发和观察 Jenkins 构建/部署；以服务为中心的 job 发现、参数解析与可配置 deploy flow。
- `tcli`：以 YAML case 编排 httpcli/dbcli/logcli，输出 conclusion-first 的 PASS/FAIL 裁决和 CI 退出码；既做一次性发布验收门禁，也做跨系统不变量的长期回归。

### 契约与基础设施

- AI-first 输出契约：所有命令默认 JSON envelope，`--output raw` 为可选的人类可读形式；NDJSON 流、diagnostics 和统一退出码。
- 安全模型：凭据只存本机，由 CLI 注入，不进入 agent 的上下文和输出（不是沙箱，权限边界在后端账号）；写操作守卫、二级 allowlist 和本地审计日志。
- Adapter 隔离：db/log 后端自包含，新增注册一行、删除移除一个目录；`make check` 内置隔离守卫。
- 跨平台：Linux / macOS / Windows；bash 与 PowerShell 安装脚本对等；goreleaser 发布六平台预编译包。
- 随安装分发 agent skills（含 Agent Skills 标准目录 `~/.agents/skills`）和 shell completion。
