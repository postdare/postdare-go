# AGENTS.md

Compact guidance for OpenCode sessions working in this repo.

## Layout

- Root Go module `github.com/hellodeveye/postdare-go` (go 1.22). The single binary entrypoint is `main.go` at the repository root and exposes `serve`, `mcp`, and `version` subcommands. App code lives under `internal/`.
- `web/` — Vite + React + TS SPA on :5173. It is not a separate Go workspace.
- `docs/` (REST API, webhooks, MCP, deployment), `examples/` (systemd units + deploy/rollback scripts), `PRODUCT.md` / `DESIGN.md` (product + visual intent: dark-first, restrained UI).

## Backend

Run Go commands from the repository root. `config.yaml` is optional and resolves from the current directory first, then from the binary directory.

```bash
go run . serve    # defaults to SQLite
go run . mcp      # needs POSTDARE_GO_BASE_URL + POSTDARE_GO_API_TOKEN env
go test ./...                # uses sqlite; NO MySQL required
go vet ./...                 # only static check configured
```

- Config path override: `POSTDARE_GO_CONFIG=/path/to/config.yaml`.
- `go test ./...` is safe to run anywhere — tests use in-memory/temp sqlite, never MySQL. Don't skip backend tests assuming a DB is required.
- No CI workflows, linter config, or pre-commit hooks. The full backend verification loop is `go vet ./... && go test ./...` from the repository root, or `make test`.
- `db.Open` runs GORM `AutoMigrate` and seeds the default admin on first boot. Without `POSTDARE_GO_ADMIN_PASSWORD`, the initial admin password is randomly generated, printed once in startup logs, and must be changed after first login.
- On startup, `ReconcileInterruptedTasks` marks any `pending`/`running` tasks as `failed` ("task interrupted by server restart").
- Runtime/secret values (`jwt.secret`, `mcp.api_token`, `database.dsn`, deploy timeouts, `log_dir`) come only from `config.yaml` and need a restart. `PATCH /api/v1/settings` stores non-runtime metadata only.

### Deploy pipeline (internal/service)

Deploys run the project's configured `deploy_stages` — typed `[]model.ProjectStage` with fields `Type` (`command`/`health_check`/`outbound_webhook`), `Name`, `RunWhen` (`""` for the main flow, or `success`/`failed`/`always` for deferred), `Enabled`, `ContinueOnError`, and `Config` (`json.RawMessage` decoded per `Type`). The main flow runs stages where `Enabled && RunWhen==""`; a failed stage with `ContinueOnError` is recorded as failed but does not abort the task, otherwise the task fails. After the task reaches a terminal state, deferred stages run per their `RunWhen` policy. Rollback runs `Project.RollbackCmd` as a single command stage, then deferred stages. Empty commands are **skipped**, not failed.

- One `pending`/`running` task per project, enforced with `SELECT ... FOR UPDATE` on the project row → `409 ErrProjectBusy`.
- `runner.LocalCommandRunner` runs each command via `bash -lc` in its own process group (`Setpgid`); cancel/timeout kills the whole group. Per-task log at `{deploy.log_dir}/{task_id}.log`.

### Security constraints enforced in code

- Deploy/build/test commands come ONLY from project config — the web UI never passes command strings at runtime.
- `app_log_path` must be absolute and resolve under the project's `app_dir` (symlinks resolved when the target exists). Deploy-log reads are likewise confined under `deploy.log_dir`. See `safePathInDir` in `internal/handler/handler.go`.
- `sanitizeLogText` strips ANSI escapes and invalid UTF-8 from every tailed log and SSE log stream.
- SSE `/stream` endpoints accept `?access_token=<jwt or mcp token>` as a fallback because `EventSource` can't set headers.
- `webhook_secret` and `notify_webhook` are masked in API responses; a value containing `******` is treated as masked and ignored on PATCH (`isMaskedValue`).
- MCP mutation tools (`trigger_deploy`, `trigger_rollback`) are off unless `mcp.allow_mutation_tools=true` AND the tool call passes `confirm=true`.
- The MCP server has two transports sharing one implementation (`internal/mcp`, dispatch entry `Server.HandleLine`): the `mcp` subcommand over stdio, and `POST /mcp` (Streamable HTTP, registered only when `mcp.enabled`) whose tool calls re-enter the same gin engine in process via `mcp.NewLocalServer`. `/mcp` takes the MCP token only, answers JSON (never SSE), keeps no session, and rejects an `Origin` outside `server.cors_origins`/`server.public_url`.
- Webhook verification differs by provider: GitHub requires `X-Hub-Signature-256` HMAC-SHA256; Gitee accepts `X-Gitee-Token` / `X-Git-Osc-Token` header or `?token=` query.

## Frontend

```bash
npm install
npm run dev     # Vite; proxies /api → http://127.0.0.1:8088
npm run build   # tsc -b && vite build — this is the only type/compile check
```

- No test script and no lint script. `npm run build` is the only verification step.
- Mocks: `VITE_ENABLE_MOCKS=true npm run dev`. Only active in Vite dev AND only on fetch failure (catch path in `src/api/client.ts`); production builds never fake API responses.
- `components.json` (shadcn) declares `@/components` and `@/lib/utils` aliases, but those aliases are NOT configured in `tsconfig.json` or `vite.config.ts`. Existing UI components use relative imports (`../../lib/utils`). If you add shadcn components, either configure the alias or rewrite imports to relative — don't assume `@/` resolves.
