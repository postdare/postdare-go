# Deployment Notes

Postdare Go targets Linux hosts with Git, systemd, and language-specific build tools installed by the user. The default deployment is one static Go binary plus a SQLite database under `/data/postdare-go`.

## Build Release

```bash
make release
```

This builds the web UI, embeds it into the server, and writes Linux binaries to `bin/`.

Copy the binary to the server:

```bash
sudo mkdir -p /opt/postdare-go /data/postdare-go
scp bin/postdare-go-linux-amd64 your-host:/opt/postdare-go/postdare-go
```

Install the service:

```bash
sudo cp examples/postdare-go.service /etc/systemd/system/postdare-go.service
sudo systemctl daemon-reload
sudo systemctl enable --now postdare-go
```

No database initialization is required for the default SQLite setup. On first start, Postdare Go creates:

```text
/data/postdare-go/postdare.db
/data/postdare-go/secrets.yaml
/data/postdare-go/logs/deploy/
```

Find the one-time generated admin password in the service log:

```bash
journalctl -u postdare-go -n 100
```

## Optional MySQL

MySQL remains available by configuration:

```yaml
database:
  driver: mysql
  dsn: "root:password@tcp(127.0.0.1:3306)/postdare_go?charset=utf8mb4&parseTime=True&loc=Local"
```

Create an empty database before starting the service. Tables are created by AutoMigrate on boot.

The one-off `copydb` subcommand used for the 2026-07 MySQL → SQLite migration has been
removed; recover it from git history if a similar migration is ever needed again.

## Deploy Stages

Each project defines an ordered list of typed deploy stages (`deploy_stages`). Supported
stage types are `command`, `health_check`, and `outbound_webhook`.

- `enabled: false` skips the stage.
- `continue_on_error: true` records the stage as failed but keeps the pipeline running.
- `run_when: success|failed|always` runs a stage after the main flow reaches a final status.

`health_check` and outbound WebHook calls are regular stages, so their order is controlled
by the project configuration. Rollback stays separate and uses `rollback_cmd`; after a
rollback reaches a final status, matching deferred stages such as `outbound_webhook`
run according to their `run_when` value.

Project commands should be explicit and absolute, for example:

```bash
cd /data/repos/my-app && git fetch --all && git reset --hard origin/main
cd /data/repos/my-app && mvn clean test
cd /data/repos/my-app && mvn package -DskipTests
bash /data/apps/my-app/deploy.sh
bash /data/apps/my-app/rollback.sh
```

Do not pass command strings from the web UI at deploy time. Store stages in the project
configuration.

### AI review report

Command stages may set `config.capture_as` to the report type they produce; the only
type today is `ai_review`. Postdare captures at most 2 MiB of stdout as structured JSON
and keeps it out of the deploy log; stderr remains in the log. The script's
`report_type` field must match the stage's `capture_as`, otherwise the capture is
recorded as failed. The value `report` is the legacy spelling of `ai_review` and is
still accepted, so project configurations written before this change keep working.

Every command stage -- not only report stages -- receives `POSTDARE_TASK_ID`,
`POSTDARE_TRIGGER_TYPE`, `POSTDARE_PROJECT_DIR`, `POSTDARE_COMMIT_ID`, and
`POSTDARE_BEFORE_COMMIT_ID`. Webhook deploys use the full `before..after` range;
manual and MCP deploys use `HEAD^..HEAD`. Report-stage failures never change the
deployment result.

Each issue may carry a `diff_hunk`: the excerpt of the reviewed diff the finding
refers to, which makes a finding checkable instead of an assertion to take on trust.
A capture script must cut it from the real diff by the reported location rather than
let the model reproduce it, or the excerpt can be hallucinated and is then worse than
none. Excerpts are capped at 40 lines each and 64 KiB per report; over-long ones are
trimmed and marked, never rejected, so evidence limits never cost a review.

`GET /api/v1/public/reports/{report_id}` withholds `diff_hunk`. A share link is a
bearer token that gets forwarded through chat, and findings and file paths are the
point of sharing while the reviewed source is not. The authenticated endpoints serve
excerpts in full.

Install the Xianhu example script at the stable server path:

```bash
sudo install -m 0755 examples/ai-review-xianhu /opt/postdare-go/bin/ai-review-xianhu
```

Configure the command as `/opt/postdare-go/bin/ai-review-xianhu`, set
`capture_as: ai_review`, `run_when: always`, and keep the Feishu outbound stage after it
with template `feishu_report_card`. Set `server.public_url` to the externally reachable
HTTPS origin so the card can link to `/reports/{report_id}`.

Authenticated report endpoints are:

- `GET /api/v1/deploy-tasks/{task_id}/reports`
- `GET /api/v1/reports/{report_id}`
- `POST /api/v1/reports/{report_id}/share` (creates or rotates the token)
- `DELETE /api/v1/reports/{report_id}/share`

The public page calls `GET /api/v1/public/reports/{report_id}` with the token in the
`X-Report-Token` header.

The raw token is never stored. Each report holds a random salt, and the token is
derived from that salt plus `jwt.secret`, so a database copy alone yields no working
link. Notification stages reuse the token already in force, which keeps a link that was
already delivered to a chat channel working; only `POST .../share` rotates the salt and
invalidates links handed out earlier.

Links shared before this change keep resolving, because verification goes through the
stored digest. They are re-minted only if that report is shared or notified again, since
their original token cannot be recovered. Rotating `jwt.secret` orphans every salt drawn
under the old secret: existing links stop resolving and are re-minted on the next share.

## Logs

Deploy logs are written to:

```text
/data/postdare-go/logs/deploy/{task_id}.log
```

Application logs come from each project's configured `app_log_path`.

For safety, `app_log_path` must be an absolute path under the project's `app_dir`. Postdare Go rejects app log reads that escape that directory, including resolved symlinks when the target exists.
