# MCP Server

Postdare Go includes a stdio MCP server for AI agents. The MCP server does not read MySQL directly. It calls the Postdare Go REST API with an API token.

## Start

```bash
POSTDARE_GO_BASE_URL=http://127.0.0.1:8088 \
POSTDARE_GO_API_TOKEN="<mcp token from /data/postdare-go/secrets.yaml or config.yaml>" \
postdare-go mcp
```

The backend accepts `POSTDARE_GO_API_TOKEN` when it matches the effective MCP API token from environment variables, `config.yaml`, or generated `secrets.yaml`.
It only accepts that token when `mcp.enabled` is true.

## Tools

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
