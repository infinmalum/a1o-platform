import { clearUser } from '@/context/User';

let redirecting = false;

export function expireSession() {
  if (redirecting) return;
  clearUser();
  if (location.pathname === '/login' || location.pathname.startsWith('/oauth/')) return;
  redirecting = true;
  const next = location.pathname + location.search + location.hash;
  location.replace('/login?' + new URLSearchParams({ expired: 'true', next }));
}

export function safeReturnPath(value: string | null) {
  if (!value?.startsWith('/') || value.startsWith('//') || value.includes('\\')) return '/chat';
  const url = new URL(value, location.origin);
  if (url.origin !== location.origin || url.pathname === '/login' || url.pathname.startsWith('/oauth/')) return '/chat';
  return url.pathname + url.search + url.hash;
}
