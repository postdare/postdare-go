# Postdare Go

一个面向个人和小团队的轻量级发布控制台，支持 RESTful API、Webhook、自动化测试、部署日志、告警、回滚和 MCP。

Postdare Go 适合一台或少量 Linux 服务器上的裸机部署流程。它不依赖 Docker、Kubernetes 或 Agent 节点，部署命令来自项目配置。

## 功能清单

- JWT 登录认证，bcrypt 密码加密
- 项目管理，支持 Gitee 和 GitHub
- Gitee WebHook 和 GitHub WebHook 自动部署
- 手动部署和回滚任务
- 可配置部署阶段：拉代码、测试、构建、部署等命令都通过 `deploy_stages` 排序执行
- 同一项目同一时间只允许一个 pending/running 任务
- 部署日志文件记录和 SSE 实时流
- 应用日志 tail 和 SSE 实时流
- 部署记录、阶段记录、Webhook 事件记录
- 出站 WebHook stage，兼容常见文本消息格式
- stdio MCP Server，支持查询、日志读取、失败分析，以及受控触发部署/回滚
- React 前端，暗色优先，支持浅色模式

## 技术栈

后端：

- Go
- Gin
- SQLite（默认，纯 Go 驱动）
- MySQL 8.x（可选）
- GORM
- Zap
- JWT
- bcrypt
- `os/exec`
- SSE

前端：

- React
- TypeScript
- Vite
- Tailwind CSS
- shadcn/ui 风格组件
- Radix UI
- TanStack Query
- React Router
- Zustand

## 目录结构

```text
postdare-go
├── cmd
│   └── postdare-go
├── internal
├── web
├── config.yaml
├── go.mod
├── examples
├── docs
├── PRODUCT.md
├── DESIGN.md
└── README.md
```

## 单二进制安装

```bash
make release
scp bin/postdare-go-linux-amd64 your-host:/opt/postdare-go/postdare-go
```

服务器上安装 systemd service：

```bash
sudo mkdir -p /opt/postdare-go /data/postdare-go
sudo cp examples/postdare-go.service /etc/systemd/system/postdare-go.service
sudo systemctl daemon-reload
sudo systemctl enable --now postdare-go
```

默认使用 SQLite，数据库文件为：

```text
/data/postdare-go/postdare.db
```

首次启动会自动生成 `<data_dir>/secrets.yaml`（权限 `0600`）和随机 `admin` 初始密码。初始密码只打印在启动日志里一次：

```bash
journalctl -u postdare-go -n 100
```

## 配置文件

`config.yaml` 是可选的；没有配置文件也可以按默认值启动。默认会先查找当前目录的 `config.yaml`，再查找二进制同目录的 `config.yaml`。需要自定义时可参考仓库根目录的 `config.yaml`。

可选 MySQL 配置：

```yaml
database:
  driver: mysql
  dsn: "root:password@tcp(127.0.0.1:3306)/postdare_go?charset=utf8mb4&parseTime=True&loc=Local"
```

使用 MySQL 时只需要提前创建空库，schema 由服务启动时的 GORM AutoMigrate 创建。

常用环境变量覆盖：

```text
POSTDARE_GO_PORT
POSTDARE_GO_DATA_DIR
POSTDARE_GO_DB_DRIVER
POSTDARE_GO_DB_PATH
POSTDARE_GO_DB_DSN
POSTDARE_GO_JWT_SECRET
POSTDARE_GO_MCP_API_TOKEN
POSTDARE_GO_ADMIN_PASSWORD
```

自定义运行数据目录可设置 `POSTDARE_GO_DATA_DIR`。只有仍使用内置默认值的派生路径会跟随该目录迁移，例如默认 SQLite 路径和默认部署日志目录；如果 `database.path` 或 `deploy.log_dir` 已在 `config.yaml` 中显式配置，则以显式值为准。

## 开发启动

```bash
go mod tidy
go run . serve
```

开发用配置默认把数据写到仓库根目录的 `data/`。也可以用 `POSTDARE_GO_DATA_DIR=/path/to/data` 指向其他目录。

默认监听：

```text
http://127.0.0.1:8088
```

前端开发：

```bash
cd web
npm install
npm run dev
```

默认地址：

```text
http://localhost:5173
```

前端会调用 `/api/v1`。Vite dev server 已代理到 `http://127.0.0.1:8088`。

如果只想先看界面，可以显式开启开发 mock：

```bash
cd web
VITE_ENABLE_MOCKS=true npm run dev
```

mock 只在 Vite 开发环境且显式开启时生效，生产构建不会伪造 API 响应。

## 默认账号

```text
username: admin
```

如果没有设置 `POSTDARE_GO_ADMIN_PASSWORD`，服务会随机生成初始密码并要求首次登录后修改。设置了 `POSTDARE_GO_ADMIN_PASSWORD` 时，使用该密码创建 `admin` 且不触发强制改密。

## 创建项目

进入 **Projects**，点击 **New project**，填写：

- 项目名称和项目标识
- Git 平台：`gitee` 或 `github`
- 分支
- 应用部署目录：例如 `/data/apps/my-app`
- 部署阶段列表：例如 `pull_code`、`unit_test`、`build`、`deploy`、`health_check`、`outbound_webhook`
- 回滚命令
- 应用日志路径
- Webhook secret

示例配置：

```text
项目名称：my-app
项目标识：my-app
Git 平台：github
分支：main
应用部署目录：/data/apps/my-app
部署阶段：
  1. pull_code：cd /data/repos/my-app && git fetch --all && git reset --hard origin/main
  2. unit_test：cd /data/repos/my-app && mvn clean test
  3. integration_test：cd /data/repos/my-app && mvn verify
  4. build：cd /data/repos/my-app && mvn package -DskipTests
  5. deploy：bash /data/apps/my-app/deploy.sh
  6. health_check：http://127.0.0.1:8080/actuator/health
  7. ai_review：always，/opt/postdare-go/bin/ai-review-xianhu，capture_as=report
  8. outbound_webhook：always，https://open.feishu.cn/open-apis/bot/v2/hook/xxx，feishu_report_card
回滚命令：bash /data/apps/my-app/rollback.sh
应用日志路径：/data/apps/my-app/logs/app.log
自动部署：开启
```

## WebHook

Gitee：

```text
POST /api/v1/webhooks/gitee/{project_key}?token=WEBHOOK_SECRET
```

GitHub：

```text
POST /api/v1/webhooks/github/{project_key}
```

GitHub 需要配置 `X-Hub-Signature-256`，签名格式为：

```text
sha256=HMAC_SHA256(secret, raw_body)
```

更多说明见 [docs/webhooks.md](docs/webhooks.md)。

## systemd

后端 service 示例：

```text
examples/postdare-go.service
```

业务应用 service 示例：

```text
examples/my-app.service
```

安装后端服务：

```bash
sudo cp examples/postdare-go.service /etc/systemd/system/postdare-go.service
sudo systemctl daemon-reload
sudo systemctl enable --now postdare-go
```

## 部署脚本

示例：

```text
examples/deploy.sh
examples/rollback.sh
```

脚本应该使用绝对路径，并由运行 Postdare Go 的用户具备执行权限。

## 日志

部署日志：

```text
/data/postdare-go/logs/deploy/{task_id}.log
```

接口：

```text
GET /api/v1/deploy-tasks/{task_id}/logs
GET /api/v1/deploy-tasks/{task_id}/logs/stream
```

应用日志只能读取项目配置的 `app_log_path`，并且该路径必须在项目 `app_dir` 内：

```text
GET /api/v1/projects/{project_id}/app-logs?lines=500
GET /api/v1/projects/{project_id}/app-logs/stream
```

默认最多返回 500 行，最大 5000 行。

## 出站 WebHook

出站 WebHook 通过 `outbound_webhook` stage 配置，URL 写在 stage 的 `config.url` 中。支持 `dingtalk_text`、`wecom_text`、`feishu_text`、`feishu_report_card` 和 `generic_json` 模板。

出站 WebHook 失败会记录到部署日志和后端日志；用于告警时建议设置 `continue_on_error=true`。

## MCP Server

启动：

```bash
POSTDARE_GO_BASE_URL=http://127.0.0.1:8088 \
POSTDARE_GO_API_TOKEN="<mcp token from /data/postdare-go/secrets.yaml or config.yaml>" \
go run . mcp
```

MCP tools：

- `postdare_go.list_projects`
- `postdare_go.get_project`
- `postdare_go.list_deploy_tasks`
- `postdare_go.get_deploy_task`
- `postdare_go.read_deploy_log`
- `postdare_go.read_app_log`
- `postdare_go.trigger_deploy`
- `postdare_go.trigger_rollback`
- `postdare_go.analyze_failed_deploy`

默认情况下，MCP mutation tools 关闭：

```yaml
mcp:
  allow_mutation_tools: false
```

开启后仍必须传：

```json
{
  "confirm": true
}
```

更多说明见 [docs/mcp.md](docs/mcp.md)。

## RESTful API

完整接口见 [docs/rest-api.md](docs/rest-api.md)。

所有管理接口需要：

```http
Authorization: Bearer TOKEN
```

触发部署和回滚返回 `202 Accepted`。

## 安全注意事项

- 未设置 `POSTDARE_GO_ADMIN_PASSWORD` 时，首次登录必须修改随机初始密码
- `jwt.secret` 和 `mcp.api_token` 默认生成到 `secrets.yaml`
- 环境变量中的 `POSTDARE_GO_JWT_SECRET` 和 `POSTDARE_GO_MCP_API_TOKEN` 优先级最高
- WebHook 必须配置 secret 或 token
- GitHub WebHook 使用 `X-Hub-Signature-256`
- 不要在前端传任意命令
- 部署命令只来自项目配置
- 不要在前端传任意日志路径
- 应用日志只读取项目配置的 `app_log_path`
- 删除项目只删除数据库元数据，不删除服务器文件
- MCP 触发部署和回滚默认关闭
- 部署命令有超时控制
- 后端日志不要打印完整 token、secret 或出站 WebHook URL

## 常见问题

### 后端启动失败，提示 SQLite 文件无法打开

检查 `/data/postdare-go` 是否存在，以及运行 Postdare Go 的用户是否有写权限。

### 使用 MySQL 时连接失败

确认 `database.driver: mysql`、DSN、用户名、密码和数据库名正确，并且目标库已创建。

### GitHub WebHook 没有触发部署

检查项目 `git_provider` 是否为 `github`，分支是否匹配，`webhook_secret` 是否与 GitHub 配置一致。

### WebHook 分支不匹配

Postdare Go 会记录 `webhook_events.ignored_reason = branch mismatch`，不会触发部署。

### 应用日志无法查看

确认项目 `app_log_path` 是绝对路径、位于项目 `app_dir` 内，并且运行 Postdare Go 的用户有读取权限。

### 同一项目无法再次部署

同一项目存在 `pending` 或 `running` 任务时，新部署会返回 `409 Conflict`。

### MCP 无法触发部署

确认 `mcp.allow_mutation_tools=true`，并且工具参数包含 `confirm=true`。
