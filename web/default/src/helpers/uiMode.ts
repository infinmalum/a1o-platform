// One console serves two very different audiences: people who only want to
// chat, and people who only wire the API into their own tools. Each account
// picks a mode once and the navigation is trimmed to what that mode uses, so a
// newcomer is not handed a gateway control panel on first login. Administrators
// always see every entry, whatever is stored here.

export type UiMode = 'chat' | 'api' | 'both';

const STORAGE_KEY = 'ui_mode';

export function getUiMode(): UiMode | null {
  const stored = localStorage.getItem(STORAGE_KEY);
  if (stored === 'chat' || stored === 'api' || stored === 'both') {
    return stored;
  }
  return null;
}

export function setUiMode(mode: UiMode) {
  localStorage.setItem(STORAGE_KEY, mode);
}

// landingForMode is where a sign-in should drop the user: chat users land in
// the chat, everyone else lands on the page that explains how to call the API.
export function landingForMode(mode: UiMode): string {
  return mode === 'chat' ? '/chat' : '/quickstart';
}

// navVisible reports whether a navigation entry belongs in the current mode.
// Entries with no mode list (settings, about) are always shown.
export function navVisible(
  modes: UiMode[] | undefined,
  mode: UiMode | null,
  isAdminUser: boolean
): boolean {
  if (isAdminUser || !modes || !mode) {
    return true;
  }
  return modes.includes(mode);
}
