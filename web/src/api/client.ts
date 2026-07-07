import { mockResponse } from "./mock";
import { useAuthStore } from "../store/auth";

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? "";
const MOCKS_ENABLED = import.meta.env.DEV && import.meta.env.VITE_ENABLE_MOCKS === "true";

export class APIError extends Error {
  status: number;

  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

function redirectToLoginOnUnauthorized() {
  const { logout } = useAuthStore.getState();
  logout();

  if (window.location.pathname === "/login") return;

  const currentPath = `${window.location.pathname}${window.location.search}${window.location.hash}`;
  const loginURL = new URL("/login", window.location.origin);
  if (currentPath !== "/") {
    loginURL.searchParams.set("next", currentPath);
  }
  window.location.assign(loginURL.toString());
}

export async function apiRequest<T>(path: string, init: RequestInit = {}, token?: string | null): Promise<T> {
  const headers = new Headers(init.headers);
  headers.set("Content-Type", "application/json");
  if (token) {
    headers.set("Authorization", `Bearer ${token}`);
  }
  try {
    const res = await fetch(`${API_BASE}${path}`, { ...init, headers });
    const json = await res.json().catch(() => ({}));
    if (!res.ok) {
      const message = json?.error?.message ?? `Request failed with ${res.status}`;
      if (res.status === 401) {
        redirectToLoginOnUnauthorized();
      }
      throw new APIError(res.status, message);
    }
    return json as T;
  } catch (error) {
    if (error instanceof APIError) throw error;
    if (MOCKS_ENABLED) {
      return mockResponse(path, init) as T;
    }
    throw error;
  }
}

export function withQuery(path: string, params: Record<string, string | number | undefined>) {
  const query = new URLSearchParams();
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== "") query.set(key, String(value));
  });
  const raw = query.toString();
  return raw ? `${path}?${raw}` : path;
}

export function streamURL(path: string, token?: string | null) {
  const url = new URL(`${API_BASE}${path}`, window.location.origin);
  if (token) url.searchParams.set("access_token", token);
  return url.toString();
}
