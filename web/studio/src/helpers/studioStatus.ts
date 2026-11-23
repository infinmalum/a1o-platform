export interface StudioStatus {
  initialized?: boolean;
  system_name?: string;
  logo?: string;
  quota_per_unit?: number;
  display_in_currency?: boolean;
  github_oauth?: boolean;
  github_client_id?: string;
  lark_client_id?: string;
}

export function cachedStudioStatus(): StudioStatus | null {
  try {
    return JSON.parse(localStorage.getItem('status') ?? 'null') as StudioStatus | null;
  } catch {
    return null;
  }
}
