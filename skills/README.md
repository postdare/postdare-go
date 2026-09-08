# Postdare Go Skills

两个可分发的 Agent Skill，配合 Claude Code / Claude.ai 等支持 skill 的客户端使用。

| Skill | 管什么 | 触发场景 |
| --- | --- | --- |
| `postdare-go-ops` | 安装、config.yaml 与环境变量、密钥解析、deploy_stages、webhook、日志路径、MCP 传输、故障排查 | "怎么配"、"为什么没触发"、"初始密码在哪" |
| `postdare-go-api` | 调 `/api/v1` 开放接口，含一个纯标准库的 Python 客户端 | "触发一次部署"、"写脚本查状态"、"CI 里调接口" |

两者互相引用但彼此独立，可以只装一个。

## 安装

**项目级**（随仓库走，clone 下来就能用）：

```bash
mkdir -p .claude/skills
cp -r skills/postdare-go-ops  .claude/skills/
cp -r skills/postdare-go-api  .claude/skills/
```

**用户级**（对本机所有项目生效）：

```bash
mkdir -p ~/.claude/skills
cp -r skills/postdare-go-ops  ~/.claude/skills/
cp -r skills/postdare-go-api  ~/.claude/skills/
```

装好后新开一个会话即可；模型会依据 SKILL.md 里的 `description` 自行判断何时加载。

## 直接用脚本（不装 skill）

`postdare.py` 是个独立的 CLI，不依赖 skill 机制，也不依赖 pip：

```bash
export POSTDARE_GO_BASE_URL=http://127.0.0.1:8088
export POSTDARE_GO_USERNAME=admin POSTDARE_GO_PASSWORD='...'
# 或 export POSTDARE_GO_API_TOKEN='<mcp.api_token>'

python3 skills/postdare-go-api/scripts/postdare.py projects list --format table
python3 skills/postdare-go-api/scripts/postdare.py deploy my-app --yes --watch
```

要求 Python 3.8+，只用标准库，所以可以直接拷到发布机上跑。

## 目录

```text
skills/
├── postdare-go-ops/
│   ├── SKILL.md
│   └── references/
│       ├── config.md            # 字段、环境变量、密钥解析、data_dir 迁移语义
│       ├── deploy-stages.md     # 三种 stage 类型、执行模型、AI review
│       ├── webhooks.md          # Gitee/GitHub 签名与"没触发"排查顺序
│       └── troubleshooting.md   # 按症状索引的故障清单
└── postdare-go-api/
    ├── SKILL.md
    ├── references/
    │   └── endpoints.md         # 完整路由表、请求体、错误码、curl 速查
    └── scripts/
        └── postdare.py          # 标准库 REST 客户端
```

## 维护

内容对齐的是当前代码，不是文档——`internal/handler/router.go` 的路由、`internal/middleware/middleware.go` 的鉴权、`internal/config/config.go` 的配置解析、`internal/model/models.go` 的字段。改动这些文件时，相应更新：

- 新增/改动路由 → `postdare-go-api/references/endpoints.md` 与 `postdare.py`
- 新增 stage 类型或 config 字段 → `postdare-go-ops/references/deploy-stages.md`
- 新增配置项或环境变量 → `postdare-go-ops/references/config.md`
- 新增错误码 → `endpoints.md` 的错误码表，必要时给 `postdare.py` 的 `ApiError.hint()` 加一条

`postdare.py` 的验证方式是对着一个真实实例跑：

```bash
POSTDARE_GO_CONFIG=/path/to/test-config.yaml POSTDARE_GO_ADMIN_PASSWORD=admin12345 go run . serve
```

然后走一遍 create → deploy --watch → logs → rollback → delete。
