export interface SiteStatus {
  github_oauth?: boolean;
  github_client_id?: string;
  lark_client_id?: string;
  turnstile_check?: boolean;
  turnstile_site_key?: string;
  top_up_link?: string;
}
