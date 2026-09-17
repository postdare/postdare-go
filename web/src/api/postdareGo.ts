import { apiRequest, withQuery } from "./client";
import type {
  Board,
  BoardUser,
  DashboardSummary,
  DataResponse,
  DeployTask,
  Issue,
  IssueComment,
  IssueMetadata,
  IssueStatus,
  ListResponse,
  Project,
  Report,
  User,
  WebhookEvent
} from "./types";

export function login(username: string, password: string) {
  return apiRequest<DataResponse<{ token: string; user: User }>>("/api/v1/auth/login", {
    method: "POST",
    body: JSON.stringify({ username, password })
  });
}

export function changePassword(oldPassword: string, newPassword: string, token?: string | null) {
  return apiRequest<DataResponse<{ ok: boolean }>>(
    "/api/v1/auth/password",
    {
      method: "PUT",
      body: JSON.stringify({ old_password: oldPassword, new_password: newPassword })
    },
    token
  );
}

export function dashboardSummary(token?: string | null) {
  return apiRequest<DataResponse<DashboardSummary>>("/api/v1/dashboard/summary", {}, token);
}

export function recentDeployTasks(token?: string | null) {
  return apiRequest<DataResponse<DeployTask[]>>("/api/v1/dashboard/recent-deploy-tasks", {}, token);
}

export function listProjects(token?: string | null) {
  return apiRequest<ListResponse<Project>>("/api/v1/projects", {}, token);
}

export function getProject(id: string | number, token?: string | null) {
  return apiRequest<DataResponse<Project>>(`/api/v1/projects/${id}`, {}, token);
}

export function createProject(payload: Partial<Project>, token?: string | null) {
  return apiRequest<DataResponse<Project>>("/api/v1/projects", { method: "POST", body: JSON.stringify(payload) }, token);
}

export function updateProject(id: string | number, payload: Partial<Project>, token?: string | null) {
  return apiRequest<DataResponse<Project>>(`/api/v1/projects/${id}`, { method: "PATCH", body: JSON.stringify(payload) }, token);
}

export function deleteProject(id: string | number, token?: string | null) {
  return apiRequest<void>(`/api/v1/projects/${id}`, { method: "DELETE" }, token);
}

export function triggerDeploy(id: string | number, token?: string | null) {
  return apiRequest<DataResponse<DeployTask>>(`/api/v1/projects/${id}/deploy-tasks`, { method: "POST", body: JSON.stringify({}) }, token);
}

export function triggerRollback(id: string | number, token?: string | null) {
  return apiRequest<DataResponse<DeployTask>>(`/api/v1/projects/${id}/rollback-tasks`, { method: "POST", body: JSON.stringify({}) }, token);
}

export function listDeployTasks(token?: string | null, params: Record<string, string | number | undefined> = {}) {
  return apiRequest<ListResponse<DeployTask>>(withQuery("/api/v1/deploy-tasks", params), {}, token);
}

export function getDeployTask(id: string | number, token?: string | null) {
  return apiRequest<DataResponse<DeployTask>>(`/api/v1/deploy-tasks/${id}`, {}, token);
}

export function listDeployTaskReports(id: string | number, token?: string | null) {
  return apiRequest<DataResponse<Report[]>>(`/api/v1/deploy-tasks/${id}/reports`, {}, token);
}

export function getReport(id: string | number, token?: string | null) {
  return apiRequest<DataResponse<Report>>(`/api/v1/reports/${id}`, {}, token);
}

export function getPublicReport(id: string | number, reportToken: string) {
  return apiRequest<DataResponse<Report>>(`/api/v1/public/reports/${id}`, { headers: { "X-Report-Token": reportToken } });
}

export function getDeployLog(id: string | number, token?: string | null, lines = 500) {
  return apiRequest<DataResponse<{ log: string }>>(`/api/v1/deploy-tasks/${id}/logs?lines=${lines}`, {}, token);
}

export function getAppLog(projectID: string | number, token?: string | null, lines = 500) {
  return apiRequest<DataResponse<{ log: string }>>(`/api/v1/projects/${projectID}/app-logs?lines=${lines}`, {}, token);
}

export function listWebhookEvents(token?: string | null) {
  return apiRequest<ListResponse<WebhookEvent>>("/api/v1/webhook-events", {}, token);
}

export function getSettings(token?: string | null) {
  return apiRequest<DataResponse<Record<string, unknown>>>("/api/v1/settings", {}, token);
}

export function listBoards(token?: string | null, params: { archived?: boolean } = {}) {
  const path = params.archived ? withQuery("/api/v1/boards", { archived: "true" }) : "/api/v1/boards";
  return apiRequest<DataResponse<Board[]>>(path, {}, token);
}

export function getBoard(id: string | number, token?: string | null) {
  return apiRequest<DataResponse<Board>>(`/api/v1/boards/${id}`, {}, token);
}

export function createBoard(payload: { name: string; key: string; description?: string; project_id?: number | null }, token?: string | null) {
  return apiRequest<DataResponse<Board>>("/api/v1/boards", { method: "POST", body: JSON.stringify(payload) }, token);
}

export function updateBoard(id: string | number, payload: Record<string, unknown>, token?: string | null) {
  return apiRequest<DataResponse<Board>>(`/api/v1/boards/${id}`, { method: "PATCH", body: JSON.stringify(payload) }, token);
}

export function deleteBoard(id: string | number, token?: string | null) {
  return apiRequest<void>(`/api/v1/boards/${id}`, { method: "DELETE" }, token);
}

export function listBoardIssues(id: string | number, token?: string | null, params: Record<string, string | number | undefined> = {}) {
  return apiRequest<DataResponse<Issue[]>>(withQuery(`/api/v1/boards/${id}/issues`, params), {}, token);
}

export function createIssue(boardID: string | number, payload: Record<string, unknown>, token?: string | null) {
  return apiRequest<DataResponse<Issue>>(`/api/v1/boards/${boardID}/issues`, { method: "POST", body: JSON.stringify(payload) }, token);
}

export function getIssue(id: string | number, token?: string | null) {
  return apiRequest<DataResponse<Issue>>(`/api/v1/issues/${id}`, {}, token);
}

/** `init` carries the odd request-level flag the autosave needs -- keepalive,
 *  so an edit sent as the page closes is not cancelled with the document. */
export function updateIssue(
  id: string | number,
  payload: Record<string, unknown>,
  token?: string | null,
  init: RequestInit = {}
) {
  return apiRequest<DataResponse<Issue>>(
    `/api/v1/issues/${id}`,
    { ...init, method: "PATCH", body: JSON.stringify(payload) },
    token
  );
}

export function deleteIssue(id: string | number, token?: string | null) {
  return apiRequest<void>(`/api/v1/issues/${id}`, { method: "DELETE" }, token);
}

/** Places a dragged card by naming the cards it was dropped between, so the
 *  server resolves the drop even if the board changed while it was in flight. */
export function moveIssue(
  id: string | number,
  payload: { status: IssueStatus; after_id?: number; before_id?: number },
  token?: string | null
) {
  return apiRequest<DataResponse<Issue>>(`/api/v1/issues/${id}/move`, { method: "POST", body: JSON.stringify(payload) }, token);
}

export function listBoardUsers(token?: string | null) {
  return apiRequest<DataResponse<BoardUser[]>>("/api/v1/users", {}, token);
}

export function listIssueComments(issueID: string | number, token?: string | null) {
  return apiRequest<DataResponse<IssueComment[]>>(`/api/v1/issues/${issueID}/comments`, {}, token);
}

export function createIssueComment(issueID: string | number, body: string, token?: string | null) {
  return apiRequest<DataResponse<IssueComment>>(
    `/api/v1/issues/${issueID}/comments`,
    { method: "POST", body: JSON.stringify({ body }) },
    token
  );
}

export function updateIssueComment(commentID: string | number, body: string, token?: string | null) {
  return apiRequest<DataResponse<IssueComment>>(
    `/api/v1/issue-comments/${commentID}`,
    { method: "PATCH", body: JSON.stringify({ body }) },
    token
  );
}

export function deleteIssueComment(commentID: string | number, token?: string | null) {
  return apiRequest<void>(`/api/v1/issue-comments/${commentID}`, { method: "DELETE" }, token);
}

export function issueMetadata(token?: string | null) {
  return apiRequest<DataResponse<IssueMetadata>>("/api/v1/issues/meta", {}, token);
}

export interface UploadedAttachment {
  id: number;
  filename: string;
  content_type: string;
  size: number;
  /** The markdown src for the image, e.g. /api/v1/attachments/12. */
  url: string;
}

/** Uploads one image. multipart, so it does not go through apiRequest, which
 *  sets a JSON content type. */
export async function uploadAttachment(file: File, token?: string | null) {
  const body = new FormData();
  body.append("file", file);
  const headers = new Headers();
  if (token) headers.set("Authorization", `Bearer ${token}`);
  const res = await fetch("/api/v1/attachments", { method: "POST", body, headers });
  const json = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(json?.error?.message ?? `Upload failed with ${res.status}`);
  return (json as DataResponse<UploadedAttachment>).data;
}

/** Fetches an attachment as a blob. Attachments are behind the same auth as
 *  everything else, and an <img src> cannot carry an Authorization header, so
 *  the bytes are fetched here and handed to the tag as an object URL rather
 *  than putting a token into the markdown where it would be saved and shared. */
export async function fetchAttachmentBlob(url: string, token?: string | null) {
  const headers = new Headers();
  if (token) headers.set("Authorization", `Bearer ${token}`);
  const res = await fetch(url, { headers });
  if (!res.ok) throw new Error(`Attachment failed to load (${res.status})`);
  return res.blob();
}

export function listBoardLabels(id: string | number, token?: string | null) {
  return apiRequest<DataResponse<string[]>>(`/api/v1/boards/${id}/labels`, {}, token);
}
