// API client for the built-in chat runtime: REST calls plus the streaming
// turn endpoint parsed from fetch's ReadableStream (POST cannot use
// EventSource).
import { showError } from '../../helpers';

export interface ChatSession {
  id: number;
  title: string;
  model: string;
  system_prompt: string;
  temperature: number | null;
  top_p: number | null;
  max_tokens: number;
  sandbox_enabled: boolean;
  context_budget: number;
  skill_ids: number[] | null;
  updated_time: number;
}

export interface ChatMessage {
  id: number;
  session_id: number;
  role: 'user' | 'assistant' | 'tool';
  content: string;
  parts?: string | null;
  tool_calls?: string | null;
  tool_call_id?: string;
  name?: string;
  created_time: number;
}

export interface ChatFile {
  id: number;
  name: string;
  kind: 'text' | 'image';
  size: number;
}

export interface ChatSkill {
  id: number;
  owner_id: number;
  name: string;
  description: string;
  instructions: string;
  tools: string[] | null;
  enabled: boolean;
}

export interface ChatMCPServer {
  id: number;
  owner_id: number;
  name: string;
  transport: 'stdio' | 'http';
  command: string;
  args: string[] | null;
  env: Record<string, string> | null;
  url: string;
  headers: Record<string, string> | null;
  enabled: boolean;
}

export interface ChatBootstrap {
  sandbox_available: boolean;
  sandbox_enabled: boolean;
  budget: number;
  max_tool_iterations: number;
  max_file_bytes: number;
  max_text_file_bytes: number;
}

export interface ChatEvent {
  type: string;
  message_id?: number;
  delta?: string;
  tool_call_id?: string;
  name?: string;
  arguments?: string;
  content?: string;
  is_error?: boolean;
  prompt_tokens?: number;
  completion_tokens?: number;
  error?: string;
}

interface ApiResponse<T> {
  success: boolean;
  message?: string;
  data: T;
}

// request uses fetch because the axios instance's response interceptor is
// the only other error toaster and streaming POSTs cannot go through it, so
// request surfaces failures itself: every empty catch on the call sites can
// rely on the toast having already been shown.
async function request<T>(url: string, init?: RequestInit): Promise<T> {
  let response: Response;
  try {
    response = await fetch(url, {
      credentials: 'same-origin',
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
    // A proxy or error page answered with non-JSON content.
    payload = { success: false, message: `HTTP ${response.status}`, data: null as T };
  }
  if (!payload.success) {
    const error = new Error(payload.message || `HTTP ${response.status}`);
    showError(error);
    throw error;
  }
  return payload.data;
}

export const chatApi = {
  bootstrap: () => request<ChatBootstrap>('/api/chat/bootstrap'),
  listSessions: () => request<ChatSession[]>('/api/chat/sessions'),
  createSession: (session: Partial<ChatSession>) =>
    request<ChatSession>('/api/chat/sessions', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(session),
    }),
  updateSession: (id: number, session: Partial<ChatSession>) =>
    request<ChatSession>(`/api/chat/sessions/${id}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(session),
    }),
  deleteSession: (id: number) =>
    request<void>(`/api/chat/sessions/${id}`, { method: 'DELETE' }),
  listMessages: (id: number) =>
    request<ChatMessage[]>(`/api/chat/sessions/${id}/messages`),
  listFiles: () => request<ChatFile[]>('/api/chat/files'),
  listSkills: () => request<ChatSkill[]>('/api/chat/skills'),
  saveSkill: (skill: Partial<ChatSkill> & { id?: number }) =>
    skill.id
      ? request<ChatSkill>(`/api/chat/skills/${skill.id}`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(skill),
        })
      : request<ChatSkill>('/api/chat/skills', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(skill),
        }),
  deleteSkill: (id: number) =>
    request<void>(`/api/chat/skills/${id}`, { method: 'DELETE' }),
  listUserMCPServers: () => request<ChatMCPServer[]>('/api/chat/mcp/user'),
  saveUserMCPServer: (server: Partial<ChatMCPServer> & { id?: number }) =>
    server.id
      ? request<ChatMCPServer>(`/api/chat/mcp/user/${server.id}`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(server),
        })
      : request<ChatMCPServer>('/api/chat/mcp/user', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(server),
        }),
  deleteUserMCPServer: (id: number) =>
    request<void>(`/api/chat/mcp/user/${id}`, { method: 'DELETE' }),
  adminListMCPServers: () => request<ChatMCPServer[]>('/api/chat/mcp'),
  adminSaveMCPServer: (server: Partial<ChatMCPServer> & { id?: number }) =>
    server.id
      ? request<ChatMCPServer>(`/api/chat/mcp/${server.id}`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(server),
        })
      : request<ChatMCPServer>('/api/chat/mcp', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(server),
        }),
  adminDeleteMCPServer: (id: number) =>
    request<void>(`/api/chat/mcp/${id}`, { method: 'DELETE' }),
  adminListSkills: () => request<ChatSkill[]>('/api/chat/skills/global'),
  adminSaveSkill: (skill: Partial<ChatSkill> & { id?: number }) =>
    skill.id
      ? request<ChatSkill>(`/api/chat/skills/global/${skill.id}`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(skill),
        })
      : request<ChatSkill>('/api/chat/skills/global', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(skill),
        }),
  adminDeleteSkill: (id: number) =>
    request<void>(`/api/chat/skills/global/${id}`, { method: 'DELETE' }),
  listModels: () => request<string[]>('/api/user/available_models'),
  uploadFile: async (file: File): Promise<ChatFile> => {
    const form = new FormData();
    form.append('file', file);
    return request<ChatFile>('/api/chat/files', { method: 'POST', body: form });
  },
};

// runTurn posts one message and invokes onEvent for every SSE payload. The
// abort signal doubles as the stop button and unmount cleanup.
export async function runTurn(
  sessionId: number,
  content: string,
  fileIds: number[],
  onEvent: (event: ChatEvent) => void,
  signal: AbortSignal
): Promise<void> {
  const response = await fetch(`/api/chat/sessions/${sessionId}/run`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ content, file_ids: fileIds }),
    signal,
  });
  if (!response.ok || !response.body) {
    let message = `HTTP ${response.status}`;
    try {
      const payload = await response.json();
      if (payload.message) {
        message = payload.message;
      }
    } catch {
      // keep the status-line message
    }
    throw new Error(message);
  }
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  for (;;) {
    const { done, value } = await reader.read();
    if (done) {
      break;
    }
    buffer += decoder.decode(value, { stream: true });
    let boundary = buffer.indexOf('\n');
    while (boundary >= 0) {
      const line = buffer.slice(0, boundary).replace(/\r$/, '');
      buffer = buffer.slice(boundary + 1);
      if (line.startsWith('data: ')) {
        const payload = line.slice(6).trim();
        if (payload) {
          try {
            onEvent(JSON.parse(payload) as ChatEvent);
          } catch {
            // ignore malformed keep-alive fragments
          }
        }
      }
      boundary = buffer.indexOf('\n');
    }
  }
}
