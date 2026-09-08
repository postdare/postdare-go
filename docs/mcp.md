# MCP Server

Postdare Go serves the same MCP tools, resources and prompts over two transports. Neither reads MySQL directly: both answer a tool call by going through the Postdare Go REST API with an API token, so an agent reaches exactly what the REST handlers allow.

The API token is the effective MCP token, resolved in this order: `POSTDARE_GO_MCP_API_TOKEN`, then `mcp.api_token` in `config.yaml`, then `{data_dir}/secrets.yaml` — which the server generates on first boot when the other two are unset or still hold a `please-change-this-` placeholder. `data_dir` is `/data/postdare-go` by default and `./data` in the repository's `config.yaml`. Either way the backend only accepts the token when `mcp.enabled` is true.

## Transport 1: stdio

`postdare-go mcp` speaks JSON-RPC over stdin/stdout. The client launches it as a child process; there is no address to connect to. It reads neither `config.yaml` nor the database — only these two environment variables:

```bash
POSTDARE_GO_BASE_URL=http://127.0.0.1:8088 \
POSTDARE_GO_API_TOKEN="<mcp token>" \
postdare-go mcp
```

`POSTDARE_GO_BASE_URL` is the REST backend this process calls, defaulting to `http://127.0.0.1:8088`. Client configuration looks like:

```json
{
  "mcpServers": {
    "postdare-go": {
      "command": "postdare-go",
      "args": ["mcp"],
      "env": {
        "POSTDARE_GO_BASE_URL": "http://127.0.0.1:8088",
        "POSTDARE_GO_API_TOKEN": "<mcp token>"
      }
    }
  }
}
```

## Transport 2: Streamable HTTP

`postdare-go serve` mounts the same server at `POST /mcp`, next to the REST API rather than under `/api/v1`. Point a client at `http://127.0.0.1:8088/mcp` (or the public origin) with the MCP token as a bearer:

```bash
curl -X POST http://127.0.0.1:8088/mcp \
  -H "Authorization: Bearer <mcp token>" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```

Tool calls loop back into the running process — they are dispatched to the REST handlers directly, without a second network hop.

What the endpoint does and does not do:

- **Responses are always `application/json`**, never an SSE stream. Every tool returns in one round trip, so there is nothing to stream. The transport permits this.
- **No sessions.** No `Mcp-Session-Id` is issued or required, and `DELETE /mcp` answers `405`.
- **No server-initiated notifications**, so `GET /mcp` also answers `405` with `Allow: POST`.
- **A notification** (a request without `id`) is answered with a bare `202 Accepted`.
- **JSON-RPC batching is refused** with `-32600`; it was dropped in the 2025-06-18 revision.
- **Protocol versions**: `2024-11-05`, `2025-03-26` and `2025-06-18`. `initialize` echoes the client's version when it is one of these and otherwise proposes the newest. An `MCP-Protocol-Version` header naming an unsupported revision is rejected with `400`.
- **Requests carrying an `Origin`** must match `server.cors_origins` or `server.public_url`, or they are rejected with `403` — a browser that has been redirected to this endpoint must not be able to spend the token. Non-browser clients send no `Origin` and are unaffected.
- **The endpoint only exists when `mcp.enabled` is true**; otherwise `/mcp` is a `404`.
- **Only the MCP token opens it.** A user JWT authenticates against the REST API but is refused here with `403`.

Exposing `/mcp` grants no access the token did not already have: the same token is accepted on every `/api/v1` route.

## Tools

Both transports serve this list.

| Tool | Purpose |
| --- | --- |
| `postdare_go.list_projects` | List projects |
| `postdare_go.get_project` | Get project detail with sensitive fields masked |
| `postdare_go.list_deploy_tasks` | List recent deploy tasks |
| `postdare_go.get_deploy_task` | Get deploy task detail and stages |
| `postdare_go.read_deploy_log` | Read the last N deploy log lines |
| `postdare_go.read_app_log` | Read the last N app log lines |
| `postdare_go.trigger_deploy` | Trigger deploy when mutation tools are enabled |
| `postdare_go.trigger_rollback` | Trigger rollback when mutation tools are enabled |
| `postdare_go.analyze_failed_deploy` | Rule-based failure analysis |
| `postdare_go.list_deploy_task_reports` | List the reports a deploy task captured |
| `postdare_go.get_report` | Get one report with summary, issues and markdown |

Reports are the structured artifacts a deploy stage captures with `capture_as` (today `ai_review`). Both report tools are read-only and return the same payload as the REST endpoints, including each issue's `diff_hunk`, so an agent reads the reviewed code beside the finding. Sharing a report stays out of MCP: share links are created and revoked from the web UI only.

Mutation tools are disabled by default:

```yaml
mcp:
  allow_mutation_tools: false
```

To allow deploy or rollback through MCP, set:

```yaml
mcp:
  allow_mutation_tools: true
```

The tool call must still pass:

```json
{
  "project_id": 1,
  "confirm": true
}
```

## Resources

- `postdare-go://projects`
- `postdare-go://projects/{project_id}`
- `postdare-go://deploy-tasks/{task_id}`
- `postdare-go://deploy-tasks/{task_id}/logs`
- `postdare-go://projects/{project_id}/app-logs`
- `postdare-go://deploy-tasks/{task_id}/reports`
- `postdare-go://reports/{report_id}`

## Prompts

- `analyze_deploy_failure`
- `generate_release_summary`
- `suggest_ci_improvements`
