import { pageQuery } from '@/components/StudioList';
// API client for the workspace page (R4): the user's run history across
// Chat and Work plus the shared asset (file) base. Run history reads the
// same /api/studio/runs fact source as the run API.
import { studioRequest as request } from '@/helpers/studioRequest';

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

export const workspaceApi = {
  listRuns: (page: number) => request<WorkspaceRun[]>('/api/studio/runs?' + pageQuery(page), undefined, false),
  listFiles: (page: number) => request<WorkspaceFile[]>('/api/chat/files?' + pageQuery(page), undefined, false),
};

export function formatBytes(size: number): string {
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
  return `${(size / 1024 / 1024).toFixed(1)} MB`;
}
