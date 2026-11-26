// API client for the admin console's chat-management settings (global skills
// and MCP servers). End-user chat surfaces live in the studio app; the
// session/message/file/turn endpoints are intentionally absent here.
import { showError } from './utils';
import i18n from '../i18n';

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

interface ApiResponse<T> {
  success: boolean;
  message?: string;
  code?: string;
  data: T;
}

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
    const markdownError = payload.code === 'skill_markdown_invalid'
      ? i18n.t('chat.skillMarkdownInvalid')
      : payload.code === 'skill_markdown_too_large' ? i18n.t('chat.skillMarkdownTooLarge') : undefined;
    const error = new Error(markdownError || payload.message || `HTTP ${response.status}`);
    showError(error);
    throw error;
  }
  return payload.data;
}

export const chatApi = {
  previewSkillMarkdown: (body: File) =>
    request<Pick<ChatSkill, 'name' | 'description' | 'instructions'>>('/api/chat/skills/preview', {
      method: 'POST',
      headers: { 'Content-Type': 'text/markdown; charset=utf-8' },
      body,
    }),
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
};
