# 部署阶段（deploy_stages）

一个项目的流水线是一个有序数组，每个元素是一个 typed stage。命令**只能**来自这里——前端和 API 都不接受运行时传入的命令字符串，这是这套系统最重要的安全边界。

## 字段

```json
{
  "name": "unit_test",
  "type": "command",
  "enabled": true,
  "run_when": "",
  "continue_on_error": false,
  "config": { "command": "cd /data/repos/my-app && mvn clean test" }
}
```

| 字段 | 说明 |
| --- | --- |
| `name` | 必填，显示在阶段记录和日志前缀里 |
| `type` | `command` / `health_check` / `outbound_webhook` |
| `enabled` | false 则整条跳过，且不校验它的 config |
| `run_when` | `""` 主流程；`success` / `failed` / `always` 延后执行 |
| `continue_on_error` | true：该阶段失败记为 failed 但流水线继续 |
| `config` | 按 `type` 解析，形状见下 |

## 执行模型

1. 主流程按数组顺序跑所有 `enabled && run_when == ""` 的阶段。
2. 某阶段失败：带 `continue_on_error` 就继续，否则整个任务判定 failed 并停止主流程。
3. 任务到达终态（`success` / `failed`）后，按 `run_when` 匹配跑延后阶段。
4. 回滚不走这个数组：只跑 `rollback_cmd` 一条命令，然后同样跑匹配的延后阶段。
5. 命令为空的阶段是**跳过**（`skipped`），不是失败。

延后阶段是告警的正确落点：`run_when: always` + `continue_on_error: true` 的出站 webhook，无论成败都发通知，且通知本身失败不会影响部署结论。

并发上，同一项目同一时刻只允许一个 `pending`/`running` 任务（项目行上的 `SELECT ... FOR UPDATE`），冲突返回 `409`。

## 三种 config

### command

```json
{ "command": "cd /data/repos/my-app && mvn clean test", "capture_as": "ai_review" }
```

命令用 `bash -lc` 在独立进程组里执行，默认超时 30 分钟（`deploy.command_timeout_minutes`），超时或取消会杀掉整个进程组。stdout/stderr 进部署日志。

每个命令阶段都会拿到这些环境变量：

```text
POSTDARE_TASK_ID
POSTDARE_TRIGGER_TYPE
POSTDARE_PROJECT_DIR
POSTDARE_COMMIT_ID
POSTDARE_BEFORE_COMMIT_ID
```

Webhook 触发的部署给的是完整 `before..after` 区间；手动和 MCP 触发给的是 `HEAD^..HEAD`。

`capture_as` 让这个阶段的 stdout 被当作结构化报告捕获（最多 2 MiB），不进部署日志；stderr 仍然进日志。目前唯一的类型是 `ai_review`（旧写法 `report` 仍兼容）。脚本打印的 JSON 里 `report_type` 必须与 `capture_as` 一致，否则记为捕获失败。**报告阶段失败不改变部署结论。**

### health_check

```json
{ "url": "http://127.0.0.1:8080/actuator/health" }
```

作为普通阶段参与排序，所以想在部署后校验就把它放在 deploy 之后。

### outbound_webhook

```json
{ "url": "https://open.feishu.cn/open-apis/bot/v2/hook/xxx", "template": "feishu_report_card" }
```

模板取值：`dingtalk_text`、`wecom_text`、`feishu_text`、`feishu_report_card`、`generic_json`。`message_template` 可选，用于自定义文本。

URL 在 API 响应里是脱敏的，PATCH 时带回掩码值会保留原值。出站失败会记进部署日志和后端日志——**用于告警时务必设 `continue_on_error: true`**，否则通知服务抖动会把一次成功的发布判成失败。

## 一套完整示例

```json
[
  {"name": "pull_code",  "type": "command", "enabled": true,
   "config": {"command": "cd /data/repos/my-app && git fetch --all && git reset --hard origin/main"}},

  {"name": "unit_test",  "type": "command", "enabled": true,
   "config": {"command": "cd /data/repos/my-app && mvn clean test"}},

  {"name": "build",      "type": "command", "enabled": true,
   "config": {"command": "cd /data/repos/my-app && mvn package -DskipTests"}},

  {"name": "deploy",     "type": "command", "enabled": true,
   "config": {"command": "bash /data/apps/my-app/deploy.sh"}},

  {"name": "health_check", "type": "health_check", "enabled": true,
   "config": {"url": "http://127.0.0.1:8080/actuator/health"}},

  {"name": "ai_review",  "type": "command", "enabled": true,
   "run_when": "always", "continue_on_error": true,
   "config": {"command": "/opt/postdare-go/bin/ai-review", "capture_as": "ai_review"}},

  {"name": "notify",     "type": "outbound_webhook", "enabled": true,
   "run_when": "always", "continue_on_error": true,
   "config": {"url": "https://open.feishu.cn/open-apis/bot/v2/hook/xxx",
              "template": "feishu_report_card"}}
]
```

顺序上，`ai_review` 要排在 `notify` 之前，飞书卡片才有报告可引用。

## AI review 阶段

示例脚本在 `examples/ai-review`，用 `sudo make install-scripts` 装到 `/opt/postdare-go/bin/ai-review`。它依赖 **python3**——没有 python3 时脚本会如实报告失败，而不是把模型输出原样透传：服务端无法分辨 `diff_hunk` 是从真实 diff 里裁出来的还是模型编的，这份信任只有在脚本自己做处理时才成立。

机器相关的配置放 `/etc/postdare-go/ai-review.env`（见 `examples/ai-review.env`），或者直接写在 stage 命令里：

```bash
POSTDARE_REVIEW_PI_BIN=/path/to/pi          # 默认走 PATH 上的 pi
POSTDARE_REVIEW_MODEL=litellm/glm-5.3-flash
POSTDARE_REVIEW_SKILL_DIR=/path/to/skill    # 可选
POSTDARE_REVIEW_REPO_DIR=/srv/repos/example # 默认取项目的 app_dir
```

**配置文件优先于环境变量**，所以想按 stage 区分某个值，就把它从文件里去掉。

`POSTDARE_REVIEW_REPO_DIR` 是最容易出问题的一项：评审要从 git 仓库读 diff，默认用项目 `app_dir`。只有"直接 pull 到 app_dir 部署"的项目那里才有 git 历史；由 CI 在别处构建、以产物形式投递的项目，`app_dir` 下没有 checkout，每次都会以 `repo_missing` 或 `commit_missing` 失败。这时把它指向同一个远端的一个克隆——脚本在 commit 缺失时会从 `origin` fetch，而且不会交互式要凭据，所以那个克隆需要一个能无人值守读取的 remote。

脚本每一种拒绝执行的情况（没有 reviewer、没有 python3、skill 目录不存在、commit 区间解析不了）都以"失败的报告"呈现而不是非零退出，原因同时写到 stderr，所以在 UI 里和终端里都能直接看到。

手工试跑：

```bash
POSTDARE_REVIEW_REPO_DIR=/srv/repos/example /opt/postdare-go/bin/ai-review
```

不给 commit 区间时它评审 `HEAD^..HEAD`，等价于最近一次推送触发的部署会看到的内容。

**脚本和二进制要成对安装。** 旧脚本对新服务端只是少了 `diff_hunk` 字段，新脚本对旧服务端则是该字段被当未知字段丢弃——两个方向都不报错，只会静默劣化。
