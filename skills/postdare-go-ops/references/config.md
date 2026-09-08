# 配置参考

## 解析顺序

配置来源按优先级从高到低：**环境变量 → config.yaml → `{data_dir}/secrets.yaml`（仅密钥）→ 内置默认值**。

`config.yaml` 本身是可选的，查找顺序：

1. `POSTDARE_GO_CONFIG` 指定的路径（读不到直接报错退出）
2. 当前工作目录下的 `config.yaml`
3. 二进制所在目录下的 `config.yaml`

systemd 环境下第 2 条取决于 `WorkingDirectory`，所以生产上要么把 config 放二进制旁边，要么显式设 `POSTDARE_GO_CONFIG`——"改了配置没生效"多半是读的另一个文件。

## 完整字段

```yaml
data_dir: "/data/postdare-go"          # 默认 /data/postdare-go

server:
  port: 8088
  public_url: https://go.example.com   # 报告分享链接的外部可达地址
  cors_origins:                        # 同时也是 /mcp 的 Origin 白名单
    - http://localhost:5173

database:
  driver: sqlite                       # sqlite | mysql
  path: ""                             # 空 → {data_dir}/postdare.db
  dsn: ""                              # driver=mysql 时必填

jwt:
  secret: "please-change-this-secret"  # 占位符 → 走 secrets.yaml 自动生成
  expire_hours: 72

deploy:
  log_dir: ""                          # 空 → {data_dir}/logs/deploy
  command_timeout_minutes: 30

app_log:
  max_tail_lines: 500                  # 不传 lines 时的默认值
  max_allowed_lines: 5000              # lines 的硬上限

mcp:
  enabled: true                        # false → /mcp 返回 404，且 MCP token 不再被接受
  allow_mutation_tools: false
  api_token: "please-change-this-token"
```

注意 `mcp.enabled` 的**默认值是 false**（Go 零值）。仓库根目录的 `config.yaml` 里写了 `true`，所以不带配置文件启动时 MCP 是关的——包括用 MCP token 调普通 `/api/v1` 路由也会 401。这是"token 明明没写错却认证失败"的最常见原因。

## 环境变量

| 变量 | 覆盖 |
| --- | --- |
| `POSTDARE_GO_CONFIG` | config.yaml 路径 |
| `POSTDARE_GO_PORT` | `server.port` |
| `POSTDARE_GO_DATA_DIR` | `data_dir` |
| `POSTDARE_GO_DB_DRIVER` | `database.driver` |
| `POSTDARE_GO_DB_PATH` | `database.path` |
| `POSTDARE_GO_DB_DSN` | `database.dsn` |
| `POSTDARE_GO_JWT_SECRET` | `jwt.secret`（最高优先级） |
| `POSTDARE_GO_MCP_API_TOKEN` | `mcp.api_token`（最高优先级） |
| `POSTDARE_GO_ADMIN_PASSWORD` | 首启创建 admin 的密码，且不触发强制改密 |

`mcp` 子命令（stdio 传输）是个例外：它既不读 config.yaml 也不连数据库，只认 `POSTDARE_GO_BASE_URL` 和 `POSTDARE_GO_API_TOKEN` 两个变量。

### data_dir 的迁移语义

设置 `POSTDARE_GO_DATA_DIR` 时，只有**仍是内置默认值**的派生路径会跟着搬：

- `database.path` 仅在等于 `/data/postdare-go/postdare.db` 时改写
- `deploy.log_dir` 仅在等于 `/data/postdare-go/logs/deploy` 时改写

一旦在 config.yaml 里显式写了这两个值，它们就锚定住了。这是刻意的——显式配置不该被一个环境变量悄悄搬走——但会让人以为 `data_dir` 没生效。

## 密钥

`jwt.secret` 和 `mcp.api_token` 的解析：

1. 对应环境变量非空 → 用它，且**不再**读写 secrets.yaml
2. 否则 config.yaml 里的值不是 `please-change-this-*` 占位符 → 用它
3. 否则读 `{data_dir}/secrets.yaml`；里面也没有就随机生成 32 字节 hex 并写回（权限 0600）

所以生产上有两种干净的做法：全部走环境变量（配置文件里不出现密钥），或者全部交给 secrets.yaml 自动生成并把该文件纳入备份。混着用最容易搞不清当前生效的是哪个——`GET /api/v1/settings` 会返回脱敏后的 `mcp.api_token`，前 3 位和后 3 位足以比对。

**轮换 `jwt.secret` 的副作用**：所有已签发的 JWT 立即失效（正常），并且所有报告分享链接失效——分享 token 由 salt + jwt.secret 派生，旧 salt 在新 secret 下算不出原来的 token，需要重新 share 才会重新生成。

## 数据库

默认 SQLite（纯 Go 驱动，无需 CGO）。切 MySQL：

```yaml
database:
  driver: mysql
  dsn: "user:pass@tcp(127.0.0.1:3306)/postdare_go?charset=utf8mb4&parseTime=True&loc=Local"
```

只需要预先建好空库，表结构由启动时的 GORM AutoMigrate 创建。DSN 里 `parseTime=True` 不能省，否则时间字段扫描会失败。

启动时还会做两件事，值得知道：首启 seed 默认 admin；每次启动 `ReconcileInterruptedTasks` 把残留的 `pending`/`running` 任务标记为 `failed`，原因写 "task interrupted by server restart"——进程被杀时正在跑的部署命令已经没人管了，把任务留在 running 会永远占着该项目的并发位。

## systemd

示例单元在 `examples/postdare-go.service`（后端）和 `examples/my-app.service`（业务应用）。要点：

- 运行用户需要对 `{data_dir}`、`deploy.log_dir`、各项目 `app_dir` 和 `app_log_path` 有相应读写权限
- 部署命令用 `bash -lc` 执行，会加载登录 shell 的环境；构建工具（mvn、node、go）的 PATH 依赖这一点
- 命令跑在独立进程组里（`Setpgid`），超时或取消会杀掉整组，所以子进程不会变孤儿
