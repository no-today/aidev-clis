# dbcli `dataease` adapter

| 字段 | 值 |
|---|---|
| 分类 | 旁路型（**最后手段**） |
| 注册名 | `dataease` |
| 运行形态 | 内置 driver，编译进 `dbcli`（`cmd/dbcli/register.go`） |
| 传输 | 纯 `net/http`，跨平台，不依赖 curl |

## 用途和边界

`dataease` 是数据库的**只读 HTTP 旁路**：当本机无法直连数据库、但能访问 DataEase Web 服务时，把 dbcli 的只读 SQL 转成 DataEase `sqlPreview` 请求。

**这是迫不得已的手段，不是常规 db driver**——只要网络允许直连，就用 `mysql`/`postgres`/… 等真实 driver。dataease 不走 `internal/dbcli/sqlcore`（无共享 SQL 解析、无 auto-LIMIT），不提供 `databases`/`tables`/`describe`，没有写入路径（`--allow-write` 无意义），也不建 DB SSH tunnel。

## 配置

`~/.aidev-clis/dbcli.yaml` 的 target block：

```yaml
targets:
  de:
    adapter: dataease
    base_url: https://dataease.example.com                 # 必填，末尾 / 去掉
    data_source_id: 8d176702-2684-4371-93c4-bee7bc1e13f2   # 必填
    session: dataease.de.session                           # 可选，默认 dataease.<target>.session
    login_credential: dataease.de.login                    # 可选，配了才自动登录
    timeout_seconds: 30                                     # 可选，默认 30，正整数
```

| 字段 | 必填 | 默认 | 说明 |
|---|---|---|---|
| `adapter` | yes | - | 固定 `dataease` |
| `base_url` | yes | - | DataEase base URL；末尾 `/` 去掉 |
| `data_source_id` | yes | - | DataEase datasource id |
| `session` | no | `dataease.<target>.session` | session 文件名，位于 `~/.aidev-clis/sessions/<session>` |
| `login_credential` | no | 空 | 自动登录凭据文件名，位于 `~/.aidev-clis/credentials/<login_credential>` |
| `timeout_seconds` | no | `30` | HTTP timeout，正整数 |

> 已移除旧仓库的 `use_curl`：为跨平台只用 `net/http`，不再以 curl 作为传输或 WAF 兜底。

## 凭据和会话

- session 位于 `~/.aidev-clis/sessions/<session>`：JSON，含 DataEase JWT、`base_url`、`captured_at`，原子写、权限 `0600`。session 绑 `base_url`，避免把一个实例的 token 错用到另一个实例。
- `login_credential` 位于 `~/.aidev-clis/credentials/<login_credential>`（`0600`），内容是 DataEase 登录 JSON：

```json
{"username":"<pre-encrypted>","password":"<pre-encrypted>","loginType":0}
```

- session 缺失/损坏/无 token/认证过期，且配置了 `login_credential` 时，自动登录一次、保存 session 并重试原请求（仅一次）。

## 能力

- `dbcli dataease --target <name> "<read-only SQL>"`（SQL 原样交给 `sqlPreview`，无 auto-LIMIT，自行加 `LIMIT`）
- `dbcli dataease --target <name> doctor`（auth + 连通性探针：auto-login + `select 1` 取一行）
- `dbcli dataease --target <name> insert [--table <name>] [--exclude a,b] "<SELECT ...>"`（把结果渲染成 INSERT 语句）

### insert 的类型局限

DataEase 返回的值**全是字符串/null，没有类型信息**。所以 insert：
- 一律 **MySQL flavor**（反引号标识符、`'`/`\` 转义），非空值**统一按字符串加引号**，`null→NULL`。
- MySQL 插入数值/日期列会自动转换字符串字面量，这些 DataEase 后端都是 med2/MySQL，没问题；**目标若是严格模式或非 MySQL，需自行去引号/调整**。
- 表名：`--table` 优先；否则从 `FROM` 推断单表，遇到 JOIN/子查询报 `INSERT_NO_TABLE`，需显式 `--table`。
- `--exclude a,b,c`：丢掉指定列（逗号分隔、大小写不敏感、可重复）；常用于排除自增主键、`*_time` 等。全部列被排除报 `INSERT_NO_COLUMNS`。
- SQL 必须是 `SELECT`/`WITH`，否则 `WRITE_NOT_ALLOWED`。

## 限制

不支持 exec/写入、`databases`/`tables`/`describe`（显式报 `DATAEASE_UNSUPPORTED_VERB`）、DB SSH tunnel、流式输出。`insert` 是只读渲染（把 SELECT 结果变成 INSERT 文本，不写库）。tab 补全只提示 `doctor` / `insert`。

## WAF（gyfe）

DataEase 后台有 WAF。过 WAF 靠请求里的**浏览器伪装头**（`User-Agent` / `Sec-Fetch-*` / `sec-ch-ua` / `Origin` / `Referer`），与传输无关。若仍被拦截，返回清晰报错 `DATAEASE_WAF_BLOCKED`（不自动绕过）。

## 主要错误码

| 错误码 | 含义 |
|---|---|
| `DATAEASE_BASE_URL_MISSING` / `DATAEASE_DATA_SOURCE_ID_MISSING` | 配置缺必填字段 |
| `DATAEASE_AUTH_EXPIRED` | session 失效（配了 credential 会自动重登重试一次） |
| `DATAEASE_WAF_BLOCKED` | 被后台 WAF 拦截（不可自动重试） |
| `DATAEASE_QUERY_FAILED` | DataEase 拒绝该 SQL |
| `DATAEASE_UNSUPPORTED_VERB` | 用了 `databases`/`tables`/`describe`（dataease 不支持） |
| `DATAEASE_SESSION_*` | session 缺失/无 token/JSON 损坏/base_url 或 key 不匹配 |

## 代码入口

| 关注点 | 文件 |
|---|---|
| 注册 / verb 分发 / auto-login 编排 | `dataease.go` |
| 配置解析 | `config.go` |
| DataEase HTTP client | `client.go` |
| session 持久化 | `session.go` |
| 响应投影 | `response.go` |
