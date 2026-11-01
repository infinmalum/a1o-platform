// API client for the workspace page (R4): the user's run history across
// Chat and Work plus the shared asset (file) base. Run history reads the
// same /api/studio/runs fact source as the run API.
import { showError } from '../../helpers/utils';

export interface WorkspaceRun {
  id: number;
  kind: 'chat' | 'work';
  status: string;
  session_id: number;
  task_id: number;
  agent_version_id: number;
  model: string;
  task_title: string;
  session_title: string;
  created_time: number;
  updated_time: number;
}

export interface WorkspaceFile {
  id: number;
  name: string;
  kind: 'text' | 'image';
  size: number;
  created_time: number;
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

export const workspaceApi = {
  listRuns: () => request<WorkspaceRun[]>('/api/studio/runs'),
  listFiles: () => request<WorkspaceFile[]>('/api/chat/files'),
};

export function formatBytes(size: number): string {
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
  return `${(size / 1024 / 1024).toFixed(1)} MB`;
}
