// Shared plumbing for the playground pages: enabled-token loading and direct
// relay calls that authenticate with the chosen token key, so a playground
// request bills the operator's own account exactly like an API call would.

import { API } from '../../helpers';

export interface PlaygroundToken {
  id: number;
  name: string;
  key: string;
  status: number;
}

export async function loadPlaygroundTokens(): Promise<PlaygroundToken[]> {
  const res = await API.get('/api/token/?p=0&order=');
  const { success, message, data } = res.data;
  if (!success) {
    throw new Error(message || '无法加载令牌列表');
  }
  return (data || []).filter((token: PlaygroundToken) => token.status === 1);
}

// relayPost sends one request to a token-authenticated relay endpoint and
// returns the parsed JSON body. Non-2xx replies surface the error message
// from the envelope the gateway or provider produced.
export async function relayPost(
  path: string,
  tokenKey: string,
  body: unknown
): Promise<any> {
  const response = await fetch(path, {
    method: 'POST',
    credentials: 'same-origin',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer sk-${tokenKey}`,
    },
    body: JSON.stringify(body),
  });
  const text = await response.text();
  let payload: any = null;
  try {
    payload = JSON.parse(text);
  } catch {
    // non-JSON body: fall back to the raw text in the error below
  }
  if (!response.ok) {
    throw new Error(
      payload?.error?.message ||
        payload?.message ||
        text ||
        `HTTP ${response.status}`
    );
  }
  return payload;
}
