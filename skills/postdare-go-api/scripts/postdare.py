#!/usr/bin/env python3
"""Postdare Go REST API client.

Standard library only: it has to run on a bare release host where nothing but
python3 is installed, which is the same place the deploy stages run.

Auth resolves in one order, and the difference matters operationally:

  1. POSTDARE_GO_API_TOKEN  -- the MCP token. Accepted on every /api/v1 route,
     but the backend treats the caller as the "mcp" actor, so deploy and
     rollback stay blocked unless mcp.allow_mutation_tools is true.
  2. POSTDARE_GO_USERNAME + POSTDARE_GO_PASSWORD -- exchanged at /auth/login for
     a JWT, cached on disk until it expires. A JWT is an ordinary user, so it is
     not subject to the mutation gate.

Every state-changing command refuses to run without --yes, because this client
triggers real deploys against real servers and a typo should cost nothing.
"""

from __future__ import annotations

import argparse
import getpass
import hashlib
import json
import os
import stat
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from datetime import datetime, timezone

DEFAULT_BASE_URL = "http://127.0.0.1:8088"
API_PREFIX = "/api/v1"
TERMINAL_STATUSES = {"success", "failed", "canceled", "rollbacked"}


class ApiError(Exception):
    """A non-2xx answer from the backend, carrying its error envelope."""

    def __init__(self, status, code, message, details=None, request_id=""):
        super().__init__(f"{status} {code}: {message}")
        self.status = status
        self.code = code
        self.message = message
        self.details = details
        self.request_id = request_id

    def hint(self):
        """Operational advice for the failures that are easy to misread."""
        hints = {
            "MCP_MUTATION_DISABLED": (
                "The MCP token cannot deploy or roll back while "
                "mcp.allow_mutation_tools is false. Set it to true and restart, "
                "or authenticate with POSTDARE_GO_USERNAME/POSTDARE_GO_PASSWORD."
            ),
            "PASSWORD_CHANGE_REQUIRED": (
                "This account still holds its generated initial password. "
                "Run: postdare.py passwd --yes"
            ),
            "PROJECT_DEPLOY_RUNNING": (
                "The project already has a pending/running task. Wait for it, or "
                "cancel it: postdare.py tasks cancel <task_id> --yes"
            ),
            "ROLLBACK_CMD_REQUIRED": "The project has an empty rollback_cmd.",
            "APP_LOG_NOT_CONFIGURED": "The project has an empty app_log_path.",
            "APP_LOG_NOT_FOUND": (
                "app_log_path must be absolute and resolve under the project's "
                "app_dir, and the postdare-go user must be able to read it."
            ),
            "SETTING_READ_ONLY": (
                "Runtime and secret settings live in config.yaml and need a "
                "restart. PATCH /settings only stores free-form metadata."
            ),
            "MCP_TOKEN_REQUIRED": "POST /mcp accepts the MCP token only, never a user JWT.",
        }
        return hints.get(self.code, "")


# --------------------------------------------------------------------------- #
# Token cache
# --------------------------------------------------------------------------- #

def _cache_path():
    base = os.environ.get("XDG_CACHE_HOME") or os.path.expanduser("~/.cache")
    return os.path.join(base, "postdare-go", "tokens.json")


def _cache_key(base_url, username):
    return hashlib.sha256(f"{base_url}|{username}".encode()).hexdigest()[:16]


def _read_cache():
    try:
        with open(_cache_path(), encoding="utf-8") as handle:
            return json.load(handle)
    except (OSError, ValueError):
        return {}


def _write_cache(cache):
    path = _cache_path()
    os.makedirs(os.path.dirname(path), mode=0o700, exist_ok=True)
    tmp = path + ".tmp"
    with open(tmp, "w", encoding="utf-8") as handle:
        json.dump(cache, handle)
    os.chmod(tmp, stat.S_IRUSR | stat.S_IWUSR)  # a JWT is a bearer credential
    os.replace(tmp, path)


# --------------------------------------------------------------------------- #
# Client
# --------------------------------------------------------------------------- #

class Client:
    def __init__(self, base_url, token=None, username=None, password=None, timeout=30):
        self.base_url = base_url.rstrip("/")
        self.timeout = timeout
        self.static_token = token
        self.username = username
        self.password = password
        self._jwt = None

    # -- auth ------------------------------------------------------------- #

    def token(self):
        """The bearer this client sends, logging in on demand."""
        if self.static_token:
            return self.static_token
        if self._jwt:
            return self._jwt
        cached = self._cached_jwt()
        if cached:
            self._jwt = cached
            return cached
        return self.login()

    def _cached_jwt(self):
        if not self.username:
            return None
        entry = _read_cache().get(_cache_key(self.base_url, self.username))
        if not entry:
            return None
        # 60s of slack so a token does not expire between this check and the call.
        if entry.get("expires_at", 0) - 60 <= time.time():
            return None
        return entry.get("token")

    def login(self):
        if not self.username or not self.password:
            raise SystemExit(
                "No credentials. Set POSTDARE_GO_API_TOKEN, or "
                "POSTDARE_GO_USERNAME and POSTDARE_GO_PASSWORD."
            )
        payload = {"username": self.username, "password": self.password}
        data = self._raw("POST", "/auth/login", body=payload, authenticate=False)["data"]
        self._jwt = data["token"]
        expires_at = _parse_time(data.get("expires_at"))
        cache = _read_cache()
        cache[_cache_key(self.base_url, self.username)] = {
            "token": self._jwt,
            "expires_at": expires_at or (time.time() + 3600),
        }
        _write_cache(cache)
        if data.get("user", {}).get("must_change_password"):
            print(
                "warning: this account must change its password before any other "
                "endpoint works (postdare.py passwd --yes)",
                file=sys.stderr,
            )
        return self._jwt

    def forget_token(self):
        self._jwt = None
        if not self.username:
            return
        cache = _read_cache()
        cache.pop(_cache_key(self.base_url, self.username), None)
        _write_cache(cache)

    # -- transport -------------------------------------------------------- #

    def request(self, method, path, params=None, body=None):
        try:
            return self._raw(method, path, params=params, body=body)
        except ApiError as err:
            # A cached JWT can outlive a restart that rotated jwt.secret; one
            # silent re-login is friendlier than telling the user to clear a cache.
            if err.status == 401 and not self.static_token and self.username:
                self.forget_token()
                self.login()
                return self._raw(method, path, params=params, body=body)
            raise

    def _raw(self, method, path, params=None, body=None, authenticate=True):
        url = self.base_url + API_PREFIX + path
        if params:
            clean = {k: v for k, v in params.items() if v is not None}
            if clean:
                url += "?" + urllib.parse.urlencode(clean, doseq=True)
        data = json.dumps(body).encode() if body is not None else None
        headers = {"Accept": "application/json"}
        if data is not None:
            headers["Content-Type"] = "application/json"
        if authenticate:
            headers["Authorization"] = "Bearer " + self.token()
        req = urllib.request.Request(url, data=data, headers=headers, method=method)
        try:
            with urllib.request.urlopen(req, timeout=self.timeout) as resp:
                raw = resp.read()
                if resp.status == 204 or not raw:
                    return {"data": None, "status": resp.status}
                return json.loads(raw)
        except urllib.error.HTTPError as err:
            raise _api_error(err) from None
        except urllib.error.URLError as err:
            raise SystemExit(f"cannot reach {self.base_url}: {err.reason}") from None

    def stream(self, path, params=None, on_line=print):
        """Consume one of the SSE /stream endpoints until interrupted.

        EventSource cannot set headers, so these routes accept the bearer as
        ?access_token= -- the same fallback the web UI uses.
        """
        params = dict(params or {})
        params["access_token"] = self.token()
        url = self.base_url + API_PREFIX + path + "?" + urllib.parse.urlencode(params)
        req = urllib.request.Request(url, headers={"Accept": "text/event-stream"})
        try:
            with urllib.request.urlopen(req, timeout=None) as resp:
                for raw in resp:
                    line = raw.decode("utf-8", "replace").rstrip("\n")
                    if line.startswith("data:"):
                        on_line(line[5:].lstrip())
        except urllib.error.HTTPError as err:
            raise _api_error(err) from None
        except KeyboardInterrupt:
            return

    # -- helpers ---------------------------------------------------------- #

    def paged(self, path, params=None, fetch_all=False):
        params = dict(params or {})
        first = self.request("GET", path, params=params)
        if not fetch_all:
            return first
        items = list(first.get("data") or [])
        pagination = first.get("pagination") or {}
        total = pagination.get("total", len(items))
        page_size = pagination.get("page_size", 20)
        page = pagination.get("page", 1)
        while len(items) < total:
            page += 1
            params.update({"page": page, "page_size": page_size})
            batch = self.request("GET", path, params=params).get("data") or []
            if not batch:
                break
            items.extend(batch)
        first["data"] = items
        return first

    def resolve_project(self, ref):
        """Accept a numeric id or a project_key, so scripts can use stable keys."""
        if str(ref).isdigit():
            return int(ref)
        projects = self.paged("/projects", {"page_size": 100}, fetch_all=True)["data"]
        for project in projects:
            if project.get("project_key") == ref:
                return project["id"]
        raise SystemExit(f"no project with project_key={ref!r}")


def _api_error(err):
    try:
        payload = json.loads(err.read())
    except (ValueError, OSError):
        payload = {}
    body = payload.get("error") or {}
    return ApiError(
        err.code,
        body.get("code", "HTTP_ERROR"),
        body.get("message", err.reason or "request failed"),
        body.get("details"),
        payload.get("request_id", ""),
    )


def _parse_time(value):
    if not value:
        return None
    text = str(value).replace("Z", "+00:00")
    try:
        parsed = datetime.fromisoformat(text)
    except ValueError:
        return None
    if parsed.tzinfo is None:
        parsed = parsed.replace(tzinfo=timezone.utc)
    return parsed.timestamp()


# --------------------------------------------------------------------------- #
# Output
# --------------------------------------------------------------------------- #

def emit(payload, args):
    if args.format == "json":
        print(json.dumps(payload, indent=2, ensure_ascii=False))
        return
    data = payload.get("data") if isinstance(payload, dict) else payload
    if isinstance(data, list):
        _table(data, _columns_for(args))
    else:
        print(json.dumps(data, indent=2, ensure_ascii=False))


def _columns_for(args):
    return {
        "projects": ["id", "project_key", "name", "git_provider", "branch", "auto_deploy_enabled"],
        "tasks": ["id", "project_id", "status", "trigger_type", "branch", "commit_id", "created_at"],
        "webhook-events": ["id", "provider", "project_key", "branch", "handled", "ignored_reason", "created_at"],
        "reports": ["id", "type", "task_id", "status", "conclusion", "share_enabled"],
    }.get(args.command)


def _table(rows, columns=None):
    if not rows:
        print("(empty)")
        return
    columns = columns or list(rows[0].keys())
    columns = [c for c in columns if any(c in row for row in rows)]
    cells = [[_cell(row.get(c)) for c in columns] for row in rows]
    widths = [max(len(c), *(len(r[i]) for r in cells)) for i, c in enumerate(columns)]
    print("  ".join(c.ljust(widths[i]) for i, c in enumerate(columns)))
    print("  ".join("-" * w for w in widths))
    for row in cells:
        print("  ".join(cell.ljust(widths[i]) for i, cell in enumerate(row)))


def _cell(value):
    if value is None:
        return "-"
    if isinstance(value, bool):
        return "yes" if value else "no"
    if isinstance(value, (dict, list)):
        return json.dumps(value, ensure_ascii=False)[:60]
    return str(value)[:60]


def require_yes(args, action):
    if not args.yes:
        raise SystemExit(f"refusing to {action} without --yes")


def load_json_arg(value):
    """Accept inline JSON or @path, so long stage pipelines live in a file."""
    if value.startswith("@"):
        with open(value[1:], encoding="utf-8") as handle:
            return json.load(handle)
    return json.loads(value)


# --------------------------------------------------------------------------- #
# Commands
# --------------------------------------------------------------------------- #

def cmd_version(client, args):
    emit(client.request("GET", "/version"), args)


def cmd_whoami(client, args):
    emit(client.request("GET", "/auth/me"), args)


def cmd_login(client, args):
    client.forget_token()
    client.login()
    emit(client.request("GET", "/auth/me"), args)


def cmd_passwd(client, args):
    require_yes(args, "change the password")
    old = args.old_password or client.password or getpass.getpass("current password: ")
    new = args.new_password or getpass.getpass("new password (>= 8 chars): ")
    if len(new) < 8:
        raise SystemExit("the new password must be at least 8 characters")
    emit(client.request("PUT", "/auth/password", body={"old_password": old, "new_password": new}), args)


def cmd_projects(client, args):
    if args.action == "list":
        emit(client.paged("/projects", {"page": args.page, "page_size": args.page_size,
                                        "provider": args.provider, "sort": args.sort},
                          fetch_all=args.all), args)
    elif args.action == "get":
        emit(client.request("GET", f"/projects/{client.resolve_project(args.project)}"), args)
    elif args.action == "create":
        require_yes(args, "create a project")
        emit(client.request("POST", "/projects", body=load_json_arg(args.data)), args)
    elif args.action == "update":
        require_yes(args, "update a project")
        pid = client.resolve_project(args.project)
        emit(client.request("PATCH", f"/projects/{pid}", body=load_json_arg(args.data)), args)
    elif args.action == "delete":
        require_yes(args, "delete a project")
        pid = client.resolve_project(args.project)
        project = client.request("GET", f"/projects/{pid}")["data"]
        # Deletion also drops the project's deploy tasks, stages and webhook
        # events, so make the caller name the project rather than an id they may
        # have copied from the wrong row.
        if args.confirm_key != project["project_key"]:
            raise SystemExit(
                f"pass --confirm-key {project['project_key']} to delete this project "
                "and all of its deploy tasks, stages and webhook events"
            )
        emit(client.request("DELETE", f"/projects/{pid}"), args)


def cmd_deploy(client, args):
    require_yes(args, "trigger a deploy")
    pid = client.resolve_project(args.project)
    # confirm=true is ignored for a JWT and required for the MCP token; sending
    # it always keeps one command working under both credentials.
    payload = client.request("POST", f"/projects/{pid}/deploy-tasks", body={"confirm": True})
    emit(payload, args)
    if args.watch:
        _watch(client, payload["data"]["id"], args)


def cmd_rollback(client, args):
    require_yes(args, "trigger a rollback")
    pid = client.resolve_project(args.project)
    payload = client.request("POST", f"/projects/{pid}/rollback-tasks", body={"confirm": True})
    emit(payload, args)
    if args.watch:
        _watch(client, payload["data"]["id"], args)


def cmd_tasks(client, args):
    if args.action == "list":
        project_id = client.resolve_project(args.project) if args.project else None
        emit(client.paged("/deploy-tasks", {"page": args.page, "page_size": args.page_size,
                                            "project_id": project_id, "status": args.status,
                                            "sort": args.sort}, fetch_all=args.all), args)
    elif args.action == "get":
        emit(client.request("GET", f"/deploy-tasks/{args.task_id}"), args)
    elif args.action == "stages":
        emit(client.request("GET", f"/deploy-tasks/{args.task_id}/stages"), args)
    elif args.action == "logs":
        if args.follow:
            client.stream(f"/deploy-tasks/{args.task_id}/logs/stream")
            return
        payload = client.request("GET", f"/deploy-tasks/{args.task_id}/logs", {"lines": args.lines})
        _emit_log(payload, args)
    elif args.action == "cancel":
        require_yes(args, "cancel a task")
        emit(client.request("POST", f"/deploy-tasks/{args.task_id}/cancel"), args)
    elif args.action == "analyze":
        emit(client.request("GET", f"/deploy-tasks/{args.task_id}/analysis"), args)
    elif args.action == "watch":
        _watch(client, args.task_id, args)


def _watch(client, task_id, args):
    """Poll a task to a terminal status, printing each stage transition.

    Polling rather than SSE: the log stream carries output, not status, and the
    thing a caller waits on is whether the release succeeded.
    """
    deadline = time.time() + args.timeout
    last = None
    while True:
        task = client.request("GET", f"/deploy-tasks/{task_id}")["data"]
        marker = (task.get("status"), task.get("current_stage"))
        if marker != last:
            print(f"[{_now()}] status={marker[0]} stage={marker[1] or '-'}", file=sys.stderr)
            last = marker
        if task.get("status") in TERMINAL_STATUSES:
            emit({"data": task}, args)
            if task["status"] in {"failed", "canceled"}:
                if task.get("fail_reason"):
                    print(f"fail_reason: {task['fail_reason']}", file=sys.stderr)
                raise SystemExit(1)
            return
        if time.time() > deadline:
            raise SystemExit(f"task {task_id} still {task.get('status')} after {args.timeout}s")
        time.sleep(args.interval)


def cmd_app_logs(client, args):
    pid = client.resolve_project(args.project)
    if args.follow:
        client.stream(f"/projects/{pid}/app-logs/stream")
        return
    payload = client.request("GET", f"/projects/{pid}/app-logs", {"lines": args.lines})
    _emit_log(payload, args)


def _emit_log(payload, args):
    """Log tails read better raw; --format json keeps the envelope for scripts."""
    if args.format == "json":
        emit(payload, args)
    else:
        print(payload["data"]["log"], end="")


def cmd_reports(client, args):
    if args.action == "list":
        emit(client.request("GET", f"/deploy-tasks/{args.task_id}/reports"), args)
    elif args.action == "get":
        emit(client.request("GET", f"/reports/{args.report_id}"), args)
    elif args.action == "share":
        require_yes(args, "mint a public share link")
        # Sharing rotates the token: links handed out earlier stop working.
        emit(client.request("POST", f"/reports/{args.report_id}/share"), args)
    elif args.action == "unshare":
        require_yes(args, "revoke a share link")
        emit(client.request("DELETE", f"/reports/{args.report_id}/share"), args)


def cmd_webhook_events(client, args):
    if args.action == "list":
        project_id = client.resolve_project(args.project) if args.project else None
        emit(client.paged("/webhook-events", {"page": args.page, "page_size": args.page_size,
                                              "project_id": project_id, "provider": args.provider},
                          fetch_all=args.all), args)
    else:
        emit(client.request("GET", f"/webhook-events/{args.event_id}"), args)


def cmd_dashboard(client, args):
    if args.action == "summary":
        emit(client.request("GET", "/dashboard/summary"), args)
    else:
        emit(client.request("GET", "/dashboard/recent-deploy-tasks", {"limit": args.limit}), args)


def cmd_settings(client, args):
    if args.action == "get":
        emit(client.request("GET", "/settings"), args)
    else:
        require_yes(args, "write settings")
        emit(client.request("PATCH", "/settings", body=load_json_arg(args.data)), args)


def _now():
    return datetime.now().strftime("%H:%M:%S")


# --------------------------------------------------------------------------- #
# CLI
# --------------------------------------------------------------------------- #

def build_parser():
    # The global flags are repeated on every subparser with SUPPRESS defaults so
    # that "projects create ... --yes" works as naturally as "--yes projects
    # create ...". SUPPRESS matters: a plain default would let the subparser
    # overwrite a value the top-level parser already took.
    common = argparse.ArgumentParser(add_help=False)
    common.add_argument("--base-url", default=argparse.SUPPRESS)
    common.add_argument("--format", choices=["json", "table", "text"], default=argparse.SUPPRESS)
    common.add_argument("--yes", action="store_true", default=argparse.SUPPRESS,
                        help="required for every state-changing command")

    parser = argparse.ArgumentParser(
        prog="postdare.py",
        description="Postdare Go REST API client (stdlib only).",
        epilog="Auth: POSTDARE_GO_API_TOKEN, or POSTDARE_GO_USERNAME + POSTDARE_GO_PASSWORD. "
               "Base URL: POSTDARE_GO_BASE_URL (default %s)." % DEFAULT_BASE_URL,
    )
    parser.add_argument("--base-url", default=os.environ.get("POSTDARE_GO_BASE_URL", DEFAULT_BASE_URL))
    parser.add_argument("--format", choices=["json", "table", "text"], default="json")
    parser.add_argument("--yes", action="store_true", help="required for every state-changing command")
    parser.add_argument("--timeout-seconds", type=int, default=30, dest="http_timeout")
    sub = parser.add_subparsers(dest="command", required=True)

    def group(name, **kwargs):
        parser_ = sub.add_parser(name, parents=[common], **kwargs)
        return parser_, parser_.add_subparsers(dest="action", required=True)

    def leaf(group_sub, name, **kwargs):
        return group_sub.add_parser(name, parents=[common], **kwargs)

    def paging(p):
        p.add_argument("--page", type=int, default=None)
        p.add_argument("--page-size", type=int, default=None)
        p.add_argument("--all", action="store_true", help="follow pagination to the end")

    def watch_flags(p):
        p.add_argument("--interval", type=int, default=5, help="seconds between polls")
        p.add_argument("--timeout", type=int, default=1800, help="give up after N seconds")

    sub.add_parser("version", parents=[common]).set_defaults(func=cmd_version)
    sub.add_parser("whoami", parents=[common], help="show the identity behind the current credentials").set_defaults(func=cmd_whoami)
    sub.add_parser("login", parents=[common], help="force a fresh login and refresh the cached JWT").set_defaults(func=cmd_login)

    passwd = sub.add_parser("passwd", parents=[common], help="change the current user's password")
    passwd.add_argument("--old-password")
    passwd.add_argument("--new-password")
    passwd.set_defaults(func=cmd_passwd)

    projects, projects_sub = group("projects", help="list, inspect and edit projects")
    p_list = leaf(projects_sub, "list")
    p_list.add_argument("--provider", choices=["gitee", "github"])
    p_list.add_argument("--sort", help="e.g. created_at:desc, name:asc")
    paging(p_list)
    leaf(projects_sub, "get").add_argument("project", help="id or project_key")
    leaf(projects_sub, "create").add_argument("data", help="JSON object or @file.json")
    p_update = leaf(projects_sub, "update")
    p_update.add_argument("project", help="id or project_key")
    p_update.add_argument("data", help="JSON object or @file.json")
    p_delete = leaf(projects_sub, "delete", help="delete a project and its tasks, stages and webhook events")
    p_delete.add_argument("project", help="id or project_key")
    p_delete.add_argument("--confirm-key", required=True, help="repeat the project_key to confirm")
    projects.set_defaults(func=cmd_projects)

    deploy = sub.add_parser("deploy", parents=[common], help="trigger a manual deploy")
    deploy.add_argument("project", help="id or project_key")
    deploy.add_argument("--watch", action="store_true", help="poll until the task reaches a final status")
    watch_flags(deploy)
    deploy.set_defaults(func=cmd_deploy)

    rollback = sub.add_parser("rollback", parents=[common], help="run the project's rollback_cmd")
    rollback.add_argument("project", help="id or project_key")
    rollback.add_argument("--watch", action="store_true")
    watch_flags(rollback)
    rollback.set_defaults(func=cmd_rollback)

    tasks, tasks_sub = group("tasks", help="deploy tasks, stages, logs and failure analysis")
    t_list = leaf(tasks_sub, "list")
    t_list.add_argument("--project", help="id or project_key")
    t_list.add_argument("--status", choices=sorted(TERMINAL_STATUSES | {"pending", "running"}))
    t_list.add_argument("--sort", help="e.g. created_at:desc, finished_at:asc")
    paging(t_list)
    leaf(tasks_sub, "get").add_argument("task_id")
    leaf(tasks_sub, "stages").add_argument("task_id")
    t_logs = leaf(tasks_sub, "logs")
    t_logs.add_argument("task_id")
    t_logs.add_argument("--lines", type=int, default=500, help="server caps this at app_log.max_allowed_lines")
    t_logs.add_argument("--follow", action="store_true", help="stream over SSE instead of tailing once")
    leaf(tasks_sub, "cancel").add_argument("task_id")
    leaf(tasks_sub, "analyze").add_argument("task_id")
    t_watch = leaf(tasks_sub, "watch", help="poll a task to its final status")
    t_watch.add_argument("task_id")
    watch_flags(t_watch)
    tasks.set_defaults(func=cmd_tasks)

    app_logs = sub.add_parser("app-logs", parents=[common], help="tail a project's configured app_log_path")
    app_logs.add_argument("project", help="id or project_key")
    app_logs.add_argument("--lines", type=int, default=500)
    app_logs.add_argument("--follow", action="store_true")
    app_logs.set_defaults(func=cmd_app_logs)

    reports, reports_sub = group("reports", help="reports captured by a stage (capture_as)")
    leaf(reports_sub, "list").add_argument("task_id")
    leaf(reports_sub, "get").add_argument("report_id")
    leaf(reports_sub, "share", help="mint or rotate a public link; older links stop working").add_argument("report_id")
    leaf(reports_sub, "unshare").add_argument("report_id")
    reports.set_defaults(func=cmd_reports)

    events, events_sub = group("webhook-events", help="inspect webhook deliveries and why they were ignored")
    e_list = leaf(events_sub, "list")
    e_list.add_argument("--project", help="id or project_key")
    e_list.add_argument("--provider", choices=["gitee", "github"])
    paging(e_list)
    leaf(events_sub, "get").add_argument("event_id")
    events.set_defaults(func=cmd_webhook_events)

    dashboard, dashboard_sub = group("dashboard")
    leaf(dashboard_sub, "summary")
    leaf(dashboard_sub, "recent").add_argument("--limit", type=int, default=10)
    dashboard.set_defaults(func=cmd_dashboard)

    settings, settings_sub = group("settings", help="read runtime config; write free-form metadata only")
    leaf(settings_sub, "get")
    leaf(settings_sub, "set").add_argument("data", help="JSON object of string values, or @file.json")
    settings.set_defaults(func=cmd_settings)

    return parser


def main(argv=None):
    args = build_parser().parse_args(argv)
    client = Client(
        base_url=args.base_url,
        token=os.environ.get("POSTDARE_GO_API_TOKEN"),
        username=os.environ.get("POSTDARE_GO_USERNAME"),
        password=os.environ.get("POSTDARE_GO_PASSWORD"),
        timeout=args.http_timeout,
    )
    try:
        args.func(client, args)
    except ApiError as err:
        print(f"error: {err}", file=sys.stderr)
        if err.details:
            print(f"details: {json.dumps(err.details, ensure_ascii=False)}", file=sys.stderr)
        if err.request_id:
            print(f"request_id: {err.request_id}", file=sys.stderr)
        if err.hint():
            print(f"hint: {err.hint()}", file=sys.stderr)
        return 2
    except BrokenPipeError:
        return 0
    return 0


if __name__ == "__main__":
    sys.exit(main())
