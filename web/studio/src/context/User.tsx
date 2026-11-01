import React, { createContext, useContext } from 'react';

export interface StudioUser {
  id?: number;
  username?: string;
  role?: number;
  [key: string]: unknown;
}

interface UserContextValue {
  user: StudioUser | null;
  refresh: () => void;
}

// Studio keeps session state deliberately thin: the login payload is stored
// under the same `user` key as the admin console, so a same-origin admin
// session carries over and each page re-reads storage on navigation.
const UserContext = createContext<UserContextValue>({
  user: null,
  refresh: () => undefined,
});

export const UserProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const [user, setUser] = React.useState<StudioUser | null>(() => {
    const raw = localStorage.getItem('user');
    return raw ? (JSON.parse(raw) as StudioUser) : null;
  });

  const refresh = () => {
    const raw = localStorage.getItem('user');
    setUser(raw ? (JSON.parse(raw) as StudioUser) : null);
  };

  return <UserContext.Provider value={{ user, refresh }}>{children}</UserContext.Provider>;
};

export const useUser = () => useContext(UserContext);

export function storeUser(data: unknown) {
  localStorage.setItem('user', JSON.stringify(data));
  window.dispatchEvent(new Event('user-changed'));
}

export function clearUser() {
  localStorage.removeItem('user');
  window.dispatchEvent(new Event('user-changed'));
}
