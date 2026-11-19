# aidev-clis

## 前言

**给 AI agent 补齐验证一次改动所需的全部权限，让"改代码 → 看结果 → 再改"在真实环境里闭环。**

AI agent 默认只有**代码权限**：能读写代码，能在本地编译、跑测试。本地能回答的问题，都应该在本地回答。但跨服务调用是否打通、数据是否按预期落库、部署后的配置是否生效、线上报错由哪个请求引起，这些只有真实环境才有答案。要拿到答案，agent 还需要另外四种权限：

```mermaid
flowchart LR
  Code["代码权限<br/>读写代码 · 本地编译测试<br/>agent 自带"]
  Release["发布权限<br/>jcli<br/>Jenkins 构建 / 部署"]
  Call["调用权限<br/>httpcli<br/>登录并调用接口"]
  Log["日志权限<br/>logcli<br/>k8s · docker · SSH · 文件 · SLS"]
  DB["数据库权限<br/>dbcli<br/>MySQL · PG · Redis · Mongo …"]
  Verdict{"改对了吗？"}
  Keep["tcli<br/>固化成可重复执行的断言"]

  Code -->|改完| Release --> Call
  Call --> Log --> Verdict
  Call --> DB --> Verdict
  Verdict -->|没有：带着证据回去改| Code
  Verdict -->|对了| Keep
```

五种权限齐了，agent 才能自己走完整个循环；缺任何一环，它都只能停下来等人去部署、去点接口、去翻日志、去查库。`aidev-clis` 把后四种权限各做成一个 CLI，再用 `tcli` 把已经确认的结论固化成断言，用 `aidev` 告诉 agent 当前工作区接通了哪些能力。

每个 CLI 默认输出结构化 JSON，访问后端的调用都会写审计。几个 CLI 互相独立：没有 Jenkins 的团队，发布这一环换成自己的部署脚本或 CI 即可，其余 CLI 照常使用。

凭据放在本机配置目录，由 CLI 在调用时注入，不出现在命令参数和输出里，因此不会进入 AI 的上下文和对话记录。但这不是沙箱：同一系统账号下的进程仍然能直接读取凭据文件。真正的权限边界是后端账号本身的授权范围（只读库账号、只能看日志的 ServiceAccount、只能触发指定 job 的 token 等），详见[安全模型](docs/SECURITY-MODEL.md)。

> 验证链本身的耗时不会消失，但不再需要人盯着，工程师可以并行处理其他工作。

## 系统架构

```mermaid
flowchart LR
  Actor["AI agent / CI / Operator"] --> CLIs

  subgraph CLIs["aidev-clis"]
    direction LR
    aidev["aidev<br/>Discovery / Config backup"]
    jcli["jcli<br/>Jenkins"]
    httpcli["httpcli<br/>HTTP"]
    dbcli["dbcli<br/>DB"]
    logcli["logcli<br/>logs"]
    tcli["tcli<br/>case runner"]
  end

  tcli -.->|delegates| httpcli
  tcli -.->|delegates| dbcli
  tcli -.->|delegates| logcli

  CLIs --> Core["Local control plane<br/>config · credentials · sessions · audit"]
  Core --> Backends["Runtime backends<br/>DB · logs · HTTP apps · Jenkins"]
```

## 六个 CLI

| CLI | 用途 | 典型命令 |
|---|---|---|
| [`aidev`](docs/cli-aidev.md) | 发现当前工作区可用的能力、target 和 httpcli app；也能备份/恢复本机配置。 | `aidev` |
| [`dbcli`](docs/cli-dbcli.md) | 查询数据库；写入必须显式加 `--allow-write`。支持 mysql、postgres、kingbase、redis、sqlite、mongo。 | `dbcli mysql --target uat "SELECT id FROM orders LIMIT 5"` |
| [`logcli`](docs/cli-logcli.md) | 从日志源读取日志。支持 local-file、kubectl、docker、ssh-file、ssh-docker、sls。 | `logcli sls --target prod search "level:ERROR" --from 1h` |
| [`httpcli`](docs/cli-httpcli.md) | 以本机会话/凭据调用内部 HTTP app，支持登录、会话保存、自动重登。 | `httpcli call portal /api/me --actor alice --env pre` |
| [`jcli`](docs/cli-jcli.md) | 触发和观察 Jenkins 构建/部署。 | `jcli build orders-service --target uat --wait` |
| [`tcli`](docs/cli-tcli.md) | 运行 case 编排 API/DB/日志断言，出 PASS/FAIL 裁决：既做一次性发布验收门禁，也做守护跨系统不变量的长期回归。 | `tcli run examples/tcli-case.yaml` |


## 迭代闭环

```mermaid
flowchart LR
  Need["需求 / 问题"] --> Agent["AI agent / Operator"]
  Agent --> Change["代码 / 配置变更"]
  Change --> Local{"可以本地验证？"}

  Local -->|可以| Fast["本地验证<br/>编译 · 单测 · 集成测试"]
  Fast --> Verdict{"验证通过？"}

  Local -->|不可以，走旁路| Deploy["jcli<br/>构建 / 部署"]
  Deploy --> Trigger["httpcli<br/>触发业务动作"]
  Trigger --> Observe["dbcli + logcli<br/>检查状态和轨迹"]
  Observe --> Runtime["tcli<br/>形成运行态结论"]
  Runtime --> Verdict

  Verdict -->|未通过| Agent
  Verdict -->|通过| Done["提交 / 发布确认"]

  Discover["aidev<br/>发现可用能力"] -.-> Agent
```

旁路验证分为两层：

1. 探索层: `httpcli`、`dbcli` 和 `logcli`，负责进入真实系统、收集事实并确认预期关系。
2. 固化层: `tcli`，在关系明确后将其编码为可重复执行的断言和 PASS/FAIL 裁决。

`tcli` 不负责发现答案，而负责把已经明确的答案固化成验证契约。因此图中 `tcli` 所在的裁决环节是可选的：探索阶段由 agent 直接依据证据形成结论，关系固化后才由 `tcli` 接管这一环节。

### 理想闭环

优秀的闭环应该尽可能短、确定且可重复：修改后几秒内得到结果，失败信息能直接指向原因，同一输入始终得到同一结论。本地编译、单元测试和集成测试最接近这个目标，因此永远是首选。

旁路验证是本地无法证明时的补充，而不是终点。在旁路上确认过的每个判断，都应该沉淀到它所能到达的最快路径：

- 单个服务就能表达的问题，沉淀为本地回归测试；
- 只在本次迭代中需要重复裁决的运行态关系，编码为一次性 `tcli` case，随迭代结束丢弃；
- 只有跨系统不变量，才值得作为长期 `tcli` 回归保留。

随着系统演进，验证应不断从慢旁路前移到快反馈路径。

## Quick Start

### 1. 安装

从源码安装（需要 Go）：

```sh
make install
```

安装预编译版本：

```sh
# macOS / Linux
curl -fsSL https://raw.githubusercontent.com/no-today/aidev-clis/main/scripts/get.sh | bash
```

```powershell
# Windows PowerShell（用户级，无需管理员）
irm https://raw.githubusercontent.com/no-today/aidev-clis/main/scripts/get.ps1 | iex
```

两种方式都会安装六个二进制、agent skills 和 shell completion。

### 2. 配置

配置默认放在 `~/.aidev-clis`。从 [`examples/`](examples/) 复制示例，再按上方「六个 CLI」表格
阅读对应 CLI 手册。

### 3. 发现并调用

```sh
aidev                         # 当前工作区能用什么
dbcli targets                 # 已配置的数据库 target
dbcli mysql --target uat "SELECT 1"
```

`aidev` 会读取离当前目录最近的 `.aidev.yaml`，只展示该工作区场景允许使用的能力。<br/>
完整参数以各命令的 `-h` 为准。

## 稳定契约

- [输出契约](docs/OUTPUT-CONTRACT.md)：JSON envelope、NDJSON、diagnostics 和退出码
- [安全模型](docs/SECURITY-MODEL.md)：凭据生命周期、后端授权和信任边界
- [架构](docs/ARCHITECTURE.md)：包边界、调用路径和 CLI / adapter 隔离
- [Adapter 隔离契约](docs/ADAPTER-ISOLATION.md)：adapter 的自包含与注册规则
- [跨平台契约](docs/CROSS-PLATFORM.md)：Linux、macOS 和 Windows 的可移植要求

## License

[MIT](LICENSE)
