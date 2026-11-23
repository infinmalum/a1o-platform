import React, { createContext, useContext } from 'react';

export interface StudioUser {
  id?: number;
  username?: string;
  role?: number;
  display_name?: string;
  status?: number;
  quota?: number;
  group?: string;
  [key: string]: unknown;
}

interface UserContextValue {
  user: StudioUser | null;
  refresh: () => void;
}

function readUser(): StudioUser | null {
  try {
    return JSON.parse(localStorage.getItem('user') ?? 'null') as StudioUser | null;
  } catch {
    return null;
  }
}

const UserContext = createContext<UserContextValue>({
  user: null,
  refresh: () => undefined,
});

export const UserProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const [user, setUser] = React.useState(readUser);
  const refresh = React.useCallback(() => setUser(readUser()), []);

  React.useEffect(() => {
    window.addEventListener('user-changed', refresh);
    window.addEventListener('storage', refresh);
    return () => {
      window.removeEventListener('user-changed', refresh);
      window.removeEventListener('storage', refresh);
    };
  }, [refresh]);

  return <UserContext.Provider value={{ user, refresh }}>{children}</UserContext.Provider>;
};

export const useUser = () => useContext(UserContext);

export function storeUser(data: unknown) {
  if (!data || typeof data !== 'object') return;
  const { id, username, role, display_name, status, quota, group } = data as StudioUser;
  localStorage.setItem('user', JSON.stringify({ id, username, role, display_name, status, quota, group }));
  window.dispatchEvent(new Event('user-changed'));
}

export function clearUser() {
  localStorage.removeItem('user');
  window.dispatchEvent(new Event('user-changed'));
}
