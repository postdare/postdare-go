# Webhook 配置与排查

回调地址是公开路由（不需要 bearer），但每个请求都必须过 token 或签名校验。

## Gitee

```text
POST http://YOUR_HOST:8088/api/v1/webhooks/gitee/{project_key}?token=WEBHOOK_SECRET
```

token 可以放 `?token=` 查询参数，也可以放 `X-Gitee-Token` 或 `X-Git-Osc-Token` 请求头。

读取的字段：`ref`、`after`、`head_commit.id`、`head_commit.message`、`head_commit.author.name`、`commits`。

## GitHub

```text
POST http://YOUR_HOST:8088/api/v1/webhooks/github/{project_key}
```

GitHub 侧的 Secret 填项目的 `webhook_secret`，服务端按下式做常量时间比对：

```text
X-Hub-Signature-256 = sha256=HMAC_SHA256(webhook_secret, raw_body)
```

读取 `X-GitHub-Event`、`X-GitHub-Delivery`、`ref`、`after`、`head_commit.message`、`head_commit.author.name`。**只有 `push` 事件会触发部署。**

## 推了代码没部署

关键认知：webhook 被忽略时后端**不返回错误**，只在 `webhook_events` 表里记一条带 `ignored_reason` 的记录。所以第一步永远是看这张表，而不是翻部署日志（根本不会有部署日志）。

```bash
# 用 postdare-go-api skill 的脚本
python3 scripts/postdare.py webhook-events list --project my-app --format table
python3 scripts/postdare.py webhook-events get <id>     # 含 raw_payload
```

服务端**按固定顺序**判定，第一个命中的原因就是记录里的 `ignored_reason`。同一条原因也会出现在回调的 `202` 响应体里（`{"handled": false, "ignored_reason": "..."}`），所以在 GitHub/Gitee 的 delivery 详情里也能直接看到：

| `ignored_reason` | 含义与处理 |
| --- | --- |
| `project not found` | 回调 URL 里的 `{project_key}` 拼错了，或项目还没建 |
| `project git_provider mismatch` | 项目配的是 `gitee` 却调了 `/webhooks/github/...`，或反之 |
| `invalid webhook signature or token` | GitHub：secret 与项目 `webhook_secret` 不一致，或中间代理改写了 body（HMAC 算的是**原始字节**，任何 re-encode 都会破坏签名）。Gitee：`?token=` 和两个 token 头都没对上 |
| `unsupported event type` | 不是 push。tag、PR、以及配置 webhook 后 GitHub 发的第一条 `ping` 都会落到这里，属正常 |
| `auto deploy disabled` | 项目 `auto_deploy_enabled` 是 false |
| `branch mismatch` | 推送分支不等于项目 `branch`。服务端已经把 `refs/heads/main` 归一化成 `main` 再比对，所以项目里填短名即可 |

注意这个顺序意味着**签名错误会掩盖分支不匹配**：先把签名修对，再看下一条原因。

两种不在上表里的情况：

- **压根没有记录** —— 请求没到服务。查 GitHub/Gitee 侧的 delivery 日志、反向代理、防火墙，以及回调地址是否外部可达。
- **记录的 `ignored_reason` 是一句错误信息**（例如 `project has a running deploy task`）—— 校验全过了，但创建部署任务失败。最常见的就是该项目此刻已有 pending/running 任务被并发闸门挡下；此时回调返回的是 `409` 而不是 `202`。

## 出站 webhook

出站方向是 `outbound_webhook` 类型的 stage，URL 写在 stage 的 `config.url`，不是项目级字段。配置和模板见 `deploy-stages.md`。

用于告警时把 `run_when` 设为 `always`、`continue_on_error` 设为 `true`：通知渠道抖动不该让一次成功的发布被判成失败。出站失败会记进部署日志和后端日志，但后端日志**不会**打印完整 URL（里面通常含机器人 token）。
