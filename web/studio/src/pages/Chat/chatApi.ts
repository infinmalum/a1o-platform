// API client for the built-in chat runtime: REST calls plus the streaming
// turn endpoint parsed from fetch's ReadableStream (POST cannot use
// EventSource). Logic mirrors the admin console's copy; the UI around it is
// the Studio rewrite.
import { studioRequest as request, studioFetch, requestError } from '@/helpers/studioRequest';
import i18n from '@/i18n';
import type { KnowledgeSource } from '@/pages/Workspace/knowledgeApi';

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
  agent_version_id: number;
  agent_skill_ids?: number[] | null;
  knowledge_library_id?: number;
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
  sources?: KnowledgeSource[];
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

// RunEvent is one versioned agent run event streamed during a turn; the
// payload fields depend on the event type.
export interface ChatEvent {
  run_id?: number;
  sequence?: number;
  type: string;
  invocation_id?: number;
  payload?: {
    session_id?: number;
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
  };
}

export const chatApi = {
  bootstrap: () => request<ChatBootstrap>('/api/chat/bootstrap'),
  listSessions: () => request<ChatSession[]>('/api/chat/sessions'),
  getSession: (id: number) =>
    request<ChatSession>(`/api/chat/sessions/${id}`, undefined, false),
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
  branch: (id: number, messageId: number, title: string) =>
    request<{ session: ChatSession; message_id: number }>(`/api/chat/sessions/${id}/branches`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ message_id: messageId, title }),
    }, false),
  listMessages: (id: number) =>
    request<ChatMessage[]>(`/api/chat/sessions/${id}/messages`),
  listFiles: () => request<ChatFile[]>('/api/chat/files'),
  listSkills: () => request<ChatSkill[]>('/api/chat/skills'),
  previewSkillMarkdown: (body: File) =>
    request<Pick<ChatSkill, 'name' | 'description' | 'instructions'>>('/api/chat/skills/preview', {
      method: 'POST',
      headers: { 'Content-Type': 'text/markdown; charset=utf-8' },
      body,
    }),
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
  skillIds: number[],
  onEvent: (event: ChatEvent) => void,
  signal: AbortSignal,
  retryMessageId?: number,
  onRun?: (runId: number) => void,
): Promise<void> {
  const response = await studioFetch(`/api/chat/sessions/${sessionId}/run`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      ...(retryMessageId ? { retry_message_id: retryMessageId } : { content, file_ids: fileIds }),
      skill_ids: skillIds,
    }),
    signal,
  });
  if (!response.ok || !response.body || !response.headers.get('Content-Type')?.includes('text/event-stream')) {
    const payload = await response.json().catch(() => null);
    console.error('Chat turn rejected', response.status, payload?.code);
    throw requestError(response.status, payload?.code);
  }
  const runId = Number(response.headers.get('X-A1O-Run-Id'));
  if (runId > 0) onRun?.(runId);
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  let data: string[] = [];
  let complete = false;
  let damaged = false;
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });
      let boundary = buffer.indexOf('\n');
      while (boundary >= 0) {
        const line = buffer.slice(0, boundary).replace(/\r$/, '');
        buffer = buffer.slice(boundary + 1);
        if (line.startsWith('data:')) data.push(line.slice(5).replace(/^ /, ''));
        if (!line && data.length) {
          try {
            const event = JSON.parse(data.join('\n')) as ChatEvent;
            if (typeof event?.type !== 'string') throw new Error('Missing event type');
            if (event.type === 'run.completed') complete = true;
            onEvent(event);
          } catch (error) {
            console.error('Invalid Chat stream event', error);
            damaged = true;
          }
          data = [];
        }
        boundary = buffer.indexOf('\n');
      }
    }
  } catch (error) {
    if (signal.aborted) throw error;
    console.error('Chat stream read failed', error);
    throw new Error(i18n.t('chat.streamInterrupted'));
  } finally {
    reader.releaseLock();
  }
  if (!complete || damaged || data.length || buffer.trim()) throw new Error(i18n.t('chat.streamInterrupted'));
}
