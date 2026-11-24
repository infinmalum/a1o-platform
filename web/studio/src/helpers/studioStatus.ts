export interface StudioStatus {
  initialized?: boolean;
  system_name?: string;
  logo?: string;
  quota_per_unit?: number;
  display_in_currency?: boolean;
  github_oauth?: boolean;
  github_client_id?: string;
  lark_client_id?: string;
  studio_languages?: { code: string; enabled: boolean }[];
}

function cachedStudioStatus(): StudioStatus | null {
  try {
    return JSON.parse(localStorage.getItem('status') ?? 'null') as StudioStatus | null;
  } catch {
    return null;
  }
}

export async function loadStudioStatus(): Promise<StudioStatus | null> {
  const baseURL = import.meta.env.REACT_APP_SERVER?.replace(/\/+$/, '') ?? '';
  try {
    const response = await fetch(baseURL + '/api/status', {
      cache: 'no-store',
      signal: AbortSignal.timeout(5000),
    });
    if (!response.ok) return cachedStudioStatus();
    const payload = await response.json();
    if (payload.success && payload.data) return payload.data;
  } catch {
    return cachedStudioStatus();
  }
  return cachedStudioStatus();
}
