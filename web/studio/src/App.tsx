import React, { useEffect, useState } from 'react';
import { Link, NavLink, Navigate, Route, Routes, useLocation, useNavigate } from 'react-router-dom';
import { LogOut } from 'lucide-react';
import { Trans, useTranslation } from 'react-i18next';

import { API } from './helpers/api';
import { getLogo, getSystemName, isAdmin, showError } from './helpers/utils';
import { clearUser, storeUser, useUser, type StudioUser } from './context/User';
import type { StudioStatus } from './helpers/studioStatus';
import { expireSession } from './helpers/session';
import Home from './pages/Home';
import Login from './pages/Login';
import Chat from './pages/Chat';
import Work from './pages/Work';
import WorkTaskDetail from './pages/Work/WorkTaskDetail';
import Agents from './pages/Agents';
import Workspace from './pages/Workspace';
import Account from './pages/Account';
import Images from './pages/Images';
import AudioStudio from './pages/Audio';
import Models from './pages/Models';
import OAuthCallback from './pages/OAuthCallback';
import LanguageSwitcher from './components/LanguageSwitcher';
import { Button } from '@/components/ui/button';

const studioLinks = [
  ['chat', 'chat'], ['work', 'work'], ['agents', 'agent'], ['workspace', 'workspace'],
  ['images', 'image'], ['audio', 'audio'], ['account', 'account'],
  ['models', 'models'],
];

function PrivateRoute({ children, session, retry }: {
  children: React.ReactNode;
  session: 'checking' | 'ready' | 'error';
  retry: () => void;
}) {
  const { t } = useTranslation();
  const location = useLocation();
  const { user } = useUser();
  if (session === 'checking') return <p className='p-6 text-sm' role='status'>{t('auth.session.checking')}</p>;
  if (session === 'error') return (
    <div className='flex flex-wrap items-center gap-3 p-6' role='alert'>
      {t('auth.session.failed')} <Button size='sm' onClick={retry}>{t('studio.list.retry')}</Button>
    </div>
  );
  return user ? <>{children}</> : <Navigate to='/login' state={{ from: location }} replace />;
}

const App: React.FC<{ status: StudioStatus | null }> = ({ status }) => {
  const { t } = useTranslation();
  const { user } = useUser();
  const navigate = useNavigate();
  const [session, setSession] = useState<'checking' | 'ready' | 'error'>('checking');
  const [sessionAttempt, setSessionAttempt] = useState(0);
  const retry = () => { setSession('checking'); setSessionAttempt((current) => current + 1); };

  useEffect(() => {
    const request = new AbortController();
    const hadUser = Boolean(localStorage.getItem('user'));
    const synchronize = () => { request.abort(); setSession('ready'); };
    window.addEventListener('user-changed', synchronize);
    void (async () => {
      try {
        const response = await fetch('/api/user/self', { credentials: 'same-origin', cache: 'no-store', signal: request.signal });
        const payload = await response.json().catch(() => null);
        if (request.signal.aborted) return;
        if (response.status === 401 || payload?.code === 'studio_account_disabled') {
          const page = location.pathname;
          if (hadUser && page !== '/' && page !== '/login' && !page.startsWith('/oauth/')) expireSession();
          else if (hadUser) clearUser();
          setSession('ready');
          return;
        }
        if (!response.ok || !payload?.success || !payload.data) {
          setSession('error');
          return;
        }
        const data = payload.data as StudioUser;
        storeUser(data);
        setSession('ready');
      } catch {
        if (!request.signal.aborted) setSession('error');
      }
    })();
    return () => {
      window.removeEventListener('user-changed', synchronize);
      request.abort();
    };
  }, [sessionAttempt]);

  const logout = async () => {
    try {
      const { data } = await API.get('/api/user/logout');
      if (data?.success) {
        clearUser();
        navigate('/login');
      } else {
        showError(t('auth.session.logoutFailed'));
      }
    } catch {
      // The API interceptor handles network and HTTP failures.
    }
  };

  const initialized = status?.initialized !== false;
  if (!initialized) {
    return (
      <div className='flex min-h-screen items-center justify-center p-6'>
        <div className='text-center'>
          <p className='text-lg font-medium'>{t('auth.setup.title')}</p>
          <p className='text-muted-foreground mt-2 text-sm'>
            <Trans i18nKey='auth.setup.description' components={{ setup: <a className='underline' href='/admin/setup' /> }} />
          </p>
          <div className='mt-4 flex justify-center'><LanguageSwitcher /></div>
        </div>
      </div>
    );
  }

  return (
    <div className='flex min-h-screen flex-col'>
      <header className='bg-background/95 supports-backdrop-filter:bg-background/60 sticky top-0 z-40 border-b backdrop-blur'>
        <div className='mx-auto flex h-14 w-full max-w-6xl items-center justify-between px-4'>
          <Link to='/' className='flex items-center gap-2'>
            <img src={getLogo()} alt='' className='size-7 object-contain' />
            <span className='text-base font-semibold'>{getSystemName()}</span>
          </Link>
          <div className='flex items-center gap-2'>
            <LanguageSwitcher />
            {session === 'ready' && user ? (
              <>
                {isAdmin() && (
                  <Button variant='ghost' size='sm' asChild>
                    <a href='/admin/channel'>{t('auth.header.admin')}</a>
                  </Button>
                )}
                <span className='text-muted-foreground text-sm'>{user.username}</span>
                <Button variant='ghost' size='icon-sm' onClick={logout} title={t('auth.header.logout')} aria-label={t('auth.header.logout')}>
                  <LogOut />
                </Button>
              </>
            ) : (
              <Button size='sm' onClick={() => navigate('/login')}>
                {t('auth.login.button')}
              </Button>
            )}
          </div>
        </div>
        {session === 'ready' && user && (
          <nav aria-label={t('auth.header.navigation')} className='mx-auto flex w-full max-w-6xl gap-1 overflow-x-auto px-4 pb-2'>
            {studioLinks.map(([path, key]) => (
              <NavLink key={path} to={'/' + path} className={({ isActive }) =>
                'shrink-0 rounded-md px-2.5 py-1.5 text-sm hover:bg-accent ' +
                (isActive ? 'bg-accent font-medium text-accent-foreground' : 'text-muted-foreground')}>
                {t('studio.home.' + key + '.title')}
              </NavLink>
            ))}
          </nav>
        )}
      </header>
      <main className='flex-1'>
        <Routes>
          <Route path='/' element={<Home />} />
          <Route
            path='/chat'
            element={
              <PrivateRoute session={session} retry={retry}>
                <Chat />
              </PrivateRoute>
            }
          />
          <Route
            path='/work'
            element={
              <PrivateRoute session={session} retry={retry}>
                <Work />
              </PrivateRoute>
            }
          />
          <Route
            path='/work/:taskId'
            element={
              <PrivateRoute session={session} retry={retry}>
                <WorkTaskDetail />
              </PrivateRoute>
            }
          />
          <Route
            path='/agents'
            element={
              <PrivateRoute session={session} retry={retry}>
                <Agents />
              </PrivateRoute>
            }
          />
          <Route
            path='/workspace'
            element={
              <PrivateRoute session={session} retry={retry}>
                <Workspace />
              </PrivateRoute>
            }
          />
          <Route
            path='/account'
            element={
              <PrivateRoute session={session} retry={retry}>
                <Account />
              </PrivateRoute>
            }
          />
          <Route
            path='/images'
            element={
              <PrivateRoute session={session} retry={retry}>
                <Images />
              </PrivateRoute>
            }
          />
          <Route path='/audio' element={<PrivateRoute session={session} retry={retry}><AudioStudio /></PrivateRoute>} />
          <Route path='/models' element={<PrivateRoute session={session} retry={retry}><Models /></PrivateRoute>} />
          <Route path='/login' element={<Login status={status} />} />
          <Route path='/oauth/github' element={<OAuthCallback provider='github' onSuccess={storeUser} />} />
          <Route path='/oauth/lark' element={<OAuthCallback provider='lark' onSuccess={storeUser} />} />
          <Route path='*' element={<Navigate to='/' replace />} />
        </Routes>
      </main>
    </div>
  );
};

export default App;
