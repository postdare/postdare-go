---
name: postdare-go-api
description: 用 Python 脚本调用 Postdare Go（postdare-go）发布控制台的开放接口 REST API——查项目、触发部署/回滚、等待任务终态、读部署日志和应用日志、看 webhook 事件、读 AI review 报告、项目 CRUD、看板与 issue（建卡/改卡/拖列/关联部署/上传图片）。脚本 scripts/postdare.py 只依赖标准库，支持 MCP token 与账号密码（JWT）两种鉴权。Use this skill whenever someone wants to call, script, automate, or integrate the postdare-go HTTP API — "帮我触发一次部署"、"写个脚本查部署状态"、"批量导入项目"、"建个看板把 issue 排一下"、"把 commit 和 issue 关联上"、"在 CI 里调用 postdare 接口"、"把发布结果推到别的系统"、"curl 一下 postdare 的接口怎么写" — or asks what the /api/v1 endpoints, request bodies, error codes or auth rules are.
---

# Postdare Go 开放接口调用

`scripts/postdare.py` 是一个只依赖 Python 标准库的 REST 客户端，覆盖 `/api/v1` 的全部路由。**优先用它，不要现写 curl 或 requests 代码**——它已经处理好了这套 API 里几个非显然的地方：token 缓存与过期重登、MCP token 的 mutation 闸门、SSE 流式日志的 `?access_token=` 回退、脱敏字段的安全回传、翻页。

需要装机、配 stage、排查 webhook 不触发时，用配套的 `postdare-go-ops` skill。

## 先想清楚用哪种身份

这套 API 有两种 bearer，**能力不同**，选错会在触发部署时撞墙：

| 凭据 | 环境变量 | 触发部署/回滚、写 issue |
| --- | --- | --- |
| 用户 JWT | `POSTDARE_GO_USERNAME` + `POSTDARE_GO_PASSWORD` | 可以 |
| MCP token | `POSTDARE_GO_API_TOKEN` | **默认被拒** |

MCP token 在每条 `/api/v1` 路由上都被接受，但后端把它标记为 `mcp` actor，因此 `POST /projects/{id}/deploy-tasks`、rollback，以及 issue 的创建/修改/移动会被 `403 MCP_MUTATION_DISABLED` 挡下，除非服务端 `mcp.allow_mutation_tools: true`（此时还要求请求体带 `confirm: true`，脚本已自动带上）。读接口不受闸门限制：用 MCP token 照样能列看板、读 issue。**要自动化发布，用账号密码**；只读监控用 MCP token 更省事。

另外两个前置条件容易忘：MCP token 只在 `mcp.enabled: true` 时被接受；账号如果还是生成的初始密码，除改密相关的三个接口外一律 `403 PASSWORD_CHANGE_REQUIRED`（脚本有 `passwd` 子命令）。

```bash
export POSTDARE_GO_BASE_URL=http://127.0.0.1:8088   # 默认值，非本机时必须设
export POSTDARE_GO_USERNAME=admin
export POSTDARE_GO_PASSWORD='...'
# 或者
export POSTDARE_GO_API_TOKEN='<secrets.yaml / config.yaml 里的 mcp.api_token>'
```

JWT 会缓存在 `~/.cache/postdare-go/tokens.json`（0600），过期或服务端轮换过 `jwt.secret` 时自动重登，所以循环调用不会每次都打 `/auth/login`。

## 用法

```bash
python3 scripts/postdare.py --help          # 全部子命令
python3 scripts/postdare.py <命令> --help    # 单条命令的参数
```

**所有写操作必须带 `--yes`**，放在命令任意位置都行。这不是形式主义：这个脚本会对真实服务器发起真实发布，而 `deploy` 打错项目名的代价是一次误发布。删除项目还额外要求 `--confirm-key <project_key>`，因为删项目会连带删掉它的部署任务、阶段记录和 webhook 事件。

常用：

```bash
# 看状态
postdare.py projects list --format table
postdare.py tasks list --project my-app --status failed --format table
postdare.py dashboard summary

# 发布并等到终态（成功退出 0，失败退出 1，适合放进 CI）
postdare.py deploy my-app --yes --watch

# 出事时
postdare.py tasks logs 42 --format text          # tail 部署日志
postdare.py tasks logs 42 --follow               # SSE 实时跟
postdare.py tasks analyze 42                     # 规则化失败分析
postdare.py app-logs my-app --lines 200 --format text
postdare.py rollback my-app --yes --watch

# 为什么推了代码没部署
postdare.py webhook-events list --project my-app --format table   # 看 ignored_reason

# 看板与 issue（看板参数同样接受 id 或 key）
postdare.py boards list --format table
postdare.py boards create '{"name":"Engineering","key":"ENG"}' --yes
postdare.py boards issues ENG --status todo --format table
postdare.py issues create ENG '{"title":"部署日志尾巴是空的","priority":"high"}' --yes
postdare.py issues update 42 '{"description":"1.4.2 复现"}' --yes
postdare.py issues move 42 --status done --yes       # 也可用 --after/--before 指定邻居
postdare.py issues link 42 100 --yes                 # commit message 没写到时手工关联部署任务
postdare.py attachments upload ./screenshot.png --yes
```

项目参数处处接受 **id 或 `project_key`**，脚本会自动解析；看板参数接受 **id 或 `key`**（`ENG`）。写脚本时用 `project_key` / 看板 `key`——它们是人写在配置和 commit message 里的稳定标识，id 会随环境变化。

输出默认是完整 JSON（含 `request_id`，便于和后端日志对账）；`--format table` 给人看，`--format text` 让日志类命令直接吐原文，方便 `| grep`。

## 值得知道的行为

- **触发部署返回 `202`，不是完成。** 响应里是一个 `pending` 任务。要确认结果就用 `--watch`（轮询到 `success` / `failed` / `canceled` / `rollbacked`），它把状态变迁打到 stderr、最终 JSON 打到 stdout，所以 `> result.json` 不会被进度污染。
- **同一项目同时只能有一个 pending/running 任务**，并发触发第二个会 `409 PROJECT_DEPLOY_RUNNING`。CI 里请先 `--watch` 等前一个结束。
- **`tasks analyze` 不判断任务状态**，对成功的任务也会输出一段"某阶段失败"的分析。它只对失败任务有意义。
- **响应里的 secret 是脱敏的**（`s3c******ere`），但 PATCH 时带回掩码值会被后端**忽略**而不是写坏，所以"读出来改一改再整体写回去"是安全的。出站 webhook URL 同理。
- **应用日志有路径约束**：只能读项目配置的 `app_log_path`，且必须落在 `app_dir` 内。`APP_LOG_NOT_FOUND` 通常是路径越界或权限，不是接口问题。
- **`settings set` 只能写自由元数据**。任何 `server.*` / `database.*` / `jwt.*` / `deploy.*` / `app_log.*` / `mcp.*` 或含 secret/token 的 key 都会 `422 SETTING_READ_ONLY`——那些要改 `config.yaml` 并重启。
- **`reports share` 会轮换 token**，之前发出去的链接立刻失效；而且分享页会连带暴露 issue 里的 `diff_hunk`（被评审的源码片段）。别为了“看一眼”去 share 一个已经分享过的报告。
- **看板 issue 的 `--status` 会真的动卡片**：`issues move` / `issues update '{"status":...}'` 会给卡片在新列里重新算排序 key，把 `done`/`canceled` 走完。要真关闭 issue 才用它们，别拿来做“只改个优先级”。
- **issue 的 `status` 与 `priority` 是固定集合**，不是自由文本；看板列固定为 `backlog`/`todo`/`in_progress`/`done`/`canceled`，所以“这个部署是否完成了这项工作”有一个后端可判定的答案，不需要额外配置。
- **上传的图片只有被引用了才算数**：上传本身不绑定 issue，直到某个保存的描述里出现它的 `url`；24 小时无人引用的上传会被清理。

失败时脚本把 `code` / `message` / `details` / `request_id` 打到 stderr，并对几个最容易误读的错误码附一条 hint，退出码 2。参数或本地校验问题退出 1。

## 写自己的集成

要在别处（Go、Node、CI 步骤）复刻调用，或者需要完整的路由表、请求体字段、`deploy_stages` 结构和错误码清单，读 **`references/endpoints.md`**。`postdare.py` 里的 `Client` 类也可以直接 import 复用：

```python
from postdare import Client
client = Client("https://go.example.com", username="admin", password="...")
tasks = client.paged("/deploy-tasks", {"status": "failed"}, fetch_all=True)["data"]
```

统一的响应信封是 `{"data": ..., "request_id": "..."}`，列表多一个 `pagination`，错误是 `{"error": {"code", "message", "details"}, "request_id"}`。分页默认 `page_size=20`、上限 100，脚本的 `--all` 会自动翻完。
