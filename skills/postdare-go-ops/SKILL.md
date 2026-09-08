---
name: postdare-go-ops
description: 部署、配置和排查 Postdare Go 发布控制台（postdare-go / hellodeveye/postdare-go）——一个不依赖 Docker/K8s 的裸机发布系统。Covers install and systemd setup, config.yaml and POSTDARE_GO_* environment variables, secrets.yaml / JWT / MCP token resolution, SQLite vs MySQL, project deploy_stages pipelines (command / health_check / outbound_webhook, run_when, continue_on_error, capture_as ai_review), Gitee and GitHub webhook signing, deploy and app log paths, and the MCP server's two transports. Use this skill whenever someone asks how to configure, install, deploy, wire up webhooks for, or debug postdare-go — including "部署没触发"、"webhook 不生效"、"初始密码在哪"、"MCP 连不上"、"日志看不到"、"回滚怎么配" — even when they don't name the project explicitly but the surrounding repo is postdare-go.
---

# Postdare Go 运维与配置

Postdare Go 是一台或少量 Linux 服务器上的裸机发布控制台：单个 Go 二进制 + SQLite，部署命令全部来自项目配置，前端不传任何命令字符串。理解这条边界是排查一切问题的起点——**如果一个动作不在项目的 `deploy_stages` 里，它就不会发生**。

需要调接口或写自动化脚本时，用配套的 `postdare-go-api` skill；这份 skill 管的是"怎么装、怎么配、为什么没生效"。

## 先定位在哪一层

排查前先判断问题属于哪一层，能省掉大半弯路：

| 现象 | 大概率的层 | 先看 |
| --- | --- | --- |
| 服务起不来、SQLite 打不开 | 配置 / 权限 | `journalctl -u postdare-go -n 100` |
| 推代码没触发部署 | Webhook 层 | `webhook_events` 表里的 `ignored_reason` |
| 触发了但某阶段失败 | Stage 层 | 部署日志 `{log_dir}/{task_id}.log` |
| 应用日志读不到 | 路径约束 | `app_log_path` 是否在 `app_dir` 内 |
| MCP 客户端连不上 / 不能部署 | Token 与 mutation 开关 | `mcp.enabled`、`mcp.allow_mutation_tools` |

Webhook 被忽略时后端**不会**报错，只会在 `webhook_events` 里留一条 `ignored_reason`。所以"没反应"几乎总是先查这张表，而不是查部署日志。

## 安装

```bash
make release                       # 构建前端、嵌入、产出 bin/postdare-go-linux-{amd64,arm64}
scp bin/postdare-go-linux-amd64 host:/opt/postdare-go/postdare-go
sudo cp examples/postdare-go.service /etc/systemd/system/postdare-go.service
sudo systemctl daemon-reload && sudo systemctl enable --now postdare-go
```

首启会自动创建 `{data_dir}/postdare.db`、`{data_dir}/secrets.yaml`（0600）、`{data_dir}/logs/deploy/`，并生成随机 admin 初始密码。**这个密码只在启动日志里打印一次**：

```bash
journalctl -u postdare-go -n 100 | grep -i password
```

没设置 `POSTDARE_GO_ADMIN_PASSWORD` 时首次登录强制改密——在改密之前，除 `GET /auth/me`、`POST /auth/logout`、`PUT /auth/password` 外的所有接口都返回 `403 PASSWORD_CHANGE_REQUIRED`。自动化脚本第一次接不上，多半就是卡在这里。

开发态：`go run . serve`（默认 `127.0.0.1:8088`，数据落在仓库 `data/`）；前端 `cd web && npm run dev`（:5173，已代理 `/api`）。验证回路是 `go vet ./... && go test ./...`，测试只用临时 SQLite，不需要 MySQL。

## 配置

`config.yaml` 是可选的，解析顺序：`POSTDARE_GO_CONFIG` 指定路径 → 当前目录 `config.yaml` → 二进制同目录 `config.yaml`。完整字段、默认值和环境变量覆盖表见 `references/config.md`。

三件最容易踩的事：

1. **运行时和密钥配置改了必须重启。** `PATCH /api/v1/settings` 只存非运行时元数据，凡是 `server.*`、`database.*`、`jwt.*`、`deploy.*`、`app_log.*`、`mcp.*` 或名字里含 secret/token 的 key 都会被拒绝（`422 SETTING_READ_ONLY`）。
2. **`POSTDARE_GO_DATA_DIR` 只搬迁仍是默认值的派生路径。** 一旦在 `config.yaml` 里显式写了 `database.path` 或 `deploy.log_dir`，显式值优先，`data_dir` 改了它们也不动。
3. **密钥有固定的三级解析顺序**：环境变量 → `config.yaml` → `{data_dir}/secrets.yaml`（自动生成）。`config.yaml` 里留着 `please-change-this-*` 占位符等同于没配，会走 `secrets.yaml`。

## 部署阶段（deploy_stages）

一个项目的流水线就是一个有序的 typed stage 数组，是这个产品的核心概念。写法、字段语义、各类型的 `config` 结构和一套完整示例见 **`references/deploy-stages.md`**——要改流水线时读它。

要记住的模型：

- 主流程跑 `enabled == true && run_when == ""` 的 stage，按数组顺序。
- 某个 stage 失败：`continue_on_error: true` 记为失败但继续；否则整个任务失败。
- 任务到达终态后，再按 `run_when`（`success` / `failed` / `always`）跑延后 stage——告警类的出站 webhook 就靠这个。
- 回滚不走 `deploy_stages`，只跑 `rollback_cmd` 这一条命令，然后同样跑延后 stage。
- 命令为空的 stage 是**跳过**，不是失败。
- 同一项目同一时刻只允许一个 `pending`/`running` 任务，冲突返回 `409 PROJECT_DEPLOY_RUNNING`。

命令用 `bash -lc` 在独立进程组里跑，超时或取消会杀掉整个进程组；默认超时 30 分钟（`deploy.command_timeout_minutes`）。所以命令要写绝对路径，且 postdare-go 的运行用户要有执行权限。

## Webhook

回调地址是公开的，但每个请求都要过 token 或签名校验：

```text
POST /api/v1/webhooks/gitee/{project_key}?token=WEBHOOK_SECRET
POST /api/v1/webhooks/github/{project_key}     # X-Hub-Signature-256: sha256=HMAC_SHA256(secret, raw_body)
```

**推了代码却没部署**：查 `webhook_events.ignored_reason`。服务端按固定顺序判定，第一个命中的就是记录下来的原因——`project not found` → `project git_provider mismatch` → `invalid webhook signature or token` → `unsupported event type` → `auto deploy disabled` → `branch mismatch`。这意味着签名错误会掩盖分支不匹配，得一层层往下修。取值含义详见 `references/webhooks.md`。

## 日志

```text
部署日志：{deploy.log_dir}/{task_id}.log      默认 {data_dir}/logs/deploy/
应用日志：项目配置的 app_log_path
```

应用日志有硬约束：`app_log_path` 必须是绝对路径，且解析软链后仍落在项目 `app_dir` 内，否则读取被拒。这是防路径穿越的，不是 bug——把日志软链到 `app_dir` 下，或者把 `app_dir` 设到能覆盖日志的父目录。默认 tail 500 行，上限 5000 行。SSE 流式接口因为 `EventSource` 不能带 header，额外接受 `?access_token=`。

## MCP

两种传输共用同一套工具实现，工具集完全相同：

- **stdio**：客户端把 `postdare-go mcp` 拉起为子进程，只读 `POSTDARE_GO_BASE_URL` 和 `POSTDARE_GO_API_TOKEN` 两个环境变量，不读 config.yaml 也不读数据库。
- **Streamable HTTP**：`postdare-go serve` 在 `POST /mcp` 暴露（`mcp.enabled` 为 false 时是 404），只认 MCP token，用户 JWT 会被 `403` 拒绝；始终返回 JSON 而非 SSE；不支持 batch、不发通知、无 session（`GET`/`DELETE` 返回 405）。

两条传输都不直连数据库，而是带着 API token 回到自己的 REST handler，所以 agent 能拿到的恰好等于 REST 允许的。**默认不能触发部署**：需要 `mcp.allow_mutation_tools: true` 并且调用参数带 `confirm: true`，两者缺一不可。注意这个闸门对 REST 也生效——用 MCP token 直接 `POST /projects/{id}/deploy-tasks` 同样会被 `403 MCP_MUTATION_DISABLED` 挡下。

## 常见故障速查

完整清单（含 MySQL、报告分享、AI review stage 等）见 `references/troubleshooting.md`。最高频的几条：

- **SQLite 打不开**：`{data_dir}` 不存在或运行用户无写权限。
- **首次调接口全是 403**：`PASSWORD_CHANGE_REQUIRED`，先改密码。
- **部署返回 409**：该项目还有 pending/running 任务；等它结束或取消。
- **重启后任务变 failed**：启动时 `ReconcileInterruptedTasks` 会把残留的 pending/running 标为 failed（"task interrupted by server restart"），这是预期行为。
- **回滚报 422**：项目 `rollback_cmd` 是空的。
- **接口里 secret 是 `abc******xyz`**：响应做了脱敏；PATCH 时带回这个掩码值会被**忽略**而不是写入，所以不改密钥时可以安全地整体回传。
