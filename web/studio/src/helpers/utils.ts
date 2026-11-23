import { toast } from 'sonner';

export function isAdmin() {
  const user = localStorage.getItem('user');
  if (!user) return false;
  try {
    const role = (JSON.parse(user) as { role?: number } | null)?.role;
    return typeof role === 'number' && role >= 10;
  } catch {
    return false;
  }
}

export function getSystemName() {
  const system_name = localStorage.getItem('system_name');
  if (!system_name) return 'A1O Studio';
  return system_name;
}

export function getLogo() {
  const logo = localStorage.getItem('logo');
  if (!logo) return '/logo.png';
  return logo;
}

export function isMobile() {
  return window.innerWidth <= 600;
}

export function showError(error: unknown) {
  if (!error) return;
  toast.error(error instanceof Error ? error.message : String(error));
}

export function showSuccess(message: string) {
  toast.success(message);
}

export function showInfo(message: string) {
  toast.info(message);
}
