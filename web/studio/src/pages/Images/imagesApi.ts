// API client for the image workbench (studio proposal §7): persistent
// generation runs driven by the backend worker, retryable product saving,
// and the gallery. The server contract lives in controller/imagestudio.go.
import { studioRequest as request } from '@/helpers/studioRequest';

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
  video_duration?: number;
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
  reference_models?: string[];
  video_models?: string[];
  ark_image_models?: string[];
  ark_video_models?: string[];
  known_sizes: Record<string, string[]>;
  max_active: number;
  max_reference_bytes?: number;
  max_upload_bytes?: number;
}

function randomKey(): string {
  return `img-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
}

export const assetUrl = (id: number) => `/api/studio/images/assets/${id}/content`;

export const imagesApi = {
  assetPage: (before = 0) =>
    request<{ assets: ImageAsset[]; next: number }>(`/api/studio/images/assets?before=${before}`),
  attachAsset: (id: number) =>
    request<{ id: number; name: string }>(`/api/studio/images/assets/${id}/attachment`, { method: 'POST' }),
  deleteAsset: (id: number) =>
    request<void>(`/api/studio/images/assets/${id}`, { method: 'DELETE' }),
  meta: () => request<ImageMeta>('/api/studio/images/meta'),
  create: (payload: {
    model: string;
    prompt: string;
    size: string;
    quality?: string;
    n?: number;
    video_duration?: number;
    reference_asset_id?: number;
    reference_file_id?: number;
  }) =>
    request<ImageGeneration>('/api/studio/images', {
      method: 'POST',
      body: JSON.stringify({ ...payload, idempotency_key: randomKey() }),
    }),
  list: () =>
    request<{ generations: ImageGenerationView[]; assets: ImageAsset[] }>('/api/studio/images'),
  get: (id: number, signal: AbortSignal) => request<ImageGenerationView>(`/api/studio/images/${id}`, { signal }, false),
  retrySave: (id: number) =>
    request<ImageGenerationView>(`/api/studio/images/${id}/retry-save`, { method: 'POST' }),
  cancel: (id: number) =>
    request<null>(`/api/studio/images/${id}/cancel`, { method: 'POST' }),
};
