// API client for the studio account area: API keys (token CRUD), personal
// usage, profile settings, and redemption-code top-up. The server contracts
// live in controller/token.go, controller/log.go, and controller/user.go;
// the endpoints already exist — this client just exposes them to the
// user-facing studio.

import { studioRequest as request } from '@/helpers/studioRequest';

export interface TokenView {
  id: number;
  name: string;
  status: number;
  key: string;
  created_time: number;
  accessed_time: number;
  expired_time: number;
  remain_quota: number;
  unlimited_quota: boolean;
  used_quota: number;
  models: string | null;
}

export interface UserSelf {
  id: number;
  username: string;
  display_name: string;
  email: string;
  quota: number;
  used_quota: number;
  request_count: number;
  group: string;
  access_token: string;
}

export interface LogStatistic {
  day: string;
  model_name: string;
  request_count: number;
  quota: number;
  prompt_tokens: number;
  completion_tokens: number;
}

export interface UsageLog {
  id: number;
  created_at: number;
  token_name: string;
  model_name: string;
  prompt_tokens: number;
  completion_tokens: number;
  quota: number;
}

export const TOKEN_STATUS_ENABLED = 1;
export const TOKEN_STATUS_DISABLED = 2;
export const TOKEN_STATUS_EXPIRED = 3;
export const TOKEN_STATUS_EXHAUSTED = 4;

// Keys are stored bare and every client prefixes them with sk-.
export const displayKey = (key: string) => `sk-${key}`;

const get = <T,>(url: string) => request<T>(url);
const post = <T,>(url: string, body: unknown) =>
  request<T>(url, { method: 'POST', body: JSON.stringify(body) });
const put = <T,>(url: string, body: unknown, query = '') =>
  request<T>(url + query, { method: 'PUT', body: JSON.stringify(body) });
const del = <T,>(url: string) => request<T>(url, { method: 'DELETE' });

export interface TokenPayload {
  name: string;
  expired_time: number;
  remain_quota: number;
  unlimited_quota: boolean;
  models: string | null;
}

export const accountApi = {
  // API keys
  listTokens: () => get<TokenView[]>('/api/token/'),
  createToken: (payload: TokenPayload) => post<TokenView>('/api/token/', payload),
  updateToken: (payload: TokenPayload & { id: number }) => put<TokenView>('/api/token/', payload),
  setTokenStatus: (token: { id: number; status: number }) =>
    put<boolean>('/api/token/', token, '?status_only=true'),
  deleteToken: (id: number) => del<boolean>(`/api/token/${id}`),

  // Profile
  getSelf: () => get<UserSelf>('/api/user/self'),
  updateSelf: (payload: { username: string; display_name: string; password?: string }) =>
    put<boolean>('/api/user/self', payload),
  generateAccessToken: () => get<string>('/api/user/token'),

  // Usage
  getDashboard: () => get<LogStatistic[]>('/api/user/dashboard'),
  getUsedQuota: () =>
    get<{ quota: number }>('/api/log/self/stat'),
  listLogs: (page: number) => get<UsageLog[]>(`/api/log/self?p=${page}`),

  // Redeem
  redeem: (key: string) => post<number>('/api/user/topup', { key }),
};
