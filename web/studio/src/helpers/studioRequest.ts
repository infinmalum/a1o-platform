import i18n from '@/i18n';
import { expireSession } from './session';
import { showError } from './utils';

const errorKeys: Record<string, string> = {
  file_required: 'studio.errors.fileRequired',
  upload_read_failed: 'studio.errors.uploadRead',
  upload_save_failed: 'studio.errors.uploadSave',
  file_too_large: 'studio.errors.fileLimit',
  text_file_too_large: 'studio.errors.textFileLimit',
  knowledge_file_too_large: 'studio.errors.knowledgeFileLimit',
  knowledge_invalid_file: 'studio.errors.knowledgeInvalidFile',
  knowledge_capacity: 'studio.errors.knowledgeCapacity',
  knowledge_not_found: 'studio.errors.notFound',
  image_active_limit: 'images.activeLimit',
  image_invalid_settings: 'images.invalidSettings',
  chat_busy: 'studio.errors.chatBusy',
  retry_unavailable: 'studio.errors.retryUnavailable',
  skill_markdown_invalid: 'chat.skillMarkdownInvalid',
  skill_markdown_too_large: 'chat.skillMarkdownTooLarge',
};

class StudioRequestError extends Error {
  constructor(message: string, readonly status?: number) { super(message); }
}

export function requestError(status?: number, code?: string, limit?: string) {
  const key = (code && Object.hasOwn(errorKeys, code) && errorKeys[code]) || ({
    401: 'messages.error.login_expired',
    403: 'studio.errors.forbidden',
    404: 'studio.errors.notFound',
    413: 'studio.errors.requestTooLarge',
    429: 'studio.errors.rateLimit',
  } as Record<number, string>)[status ?? 0] || 'studio.errors.requestFailed';
  return new StudioRequestError(i18n.t(key, { limit }), status);
}

export async function studioFetch(url: string, init?: RequestInit, relay = false) {
  let response: Response;
  try {
    response = await fetch(url, { credentials: 'same-origin', ...init });
  } catch (cause) {
    if (init?.signal?.aborted) throw cause;
    console.error(cause);
    throw new Error(i18n.t('studio.errors.network'));
  }
  // A provider can reject the gateway's credentials while the browser's
  // session remains valid. The session middleware marks its own responses.
  if (response.headers.get('X-Studio-Session-Required') === '1' || (!relay && response.status === 401)) {
    expireSession();
    throw requestError(401);
  }
  return response;
}

export async function studioRequest<T>(url: string, init?: RequestInit, notify = true): Promise<T> {
  try {
    const response = await studioFetch(url, init);
    const payload = await response.json().catch(() => null);
    if (payload?.code === 'studio_account_disabled') {
      expireSession();
      throw requestError(401);
    }
    if (!response.ok || !payload?.success) {
      console.error('Studio request failed', url.split('?')[0], response.status, payload?.code);
      throw requestError(response.status, payload?.code, payload?.limit);
    }
    return payload.data;
  } catch (cause) {
    if (notify && !init?.signal?.aborted && !(cause instanceof StudioRequestError && cause.status === 401)) showError(cause);
    throw cause;
  }
}
