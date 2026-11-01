// API client for the Studio work runtime (R3): task list, run projection,
// event observation over EventSource, appended inputs, and cancel. The
// server contract lives in controller/studio.go.
import { showError } from '../../helpers/utils';

export interface WorkTask {
  id: number;
  user_id: number;
  title: string;
  status: 'active' | 'archived';
  summary: string;
  created_time: number;
  updated_time: number;
}

export interface AgentRun {
  id: number;
  user_id: number;
  kind: 'work' | 'chat';
  status:
    | 'queued'
    | 'running'
    | 'waiting_input'
    | 'cancel_requested'
    | 'completed'
    | 'failed'
    | 'cancelled'
    | 'needs_reconciliation';
  session_id: number;
  task_id: number;
  model: string;
  error: string;
  created_time: number;
  updated_time: number;
}

export interface WorkTaskView extends WorkTask {
  latest_run: AgentRun | null;
}

export interface RunProjectionItem {
  role: 'user' | 'assistant';
  text: string;
}

export interface RunProjection {
  run: AgentRun;
  task: WorkTask | null;
  messages: RunProjectionItem[];
  pending_inputs: number;
  prompt_tokens: number;
  completion_tokens: number;
  invocations: number;
}

export interface RunEventPayload {
  [key: string]: unknown;
}

export interface RunEvent {
  run_id: number;
  event_id?: string;
  sequence?: number;
  type: string;
  schema_version?: string;
  invocation_id?: number;
  payload?: RunEventPayload;
}

export interface StudioRunCreateResult {
  run: AgentRun;
  created: boolean;
  task?: WorkTask;
}

export interface StudioRunCreatePayload {
  title: string;
  input: string;
  model: string;
  agent_version_id?: number;
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

export function newIdempotencyKey(): string {
  if (typeof crypto !== 'undefined' && 'randomUUID' in crypto) {
    return crypto.randomUUID();
  }
  return `key-${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

export const workApi = {
  listTasks: () => request<WorkTaskView[]>('/api/studio/tasks'),
  getTask: (taskId: number) =>
    request<{ task: WorkTask; runs: AgentRun[] }>(`/api/studio/tasks/${taskId}`),
  getRun: (runId: number) => request<RunProjection>(`/api/studio/runs/${runId}`),
  createRun: (payload: StudioRunCreatePayload) =>
    request<StudioRunCreateResult>('/api/studio/runs', {
      method: 'POST',
      body: JSON.stringify({ kind: 'work', ...payload, idempotency_key: newIdempotencyKey() }),
    }),
  appendInput: (runId: number, content: string) =>
    request<{ input: { id: number }; created: boolean }>(`/api/studio/runs/${runId}/inputs`, {
      method: 'POST',
      body: JSON.stringify({ content, idempotency_key: newIdempotencyKey() }),
    }),
  cancelRun: (runId: number) =>
    request<{ status: string; changed: boolean }>(`/api/studio/runs/${runId}/cancel`, {
      method: 'POST',
      body: JSON.stringify({}),
    }),
  listModels: () => request<string[]>('/api/user/available_models'),
};

export const ACTIVE_RUN_STATUSES: AgentRun['status'][] = [
  'queued',
  'running',
  'cancel_requested',
];

export function isRunActive(status: AgentRun['status']): boolean {
  return ACTIVE_RUN_STATUSES.includes(status) || status === 'waiting_input';
}

export function isRunTerminal(status: AgentRun['status']): boolean {
  return !isRunActive(status);
}

export type RunStatus = AgentRun['status'];

export function statusBadgeVariant(
  status: RunStatus,
): 'default' | 'secondary' | 'destructive' | 'outline' {
  switch (status) {
    case 'running':
      return 'default';
    case 'completed':
      return 'secondary';
    case 'failed':
    case 'cancelled':
    case 'needs_reconciliation':
      return 'destructive';
    default:
      return 'outline';
  }
}
