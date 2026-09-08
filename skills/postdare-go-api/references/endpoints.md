# Postdare Go REST API 参考

面向"要自己写集成"的场景：完整路由、请求体、约束与错误码。日常调用请直接用 `scripts/postdare.py`。

- [信封与分页](#信封与分页)
- [鉴权](#鉴权)
- [路由表](#路由表)
- [项目对象](#项目对象)
- [deploy_stages 结构](#deploy_stages-结构)
- [错误码](#错误码)
- [curl 速查](#curl-速查)

## 信封与分页

成功：

```json
{ "data": {}, "request_id": "req_xxx" }
```

列表额外带 `pagination`：

```json
{ "data": [], "pagination": {"page": 1, "page_size": 20, "total": 100}, "request_id": "req_xxx" }
```

错误：

```json
{ "error": {"code": "PROJECT_NOT_FOUND", "message": "Project not found", "details": {}}, "request_id": "req_xxx" }
```

`request_id` 也会出现在响应头 `X-Request-ID` 和后端日志里；报障时带上它能直接定位到那一条请求。请求方可以自己传 `X-Request-ID` 做全链路串联。

分页参数 `page`（默认 1）、`page_size`（默认 20，**服务端硬上限 100**，超过按 100 处理）。排序 `sort=<field>:<asc|desc>`，字段白名单：项目 `created_at|updated_at|name`，任务 `created_at|updated_at|started_at|finished_at`；非法值静默回落到默认排序，不报错。

## 鉴权

```http
Authorization: Bearer <token>
```

两种 token 走同一个中间件，先比对 MCP token（constant-time），不匹配再按 JWT 解析：

- **MCP token** —— `mcp.api_token`。仅当 `mcp.enabled: true` 时被接受。actor = `mcp`，触发部署/回滚受 `mcp.allow_mutation_tools` 与请求体 `confirm: true` 双重限制。
- **用户 JWT** —— `POST /auth/login` 换取，有效期 `jwt.expire_hours`（默认 72h）。actor = `user`，不受 mutation 闸门限制。

SSE 的 `/stream` 路由允许把 token 放在 `?access_token=`，因为浏览器 `EventSource` 不能设置请求头。

`must_change_password` 为 true 时，除 `GET /auth/me`、`POST /auth/logout`、`PUT /auth/password` 外的受保护路由全部返回 `403 PASSWORD_CHANGE_REQUIRED`。

## 路由表

前缀 `/api/v1`。

### 公开

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/version` | 构建版本与 Go 版本 |
| POST | `/auth/login` | `{username, password}` → `{token, expires_at, user}` |
| POST | `/webhooks/gitee/{project_key}` | Gitee 回调，token 校验 |
| POST | `/webhooks/github/{project_key}` | GitHub 回调，`X-Hub-Signature-256` 校验 |
| GET | `/public/reports/{report_id}` | 分享页，token 放 `X-Report-Token` 头 |

### 认证

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/auth/me` | 当前身份，含 `actor` 与 `must_change_password` |
| POST | `/auth/logout` | 无状态，仅返回 ok |
| PUT | `/auth/password` | `{old_password, new_password}`，新密码 ≥ 8 位 |

### 项目

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/projects` | 过滤 `provider`；`sort` |
| POST | `/projects` | 创建，`201` |
| GET | `/projects/{id}` | 详情（secret 脱敏） |
| PATCH | `/projects/{id}` | 部分更新，白名单字段见下 |
| DELETE | `/projects/{id}` | `204`；连带删任务、阶段、webhook 事件 |
| POST | `/projects/{id}/deploy-tasks` | 触发部署，`202` |
| POST | `/projects/{id}/rollback-tasks` | 触发回滚，`202` |
| GET | `/projects/{id}/app-logs` | `?lines=` 默认 500，上限 5000 |
| GET | `/projects/{id}/app-logs/stream` | SSE |

PATCH 白名单：`name`、`project_key`、`git_provider`、`branch`、`app_dir`、`rollback_cmd`、`deploy_stages`、`app_log_path`、`webhook_secret`、`auto_deploy_enabled`。**白名单外的键被静默忽略**（不是报错），所以字段名写错时表现为"改了没生效"。

含 `******` 的 `webhook_secret` 和出站 webhook `config.url` 在 PATCH 时被视为掩码并保留原值，因此可以整体回传读到的对象。

DELETE 在项目还有 `pending`/`running` 任务时返回 `409`；物理的部署日志文件和应用日志文件不会被删。

### 部署任务

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/deploy-tasks` | 过滤 `project_id`、`status`；`sort` |
| GET | `/deploy-tasks/{id}` | 详情，含 `project` 与 `stages` |
| GET | `/deploy-tasks/{id}/stages` | 仅阶段列表，按 id 升序 |
| GET | `/deploy-tasks/{id}/logs` | `?lines=` |
| GET | `/deploy-tasks/{id}/logs/stream` | SSE，先回放最后 100 行再续流 |
| POST | `/deploy-tasks/{id}/cancel` | `202`；非 pending/running 返回 `409` |
| GET | `/deploy-tasks/{id}/analysis` | 规则化失败分析；**不校验任务状态** |
| GET | `/deploy-tasks/{id}/reports` | 该任务捕获的报告 |

任务状态：`pending`、`running`、`success`、`failed`、`canceled`、`rollbacked`。
触发类型：`manual`、`webhook`、`rollback`、`mcp`。
阶段状态：`pending`、`running`、`success`、`failed`、`skipped`。

日志文本在返回前会剥掉 ANSI 转义并把非法 UTF-8 替换掉，所以拿到的是干净文本。

### 报告

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/reports/{id}` | 详情，含 `issues[].diff_hunk` |
| POST | `/reports/{id}/share` | 生成/**轮换** 分享 token，返回 `{token, url}` |
| DELETE | `/reports/{id}/share` | 撤销分享 |

原始 token 不入库：由每份报告的随机 salt 加 `jwt.secret` 派生，所以拿到数据库副本也复原不出可用链接；反过来，轮换 `jwt.secret` 会让所有旧链接失效。

### 其他

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/webhook-events` | 过滤 `provider`、`project_id`；看 `ignored_reason` |
| GET | `/webhook-events/{id}` | 含 `raw_payload` |
| GET | `/dashboard/summary` | 今日总数/成功/失败、成功率、最近失败任务 |
| GET | `/dashboard/recent-deploy-tasks` | `?limit=` 默认 10，上限 50 |
| GET | `/settings` | 运行时配置快照（token 脱敏）+ 自定义元数据 |
| PATCH | `/settings` | 仅自由元数据，值必须是字符串 |

## 项目对象

```json
{
  "id": 1,
  "name": "my-app",
  "project_key": "my-app",
  "git_provider": "github",
  "branch": "main",
  "app_dir": "/data/apps/my-app",
  "rollback_cmd": "bash /data/apps/my-app/rollback.sh",
  "deploy_stages": [],
  "app_log_path": "/data/apps/my-app/logs/app.log",
  "webhook_secret": "s3c******ere",
  "auto_deploy_enabled": true
}
```

创建时的校验：`name` 与 `project_key` 非空；`git_provider` ∈ {`gitee`,`github`}（缺省 `gitee`）；`branch` 非空（缺省 `main`）；`app_dir` 非空且**必须是绝对路径**；`app_log_path` 若非空必须是绝对路径且解析软链后仍在 `app_dir` 内。校验失败返回 `422 INVALID_PROJECT`，`message` 会指明是哪个字段（含 `deploy_stages[i]` 下标）。

## deploy_stages 结构

```json
{
  "name": "unit_test",
  "type": "command",
  "enabled": true,
  "run_when": "",
  "continue_on_error": false,
  "config": {}
}
```

`type` 决定 `config` 的形状：

| type | config |
| --- | --- |
| `command` | `{"command": "...", "capture_as": "ai_review"}` — `capture_as` 可选，目前只有 `ai_review`（旧写法 `report` 仍兼容） |
| `health_check` | `{"url": "http://127.0.0.1:8080/actuator/health"}` |
| `outbound_webhook` | `{"url": "...", "template": "feishu_report_card", "message_template": "..."}` |

`run_when` 为 `""` 表示主流程；`success` / `failed` / `always` 表示任务到终态后运行。`enabled: true` 的 stage 其必填 config 字段为空会被拒；`enabled: false` 则不校验。出站 webhook 的 `template` 取值：`dingtalk_text`、`wecom_text`、`feishu_text`、`feishu_report_card`、`generic_json`。

## 错误码

| 码 | HTTP | 含义与处理 |
| --- | --- | --- |
| `UNAUTHORIZED` | 401 | 缺失或非法 bearer；JWT 过期后重新登录 |
| `PASSWORD_CHANGE_REQUIRED` | 403 | 先改密码 |
| `MCP_MUTATION_DISABLED` | 403 | MCP token 触发部署被闸门挡下 |
| `CONFIRM_REQUIRED` | 422 | MCP token 的写操作请求体缺 `confirm: true` |
| `MCP_TOKEN_REQUIRED` | 403 | `/mcp` 端点不收用户 JWT |
| `PROJECT_NOT_FOUND` | 404 | id 不存在 |
| `INVALID_PROJECT` | 400/422 | 请求体不合法或字段校验失败，看 `message` |
| `PROJECT_DEPLOY_RUNNING` | 409 | 该项目已有 pending/running 任务 |
| `PROJECT_HAS_ACTIVE_TASK` | 409 | 有活动任务时不允许删项目 |
| `ROLLBACK_CMD_REQUIRED` | 422 | 项目 `rollback_cmd` 为空 |
| `DEPLOY_TASK_NOT_CANCELABLE` | 409 | 任务已到终态 |
| `SERVICE_SHUTTING_DOWN` | 503 | 服务正在停机 |
| `APP_LOG_NOT_CONFIGURED` | 422 | 项目未配 `app_log_path` |
| `APP_LOG_NOT_FOUND` / `LOG_NOT_FOUND` | 404 | 路径越界、文件不存在或无读权限 |
| `SETTING_READ_ONLY` | 422 | 该配置项只能改 config.yaml 并重启 |
| `REPORT_NOT_FOUND` | 404 | 报告不存在，或分享 token 不匹配 |

## curl 速查

```bash
BASE=http://127.0.0.1:8088
TOKEN=$(curl -s -X POST $BASE/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"..."}' | python3 -c 'import json,sys;print(json.load(sys.stdin)["data"]["token"])')

curl -s $BASE/api/v1/projects -H "Authorization: Bearer $TOKEN"

# 触发部署；confirm 对 JWT 无害，对 MCP token 必需
curl -s -X POST $BASE/api/v1/projects/1/deploy-tasks \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"confirm":true}'

# SSE 跟部署日志（token 走 query，因为 EventSource 不能带 header）
curl -N "$BASE/api/v1/deploy-tasks/1/logs/stream?access_token=$TOKEN"
```

MCP 的 Streamable HTTP 端点不在 `/api/v1` 之下，且只认 MCP token：

```bash
curl -s -X POST $BASE/mcp \
  -H "Authorization: Bearer $MCP_TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```
