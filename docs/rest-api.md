# REST API

All management endpoints use JWT or the configured MCP API token:

```http
Authorization: Bearer TOKEN
```

Successful response:

```json
{
  "data": {},
  "request_id": "req_xxx"
}
```

List response:

```json
{
  "data": [],
  "pagination": {
    "page": 1,
    "page_size": 20,
    "total": 100
  },
  "request_id": "req_xxx"
}
```

Error response:

```json
{
  "error": {
    "code": "PROJECT_NOT_FOUND",
    "message": "Project not found",
    "details": {}
  },
  "request_id": "req_xxx"
}
```

## Version

| Method | Path | Description |
| --- | --- | --- |
| GET | `/api/v1/version` | Build version and Go runtime version |

## Auth

| Method | Path | Description |
| --- | --- | --- |
| POST | `/api/v1/auth/login` | Login |
| GET | `/api/v1/auth/me` | Current user |
| POST | `/api/v1/auth/logout` | Logout |
| PUT | `/api/v1/auth/password` | Change current user's password |

`POST /auth/login` and `GET /auth/me` include `must_change_password`. When it is `true`, secured endpoints except `GET /auth/me`, `POST /auth/logout`, and `PUT /auth/password` return:

```json
{
  "error": {
    "code": "PASSWORD_CHANGE_REQUIRED",
    "message": "Password change is required before continuing"
  }
}
```

Change password request:

```json
{
  "old_password": "current-password",
  "new_password": "new-password"
}
```

The new password must be at least 8 characters.

## Projects

| Method | Path | Description |
| --- | --- | --- |
| GET | `/api/v1/projects` | List projects |
| POST | `/api/v1/projects` | Create project |
| GET | `/api/v1/projects/{project_id}` | Get project |
| PATCH | `/api/v1/projects/{project_id}` | Update project |
| DELETE | `/api/v1/projects/{project_id}` | Delete project and related database records |
| POST | `/api/v1/projects/{project_id}/deploy-tasks` | Trigger deploy, returns `202 Accepted` |
| POST | `/api/v1/projects/{project_id}/rollback-tasks` | Trigger rollback, returns `202 Accepted` |
| GET | `/api/v1/projects/{project_id}/app-logs` | Read app log tail |
| GET | `/api/v1/projects/{project_id}/app-logs/stream` | Stream app log over SSE |

Project deletion is destructive for database records: it removes the project, related deploy tasks, related deploy task stages, deploy task reports, issue-to-deploy-task links, and webhook events associated with the project. Boards that were linked to the project keep their issues and are unlinked. It returns `409 Conflict` if the project has a `pending` or `running` deploy or rollback task. Physical deploy log files and application log files are not removed.

## Deploy Tasks

| Method | Path | Description |
| --- | --- | --- |
| GET | `/api/v1/deploy-tasks` | List tasks |
| GET | `/api/v1/deploy-tasks/{task_id}` | Task detail |
| GET | `/api/v1/deploy-tasks/{task_id}/stages` | Task stages |
| GET | `/api/v1/deploy-tasks/{task_id}/logs` | Deploy log tail |
| GET | `/api/v1/deploy-tasks/{task_id}/logs/stream` | Stream deploy log over SSE |
| POST | `/api/v1/deploy-tasks/{task_id}/cancel` | Mark pending/running task canceled |
| GET | `/api/v1/deploy-tasks/{task_id}/analysis` | Rule-based failure analysis |

## Boards and Issues

| Method | Path | Description |
| --- | --- | --- |
| GET | `/api/v1/boards` | List boards with linked project name and per-column issue counts |
| POST | `/api/v1/boards` | Create board |
| GET | `/api/v1/boards/{board_id}` | Get board |
| PATCH | `/api/v1/boards/{board_id}` | Update name, description, or linked project |
| DELETE | `/api/v1/boards/{board_id}` | Delete a board that has no issues |
| GET | `/api/v1/boards/{board_id}/issues` | List the board's issues |
| POST | `/api/v1/boards/{board_id}/issues` | Create an issue |
| GET | `/api/v1/boards/{board_id}/labels` | Distinct labels the board already uses |
| GET | `/api/v1/boards/{board_id}/stream` | Stream issue changes over SSE |
| GET | `/api/v1/issues/meta` | Accepted statuses and priorities |
| GET | `/api/v1/issues/{issue_id}` | Get issue |
| PATCH | `/api/v1/issues/{issue_id}` | Update issue |
| DELETE | `/api/v1/issues/{issue_id}` | Delete issue |
| POST | `/api/v1/issues/{issue_id}/move` | Move the issue to a column and position |
| POST | `/api/v1/issues/{issue_id}/deploy-links` | Link a deploy task by hand |
| DELETE | `/api/v1/issues/{issue_id}/deploy-links/{task_id}` | Unlink a deploy task |

A board is a stream of work and a project is a deployable service; a board's `project_id` is the optional bridge between the two. Boards and issues are returned whole rather than paged, because a kanban view has to place every card.

Create board:

```json
{
  "name": "Engineering",
  "key": "ENG",
  "description": "optional",
  "project_id": 1
}
```

`key` is 2-10 characters, starts with a letter, and is fixed after creation: it is baked into the issue identifiers, such as `ENG-42`, that commit messages reference. `PATCH` accepts `name`, `description`, `project_id` and `clear_project`.

Create issue:

```json
{
  "title": "Deploy log tail is empty",
  "description": "Markdown, may embed uploaded images",
  "status": "todo",
  "priority": "high",
  "assignee_id": 1,
  "labels": ["bug"]
}
```

Statuses are `backlog`, `todo`, `in_progress`, `done` and `canceled`; priorities are `urgent`, `high`, `medium`, `low` and `none`. An issue created or moved into `done` or `canceled` gets `completed_at` set.

`PATCH /issues/{issue_id}` writes only the fields present in the body: `title`, `description`, `status`, `priority`, `assignee_id`, `clear_assignee` and `labels`. A status change goes through the move path, so the card gets a valid position key in its new column.

`POST /issues/{issue_id}/move` takes the target column plus the two neighbours the card was dropped between:

```json
{
  "status": "in_progress",
  "after_id": 12,
  "before_id": 0
}
```

Neighbours rather than an index, so a board that changed underneath the client still resolves the drop to the slot the user aimed at. `0` means there is no neighbour on that side.

`POST /issues/{issue_id}/deploy-links` attaches a release by hand for the case the commit message did not name the issue:

```json
{ "task_id": 42 }
```

A manual link never closes the issue on its own. Links drawn from a commit message are stored with `source: "auto"`, and with `closing: true` when the message used a closing keyword; only those move an issue, and only once the deploy succeeds. Deleting a project removes the links to its deploy tasks and keeps the issues.

`DELETE /api/v1/boards/{board_id}` returns `409 Conflict` with `BOARD_HAS_ISSUES` while the board still has issues, so a board cannot silently discard work.

`GET /boards/{board_id}/issues` filters on `status`, `priority`, `assignee_id` (`none` selects the unassigned) and `q` (case-insensitive match on title and description).

## Attachments

| Method | Path | Description |
| --- | --- | --- |
| POST | `/api/v1/attachments` | Upload one image as `multipart/form-data`, file part named `file` |
| GET | `/api/v1/attachments/{attachment_id}` | Serve an uploaded image |

Attachments are the images pasted into an issue description. The issue is not known at upload time, so an upload stands alone until a saved description references its URL:

```json
{
  "data": {
    "id": 7,
    "filename": "screenshot.png",
    "content_type": "image/png",
    "size": 20480,
    "url": "/api/v1/attachments/7"
  },
  "request_id": "req_xxx"
}
```

The cap is 10 MiB, and only `image/png`, `image/jpeg`, `image/gif` and `image/webp` are accepted. SVG is refused because it can run script from this origin. The stored bytes are sniffed rather than trusted from the declared type, are served with `X-Content-Type-Options: nosniff` and `Content-Security-Policy: default-src 'none'; sandbox`, and are given a server-generated filename that never comes from the client. An upload no saved description references is swept 24 hours after it was made.

## Users

| Method | Path | Description |
| --- | --- | --- |
| GET | `/api/v1/users` | List users for the assignee picker: `id`, `username`, `role` |

## Webhook Events

| Method | Path | Description |
| --- | --- | --- |
| GET | `/api/v1/webhook-events` | List webhook deliveries |
| GET | `/api/v1/webhook-events/{event_id}` | Delivery detail |

## Webhook Callback

| Method | Path | Description |
| --- | --- | --- |
| POST | `/api/v1/webhooks/gitee/{project_key}` | Gitee callback |
| POST | `/api/v1/webhooks/github/{project_key}` | GitHub callback |

## Dashboard and Settings

| Method | Path | Description |
| --- | --- | --- |
| GET | `/api/v1/dashboard/summary` | Summary metrics |
| GET | `/api/v1/dashboard/recent-deploy-tasks` | Recent deployments |
| GET | `/api/v1/settings` | Runtime settings |
| PATCH | `/api/v1/settings` | Save non-runtime metadata settings |

Lists support `page`, `page_size`, `sort`, and resource-specific filters such as `project_id`, `status`, and `provider`.

`PATCH /settings` does not change live runtime configuration such as `jwt.secret`, `mcp.api_token`, deploy timeouts, or database settings. Runtime and secret settings are managed by environment variables, optional `config.yaml`, and generated `secrets.yaml`; changes require a backend restart.
