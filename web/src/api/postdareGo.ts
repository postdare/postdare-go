import { apiRequest, withQuery } from "./client";
import type { DashboardSummary, DataResponse, DeployTask, ListResponse, Project, Report, User, WebhookEvent } from "./types";

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
