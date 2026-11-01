// API client for the agent plaza (R4): built-in and own agent definitions,
// immutable versions, and the frozen spec each run pins. The server contract
// lives in controller/agents.go.
import { showError } from '../../helpers/utils';

export type AgentMode = 'chat' | 'work';

export interface AgentVersionSpec {
  instructions: string;
  model: string;
  skill_ids: number[];
  file_ids: number[];
  sandbox_enabled: boolean;
  modes: AgentMode[];
  opening_hint: string;
  temperature?: number | null;
  top_p?: number | null;
  max_tokens?: number;
}

export interface AgentDefinition {
  id: number;
  owner_id: number; // 0 = built-in
  name: string;
  description: string;
  archived: boolean;
  current_version: number;
  created_time: number;
  updated_time: number;
}

export interface AgentVersionInfo {
  id: number;
  version: number;
  created_time: number;
}

export interface AgentView extends AgentDefinition {
  latest: AgentVersionInfo;
  spec: AgentVersionSpec;
}

export interface AgentPayload {
  name: string;
  description: string;
  instructions: string;
  model: string;
  skill_ids: number[];
  file_ids: number[];
  sandbox_enabled: boolean;
  modes: AgentMode[];
  opening_hint: string;
}

export interface ChatSkillLite {
  id: number;
  name: string;
  enabled: boolean;
}

export interface ChatFileLite {
  id: number;
  name: string;
  kind: 'text' | 'image';
  size: number;
}

export interface AgentDraft {
  name: string;
  description: string;
  instructions: string;
  opening_hint: string;
}

interface ApiResponse<T> {
  success: boolean;
  message: string;
  data: T;
}

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  let response: Response;
  try {
    response = await fetch(url, {
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      ...init,
    });
  } catch (error) {
    showError(error instanceof Error ? error : String(error));
    throw error;
  }
  let payload: ApiResponse<T>;
  try {
    payload = (await response.json()) as ApiResponse<T>;
  } catch {
    payload = { success: false, message: `HTTP ${response.status}`, data: null as T };
  }
  if (!payload.success) {
    const error = new Error(payload.message || `HTTP ${response.status}`);
    showError(error);
    throw error;
  }
  return payload.data;
}

export const agentsApi = {
  listPlaza: () =>
    request<{ builtin: AgentView[]; mine: AgentView[] }>('/api/chat/agents'),
  getDetail: (id: number) => request<AgentView>(`/api/chat/agents/${id}`),
  create: (payload: AgentPayload) =>
    request<AgentView>('/api/chat/agents', { method: 'POST', body: JSON.stringify(payload) }),
  createBuiltin: (payload: AgentPayload) =>
    request<AgentView>('/api/chat/agents/builtin', {
      method: 'POST',
      body: JSON.stringify(payload),
    }),
  update: (id: number, payload: AgentPayload) =>
    request<AgentView>(`/api/chat/agents/${id}`, { method: 'PUT', body: JSON.stringify(payload) }),
  setArchived: (id: number, archived: boolean) =>
    request<{ archived: boolean }>(`/api/chat/agents/${id}/archive`, {
      method: 'POST',
      body: JSON.stringify({ archived }),
    }),
  // The plaza binds existing chat skills; the list endpoint is shared with
  // the chat page but duplicated here to keep page boundaries clean.
  listSkills: () => request<ChatSkillLite[]>('/api/chat/skills'),
  listModels: () => request<string[]>('/api/user/available_models'),
  listFiles: () => request<ChatFileLite[]>('/api/chat/files'),
  duplicate: (id: number) =>
    request<AgentView>(`/api/chat/agents/${id}/duplicate`, { method: 'POST' }),
  draft: (description: string, model: string) =>
    request<AgentDraft>('/api/chat/agents/draft', {
      method: 'POST',
      body: JSON.stringify({ description, model }),
    }),
};
