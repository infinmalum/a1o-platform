// API client for the image workbench (studio proposal §7): persistent
// generation runs driven by the backend worker, retryable product saving,
// and the gallery. The server contract lives in controller/imagestudio.go.
import { showError } from '../../helpers/utils';

export type ImageGenStatus =
  | 'queued'
  | 'running'
  | 'completed'
  | 'save_failed'
  | 'failed'
  | 'cancelled'
  | 'needs_reconciliation';

export interface ImageAsset {
  id: number;
  run_id: number;
  slot: number;
  mime: string;
  size: number;
  created_time: number;
}

export interface ImageGeneration {
  id: number;
  model: string;
  prompt: string;
  size: string;
  quality: string;
  n: number;
  status: ImageGenStatus;
  error: string;
  save_error: string;
  created_time: number;
  updated_time: number;
}

export interface ImageGenerationView extends ImageGeneration {
  assets: ImageAsset[];
}

export interface ImageMeta {
  known_models: string[];
  other_models: string[];
  known_sizes: Record<string, string[]>;
  max_active: number;
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

function randomKey(): string {
  return `img-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
}

export const assetUrl = (id: number) => `/api/studio/images/assets/${id}/content`;

export const imagesApi = {
  meta: () => request<ImageMeta>('/api/studio/images/meta'),
  create: (payload: {
    model: string;
    prompt: string;
    size: string;
    quality?: string;
    n?: number;
  }) =>
    request<ImageGeneration>('/api/studio/images', {
      method: 'POST',
      body: JSON.stringify({ ...payload, idempotency_key: randomKey() }),
    }),
  list: () =>
    request<{ generations: ImageGenerationView[]; assets: ImageAsset[] }>('/api/studio/images'),
  get: (id: number) => request<ImageGenerationView>(`/api/studio/images/${id}`),
  retrySave: (id: number) =>
    request<ImageGenerationView>(`/api/studio/images/${id}/retry-save`, { method: 'POST' }),
  cancel: (id: number) =>
    request<null>(`/api/studio/images/${id}/cancel`, { method: 'POST' }),
};
