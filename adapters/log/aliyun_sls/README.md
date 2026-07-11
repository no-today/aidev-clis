# logcli `sls` adapter

| 字段 | 值 |
|---|---|
| 分类 | 通用型 |
| 状态 | 可用 |
| 配置入口 | `~/.aidev-clis/logcli.yaml` env block |
| 注册名 | `sls` |
| 运行形态 | 内置 adapter，编译进 `logcli` |

## 用途和边界

`sls` 是通用 Aliyun SLS adapter，直接调用官方 GetLogs OpenAPI，使用静态 AccessKey（AK/SK）签名（Signature v1）认证。不依赖公司 gateway、浏览器 cookie 或 federation 网关，也不需要自动刷新（AK/SK 不过期）。

安全边界是 AK 上挂的 RAM 策略本身——给 AI 用的 AK 应当**最小权限**（只授 `log:GetLogStoreLogs`，所有动词含 `doctor` 都走这一个权限）。adapter 不重新实现授权。

## 配置

最小配置：

```yaml
targets:
  app_prod:
    adapter: sls
    project: app-prod-project
    logstore: app-service-log
    endpoint: cn-hangzhou.log.aliyuncs.com
```

完整配置：

```yaml
targets:
  app_prod:
    adapter: sls
    description: 生产 app-service 日志    # logcli targets 展示，说明这个目标是干什么的
    project: app-prod-project
    logstore: app-service-log
    endpoint: cn-hangzhou.log.aliyuncs.com
    credential: sls.ak
    trace_field: traceId
```

字段表：

| 字段 | 必填 | 默认 | 说明 |
|---|---|---|---|
| `adapter` | yes | - | 固定 `sls` |
| `description` | no | - | 该 target 的用途说明；`logcli targets` 会列出，便于 AI/人选对目标 |
| `project` | yes | - | Aliyun SLS project name |
| `logstore` | yes | - | SLS logstore name |
| `endpoint` | yes | - | region 裸域名，如 `cn-hangzhou.log.aliyuncs.com`；**不能填 console URL** |
| `credential` | no | `sls.ak` | AK 凭据文件名，位于 `~/.aidev-clis/credentials/<credential>` |
| `trace_field` | no | `traceId` | `trace` 动词使用的日志字段名 |

`endpoint` 必须是裸域名（`<region>.log.aliyuncs.com`），不带 `https://` 前缀，也不能填 SLS console URL。

## 凭据

凭据文件路径：`~/.aidev-clis/credentials/<credential>`，默认 `~/.aidev-clis/credentials/sls.ak`（权限须 0600）。

文件内容为 JSON：

```json
{
  "access_key_id": "LTAI5tXXXXXXXXXX",
  "access_key_secret": "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
  "security_token": ""
}
```

- `security_token` 为空字符串或省略表示长期 AK（RAM User）；填写则为 STS 临时凭据。
- 凭据内容由 CLI 注入签名，不进入 JSON 输出或审计。

所需 RAM 权限：`log:GetLogStoreLogs`（最小权限，所有动词含 `doctor` 共用）。

## 动词

动词的主参数是位置参数（动词已点明维度，不再用 `--query`/`--trace-id` 重复）：

| 动词 | 形态 | 主参数 | 修饰 flag |
|---|---|---|---|
| `search` | batch | `<query>`（缺省 `*`） | `--from --to --size --reverse` |
| `trace` | batch | `<trace-id>`（必填） | `--from --to --size`（按 `trace_field` 精确匹配） |
| `tail` | stream | `<query>`（缺省 `*`） | `--interval`，滑动窗口轮询，输出 NDJSON |
| `doctor` | batch | — | 核对当前 env 配置：分阶段 checks（`config` → `connect`），对配置的 `logstore` 发最小 GetLogs 探针；健康度看退出码 |

示例：

```
logcli sls --target app_prod search "level: ERROR" --from 1h --to now --size 100
logcli sls --target app_prod --from 1h --to now search "level: ERROR" --size 100
logcli sls --target app_prod trace  abc123 --from 24h --to now
logcli sls --target app_prod tail   "*" --interval 5s
logcli sls --target app_prod doctor                      # 核对配置 + 连通性
```

SLS 字段查询冒号两侧的空格可有可无——`level: ERROR` 和 `level:ERROR` 效果相同。
关键在于该字段必须建了 key-value 索引：查一个未建索引的字段会直接返回
`ParameterInvalid` 错误，而不是返回错误的行。值里含空格或特殊字符要加引号：
`msg: "connection failed"`。

`search` / `trace` 的时间范围由 `--from` / `--to` 控制，这些 SLS 修饰 flag
可以放在动词后，也可以放在动词前。取值支持 `now`、相对时间（`30s`、`5m`、
`2h`、`1d`）、Unix 秒级时间戳或 RFC3339。

`doctor` 的输出与 jcli/dbcli 一致——`data` 是分阶段 checks 的扁平数组，整体健康度看退出码（无顶层 `ok`）。失败阶段也会输出，`ok:false` 且 `detail` 为 `CODE: message`：

```json
{"data":[
  {"name":"config","ok":true,"detail":"project app-prod-project / logstore app-service-log @ cn-hangzhou.log.aliyuncs.com"},
  {"name":"connect","ok":true,"detail":"GetLogs ok"}
]}
```

## 错误码

| 错误码 | 含义 | 常见原因 |
|---|---|---|
| `SLS_PROJECT_MISSING` | 缺必填字段 | 配置块缺 `project` |
| `SLS_LOGSTORE_MISSING` | 缺必填字段 | 配置块缺 `logstore` |
| `SLS_ENDPOINT_MISSING` | 未配置 endpoint | 配置块缺少 `endpoint` 字段 |
| `SLS_ENDPOINT_INVALID` | endpoint 格式不合法 | 误填了 console URL 或带 scheme 前缀 |
| `SLS_AK_MISSING` / `SLS_SK_MISSING` | 凭据缺 AK/SK | 凭据 JSON 格式错误或字段缺失 |
| `SLS_AUTH_FAILED` | 鉴权失败 | AK/SK 错误，或 RAM 策略未授予 GetLogStoreLogs |
| `SLS_API_ERROR` | SLS API 返回错误 | project/logstore 不存在、endpoint 填错 region、网络不通 |
| `SLS_BAD_JSON` | 响应解析失败 | SLS 返回非预期格式；可开 `-v` 查看原始响应 |
| `TRACE_ID_REQUIRED` | trace 缺 trace_id | `trace` 动词未传位置参数 trace-id |

## 代码入口

| 关注点 | 文件 |
|---|---|
| 边界（Run / 动词 / flag 分发） | `aliyun_sls.go` |
| 配置解析 | `config.go` |
| AK 凭据加载 | `creds.go` |
| 签名（Signature v1） | `sign.go` |
| SLS API 请求 / 错误分类 | `client.go` |
| search 分页 / trace / doctor 探针 | `search.go` / `trace.go` |
| tail 滑动窗口 | `tail.go` |
| 时间解析 | `timeparse.go` |
