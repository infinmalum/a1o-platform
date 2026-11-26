import { pageQuery } from '@/components/StudioList';
// API client for the Studio work runtime (R3): task list, run projection,
// event observation over EventSource, appended inputs, and cancel. The
// server contract lives in controller/studio.go.
import { studioRequest as request } from '@/helpers/studioRequest';

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
  // Reconciliation forensics, populated for needs_reconciliation runs: model
  // calls without a verifiable outcome and tool executions that started but
  // never recorded a result.
  unknown_invocations: number;
  unverified_tools: string[];
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
  reference_file_id?: number;
}

export function newIdempotencyKey(): string {
  if (typeof crypto !== 'undefined' && 'randomUUID' in crypto) {
    return crypto.randomUUID();
  }
  return `key-${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

export const workApi = {
  listTasks: (page: number) =>
    request<WorkTaskView[]>('/api/studio/tasks?' + pageQuery(page), undefined, false),
  getTask: (taskId: number, signal?: AbortSignal) =>
    request<{ task: WorkTask; runs: AgentRun[] }>(`/api/studio/tasks/${taskId}`, { signal }),
  getRun: (runId: number, signal?: AbortSignal, notify = true) =>
    request<RunProjection>(`/api/studio/runs/${runId}`, { signal }, notify),
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
  resumeRun: (runId: number) =>
    request<{
      status: string;
      changed: boolean;
      unknown_invocations: number;
      unverified_tools: string[];
    }>(`/api/studio/runs/${runId}/resume`, {
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
