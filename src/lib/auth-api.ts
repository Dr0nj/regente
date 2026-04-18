import { api, isServerMode, setAuthToken } from "./server-client";

export type Role = "admin" | "operator" | "viewer";

export interface AuthUser {
  id: number;
  username: string;
  role: Role;
  createdAt?: string;
  mustChangePassword?: boolean;
}

export interface LoginResponse {
  token: string;
  user: AuthUser;
}

const LS_USER_KEY = "regente:user";

export function loadCachedUser(): AuthUser | null {
  if (typeof window === "undefined") return null;
  try {
    const raw = window.localStorage.getItem(LS_USER_KEY);
    return raw ? (JSON.parse(raw) as AuthUser) : null;
  } catch {
    return null;
  }
}

export function cacheUser(user: AuthUser | null): void {
  if (typeof window === "undefined") return;
  if (user) window.localStorage.setItem(LS_USER_KEY, JSON.stringify(user));
  else window.localStorage.removeItem(LS_USER_KEY);
}

export async function login(username: string, password: string): Promise<LoginResponse> {
  if (!isServerMode()) throw new Error("server mode disabled");
  const res = await api<LoginResponse>("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ username, password }),
  });
  setAuthToken(res.token);
  cacheUser(res.user);
  return res;
}

export async function logout(): Promise<void> {
  if (!isServerMode()) return;
  try { await api("/api/auth/logout", { method: "POST" }); } catch { /* ignore */ }
  setAuthToken(null);
  cacheUser(null);
}

export async function fetchMe(): Promise<AuthUser | null> {
  if (!isServerMode()) return null;
  try {
    const u = await api<AuthUser>("/api/auth/me");
    cacheUser(u);
    return u;
  } catch {
    return null;
  }
}

export async function changePassword(current: string, next: string): Promise<void> {
  await api("/api/auth/change-password", {
    method: "POST",
    body: JSON.stringify({ current, next }),
  });
}

export async function listUsers(): Promise<AuthUser[]> {
  return await api<AuthUser[]>("/api/users");
}

export async function createUser(username: string, password: string, role: Role): Promise<AuthUser> {
  return await api<AuthUser>("/api/users", {
    method: "POST",
    body: JSON.stringify({ username, password, role }),
  });
}

export async function updateUserRole(id: number, role: Role): Promise<void> {
  await api(`/api/users/${id}/role`, {
    method: "PATCH",
    body: JSON.stringify({ role }),
  });
}

export async function resetUserPassword(id: number, next: string): Promise<void> {
  await api(`/api/users/${id}/password`, {
    method: "PATCH",
    body: JSON.stringify({ next }),
  });
}

export async function deleteUser(id: number): Promise<void> {
  await api(`/api/users/${id}`, { method: "DELETE" });
}

// F11.10b — per-folder ACL
export interface FolderACL {
  userId: number;
  folder: string;
  perms: string; // "r" | "rw" | ""
}

export async function listUserACLs(userId: number): Promise<FolderACL[]> {
  return await api<FolderACL[]>(`/api/users/${userId}/acls`);
}

export async function setUserACL(userId: number, folder: string, perms: string): Promise<void> {
  await api(`/api/users/${userId}/acls/${encodeURIComponent(folder)}`, {
    method: "PATCH",
    body: JSON.stringify({ perms }),
  });
}

export async function replaceUserACLs(userId: number, acls: Array<{ folder: string; perms: string }>): Promise<FolderACL[]> {
  return await api<FolderACL[]>(`/api/users/${userId}/acls`, {
    method: "PUT",
    body: JSON.stringify(acls),
  });
}
