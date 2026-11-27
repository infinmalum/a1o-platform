import { studioFetch } from '@/helpers/studioRequest';

export type SettledUsage = {
  quota: number;
  prompt_tokens: number;
  completion_tokens: number;
  usage_state?: 'reported' | 'partial' | 'missing';
  charge_basis?: 'usage' | 'reservation' | 'fixed';
  interrupted?: boolean;
};

export async function waitForSettlement(path: string, signal: AbortSignal, pendingStatus?: number) {
  const bounded = AbortSignal.any([signal, AbortSignal.timeout(8000)]);
  for (const delay of [0, 200, 400, 800, 1200, 1600, 2000]) {
    if (delay) await new Promise<void>((resolve) => window.setTimeout(resolve, delay));
    bounded.throwIfAborted();
    const response = await studioFetch(path, { cache: 'no-store', signal: bounded });
    if (response.status === 200) return await response.json() as SettledUsage;
    if (response.status === 204) return null;
    if (response.status !== pendingStatus) break;
  }
}
