# 故障排查

## 服务起不来

**SQLite 文件打不开** —— `{data_dir}` 不存在，或运行 postdare-go 的用户没有写权限。默认 `/data/postdare-go`。

```bash
sudo mkdir -p /data/postdare-go && sudo chown postdare:postdare /data/postdare-go
```

**MySQL 连不上** —— 确认 `database.driver: mysql`、DSN 正确、目标库已建（表由 AutoMigrate 创建，库不会自动建）。DSN 里 `parseTime=True` 不能省。

**改了 config.yaml 没生效** —— 大概率读的是另一个文件。解析顺序是 `POSTDARE_GO_CONFIG` → 当前工作目录 → 二进制同目录；systemd 下"当前工作目录"取决于 `WorkingDirectory`。启动日志里那行 `startup configuration` 会打印实际生效的 `db_path`、`deploy_log_dir`、`port`、`mcp_enabled`，拿它对账最快。

## 认证

**初始密码找不到** —— 只在首启日志里打印一次：

```bash
journalctl -u postdare-go -n 200 | grep -i password
```

日志已经轮转掉的话，最省事的办法是设 `POSTDARE_GO_ADMIN_PASSWORD` 重启（该变量存在时会用它创建 admin，且不触发强制改密）；如果 admin 已经存在，则需要直接改数据库里的 bcrypt hash。

**所有接口返回 403 PASSWORD_CHANGE_REQUIRED** —— 账号还挂着生成的初始密码。除 `GET /auth/me`、`POST /auth/logout`、`PUT /auth/password` 外全部被拦。先改密码。

**MCP token 明明是对的却 401** —— `mcp.enabled` 为 false 时，中间件根本不比对 MCP token，直接按 JWT 解析然后失败。注意这个字段的默认值是 false，不带 config.yaml 启动时是关的。

**JWT 突然全部失效** —— `jwt.secret` 变了。可能是从环境变量切回了 config.yaml，或者 `data_dir` 换了导致读到另一份 `secrets.yaml`。顺带一提，这也会让所有报告分享链接失效。

## 部署

**同一项目无法再次部署（409）** —— 存在 `pending`/`running` 任务。等它结束，或取消：

```bash
python3 scripts/postdare.py tasks cancel <task_id> --yes
```

**重启后任务全变 failed** —— 预期行为。启动时 `ReconcileInterruptedTasks` 把残留的 pending/running 标为 failed（"task interrupted by server restart"）：进程被杀时那些部署命令已经无人接管，留在 running 会永久占住项目的并发位。

**回滚返回 422 ROLLBACK_CMD_REQUIRED** —— 项目 `rollback_cmd` 为空。

**某阶段"没跑"** —— 三种可能：`enabled: false`；命令为空（记为 `skipped` 而非失败）；`run_when` 非空所以它是延后阶段，只在任务到终态之后按策略执行。

**命令在服务器上手跑没问题，在 postdare 里失败** —— 命令走 `bash -lc`，环境来自登录 shell 而不是你的交互式 shell。PATH 差异（nvm、sdkman、pyenv）是最常见原因。用绝对路径，或在命令里显式 source 需要的环境。

**部署卡住不动** —— 默认 30 分钟超时（`deploy.command_timeout_minutes`）。超时会杀掉整个进程组。交互式等待输入的命令（比如要密码的 git、需要确认的包管理器）会一直挂到超时。

## 日志

**应用日志读不到** —— `app_log_path` 必须是绝对路径，且解析软链后仍落在项目 `app_dir` 内，运行用户要有读权限。这是防路径穿越的硬约束，不可绕过。要么把日志软链进 `app_dir`，要么把 `app_dir` 设成能覆盖日志的父目录。

**部署日志 404** —— 日志文件必须在 `deploy.log_dir` 之下。任务记录里的 `log_file` 是绝对路径；如果 `log_dir` 在任务创建之后改过，旧任务的日志就落在白名单外了。

**日志里有乱码** —— 服务端已经剥掉 ANSI 转义并替换了非法 UTF-8。仍然是乱码说明源文件本身不是 UTF-8。

## MCP

**客户端连不上 `/mcp`** —— 依次确认：`mcp.enabled: true`（否则 404）；用的是 MCP token 而不是用户 JWT（JWT 会 403）；`Accept` 头允许 `application/json`；如果客户端发了 `Origin`，它必须命中 `server.cors_origins` 或 `server.public_url`（防 DNS rebinding，非浏览器客户端不发 Origin，不受影响）。

**`GET /mcp` 或 `DELETE /mcp` 返回 405** —— 正常。这个实现不发服务端通知也不维护 session，只支持 POST。

**MCP 触发不了部署** —— 需要 `mcp.allow_mutation_tools: true`（改完要重启）**并且**工具参数带 `confirm: true`，两者缺一不可。这个闸门对 REST 同样生效：MCP token 直接 POST deploy-tasks 也会被挡。

**批量请求被拒（-32600）** —— JSON-RPC batching 在 2025-06-18 修订里被移除了，这里不支持。

## 报告与分享

**分享链接失效** —— 三种原因：被 `DELETE /reports/{id}/share` 撤销；重新 `POST .../share` 轮换了 token（旧链接立刻失效）；`jwt.secret` 轮换过（所有旧 salt 都算不出原 token）。注意通知阶段复用的是当前有效 token，不会轮换，所以已经发到群里的链接不会因为再发一次通知而失效。

**AI review 报告一直失败** —— 看报告里的 `error_message`。常见：`repo_missing` / `commit_missing`（`POSTDARE_REVIEW_REPO_DIR` 指向的目录不是 git 仓库或没有那个 commit——由 CI 在别处构建的项目 `app_dir` 下没有 checkout）；没装 python3；`POSTDARE_REVIEW_PI_BIN` 指向了一个随 Node 版本变动的 nvm 路径。详见 `deploy-stages.md`。

**报告阶段失败，部署却是成功** —— 预期行为。报告阶段的失败从不改变部署结论。

## 安全相关的"这不是 bug"

- 接口里的 secret 显示成 `abc******xyz`：响应脱敏。PATCH 时带回掩码值会被**忽略**，不会把 `******` 写进库。
- 删除项目只删数据库记录（项目、部署任务、阶段、webhook 事件），不动服务器上的文件和日志。
- 后端日志不打印完整 token、secret 和出站 webhook URL。
- 前端永远不能传命令字符串，部署命令只来自项目配置。
