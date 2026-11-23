import { studioFetch } from '@/helpers/studioRequest';

export const protocols = {
  chat: 'OpenAI Chat Completions',
  responses: 'OpenAI Responses',
  anthropic: 'Anthropic Messages',
  gemini: 'Gemini generateContent',
} as const;

export type Protocol = keyof typeof protocols;
export type PlaygroundRequest = { path: string; body: string; headers: Record<string, string> };
export type PlaygroundUsage = { quota: number; prompt_tokens: number; completion_tokens: number };

export function requestTemplate(
  protocol: Protocol,
  prompt: string,
  system?: string,
  sampling?: { temperature: number | null; top_p: number | null; max_tokens: number },
) {
  const temperature = sampling?.temperature ?? undefined;
  const topP = sampling?.top_p ?? undefined;
  const maxTokens = sampling?.max_tokens || undefined;
  const messages = [{ role: 'user', content: prompt }];
  switch (protocol) {
    case 'chat':
      return {
        messages: system ? [{ role: 'system', content: system }, ...messages] : messages,
        temperature, top_p: topP, max_completion_tokens: maxTokens,
      };
    case 'responses':
      return { input: messages, instructions: system, temperature, top_p: topP, max_output_tokens: maxTokens };
    case 'anthropic':
      return { messages, system, temperature, top_p: topP, max_tokens: maxTokens ?? 1024 };
    case 'gemini':
      return {
        contents: [{ role: 'user', parts: [{ text: prompt }] }],
        systemInstruction: system ? { parts: [{ text: system }] } : undefined,
        generationConfig: sampling ? { temperature, topP, maxOutputTokens: maxTokens } : undefined,
      };
  }
}

export function buildRequest(
  protocol: Protocol, model: string, source: string, geminiStream: boolean,
): PlaygroundRequest {
  const raw = source.trim();
  const parsed: unknown = JSON.parse(raw);
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed) || Object.hasOwn(parsed, 'model')) {
    throw new Error('Expected a JSON object without a model field');
  }
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  if (protocol === 'anthropic') headers['anthropic-version'] = '2023-06-01';

  if (protocol === 'gemini') {
    const action = geminiStream ? 'streamGenerateContent?alt=sse' : 'generateContent';
    return { path: `/v1beta/models/${encodeURIComponent(model)}:${action}`, body: raw, headers };
  }
  const path = {
    chat: '/v1/chat/completions',
    responses: '/v1/responses',
    anthropic: '/v1/messages',
  }[protocol];
  // Add the selected model without reserializing protocol-specific JSON values.
  const fields = raw.slice(1, -1).trim();
  const body = `{\n  "model": ${JSON.stringify(model)}${fields ? `,\n  ${fields}` : ''}\n}`;
  return { path, body, headers };
}

function shellQuote(value: string) {
  return "'" + value.replaceAll("'", "'\"'\"'") + "'";
}

export function exportCurl(request: PlaygroundRequest, origin: string) {
  const headers = Object.entries(request.headers).map(([key, value]) =>
    `  -H ${shellQuote(`${key}: ${value}`)}`,
  );
  return [
    `curl -N ${shellQuote(origin + request.path)}`,
    '  -H "Authorization: Bearer ${A1O_API_KEY}"',
    ...headers,
    `  --data-binary ${shellQuote(request.body)}`,
  ].join(' \\\n');
}

export function runPlayground(request: PlaygroundRequest, signal: AbortSignal) {
  return studioFetch('/api/chat/playground' + request.path, {
    method: 'POST',
    credentials: 'same-origin',
    headers: request.headers,
    body: request.body,
    signal,
  }, true);
}

export async function getPlaygroundUsage(requestId: string, signal: AbortSignal) {
  const response = await studioFetch(`/api/chat/playground/usage/${encodeURIComponent(requestId)}`, {
    credentials: 'same-origin',
    cache: 'no-store',
    signal,
  });
  if (response.ok) return await response.json() as PlaygroundUsage;
}
