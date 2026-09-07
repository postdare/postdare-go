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

A capture script must print the report object on stdout. The example script tolerates
a model that wraps it in a code fence or adds a sentence after it, extracting the
outermost JSON object. **It requires python3**: the server cannot tell an excerpt cut
from the diff from one the model invented, so it trusts whatever the script sends,
and that trust only holds if the script never forwards output it did not process.
Without python3 the example script reports a failure instead of passing the model's
output through -- install python3 wherever it runs. When output does not parse, the
failure carries the first 200 characters of what was printed, which is usually enough
to see why.

The same reasoning applies to any capture script you write: strip whatever `diff_hunk`
the model supplied before attaching one you cut yourself, since a hallucinated excerpt
presented as evidence is worse than no excerpt.

Each issue may carry a `diff_hunk`: the excerpt of the reviewed diff the finding
refers to, which makes a finding checkable instead of an assertion to take on trust.
A capture script must cut it from the real diff by the reported location rather than
let the model reproduce it, or the excerpt can be hallucinated and is then worse than
none. Cut it *around* that location, not from the top of the hunk: the example script
diffs with a wide context window so the model reads enough of the file, which makes a
hunk hundreds of lines long, and its opening lines are unchanged code. An excerpt of
those shows a finding with none of the change it is about. Excerpts are capped at 40 lines each and 64 KiB per report. Neither limit discards a
review or a finding, and neither cut is silent: an excerpt past the line cap ends with
`... truncated`, and one the report budget cannot hold is replaced by
`... excerpt omitted: report excerpt budget reached`, so a reader can always tell a
shortened or omitted excerpt from a finding that came without one.

Excerpts are served on the shared report as well, since a reader without repo access
is exactly who needs the code beside the finding. Note what that means operationally:
a share link is a bearer token, so whoever it reaches -- including anyone it is
forwarded to -- reads the excerpted source. Treat a report link like the diff itself,
and revoke one that has spread (`DELETE /api/v1/reports/{report_id}/share`).

Install the example script at the stable server path:

```bash
sudo make install-scripts            # or PREFIX=/somewhere make install-scripts
```

Run it from the release that built the binary. A capture script and the server that
reads its output are versioned together, and they drift silently in both directions:
an older script simply omits `diff_hunk`, and a newer one against an older server has
the field dropped as an unknown field. Neither reports an error, so install the pair
together.

The script holds no machine-specific values, so a release can ship it unchanged.
Point it at the reviewer through `/etc/postdare-go/ai-review.env` (see
`examples/ai-review.env`), or set the same variables in the stage command:

```bash
POSTDARE_REVIEW_PI_BIN=/path/to/pi      # defaults to "pi" on PATH
POSTDARE_REVIEW_MODEL=litellm/glm-5.3-flash
POSTDARE_REVIEW_SKILL_DIR=/path/to/skill   # optional; omitted means no --skill
```

The config file wins over the environment, so leave a value out of the file to set it
per stage. Pin `POSTDARE_REVIEW_PI_BIN` only when the binary is off PATH: an nvm path
carries a Node version and breaks on the next upgrade, and the failure surfaces as a
report that could not run rather than an obvious error.

Every refusal to run -- no reviewer, no python3, a missing skill directory, an
unparseable commit range -- is reported as a failed report rather than a non-zero
exit, so the reason is readable in the UI. The same reason goes to stderr, which the
runner keeps in the deploy log and a terminal shows directly, so running the script
by hand does not mean reading JSON to find out what went wrong.

To try it by hand, supply the variables Postdare would inject:

```bash
POSTDARE_PROJECT_DIR=/opt/postdare-go/app /opt/postdare-go/bin/ai-review
```

Configure the command as `/opt/postdare-go/bin/ai-review`, set
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
