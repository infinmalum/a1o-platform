// API client for the translation workbench (studio proposal §6): engine
// metadata, one billed translation per call, and the cross-refresh history.
// The server contract lives in controller/translate.go.
import { showError } from '../../helpers/utils';

export interface TranslationRecord {
  id: number;
  engine: 'deepl' | 'llm';
  model: string;
  source_lang: string;
  target_lang: string;
  input_text: string;
  output_text: string;
  detected_lang: string;
  billed_characters: number;
  input_tokens: number;
  output_tokens: number;
  status: 'running' | 'completed' | 'failed';
  error: string;
  created_time: number;
}

export interface TranslationMeta {
  deepl_languages: string[];
  models: string[];
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
  return `tr-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
}

export const translateApi = {
  meta: () => request<TranslationMeta>('/api/studio/translations/meta'),
  create: (payload: {
    text: string;
    target_lang: string;
    source_lang?: string;
    engine: 'deepl' | 'llm';
    model?: string;
  }) =>
    request<TranslationRecord>('/api/studio/translations', {
      method: 'POST',
      body: JSON.stringify({ ...payload, idempotency_key: randomKey() }),
    }),
  history: () => request<TranslationRecord[]>('/api/studio/translations'),
};
