import React, { useEffect } from 'react';
import { Navigate, Route, Routes, useLocation, useNavigate } from 'react-router-dom';
import { LogOut } from 'lucide-react';

import { API } from './helpers/api';
import { getLogo, getSystemName, isAdmin } from './helpers/utils';
import { clearUser, storeUser, useUser } from './context/User';
import Home from './pages/Home';
import Login from './pages/Login';
import Chat from './pages/Chat';
import OAuthCallback from './pages/OAuthCallback';
import { Button } from '@/components/ui/button';

function PrivateRoute({ children }: { children: React.ReactNode }) {
  const location = useLocation();
  if (!localStorage.getItem('user')) {
    return <Navigate to='/login' state={{ from: location }} replace />;
  }
  return <>{children}</>;
}

const App: React.FC = () => {
  const { user, refresh } = useUser();
  const navigate = useNavigate();

  // One status fetch at boot: it carries the branding, the quota display
  // unit, and the initialization flag the setup notice reacts to.
  useEffect(() => {
    API.get('/api/status')
      .then((res) => {
        const { success, data } = res.data || {};
        if (success && data) {
          localStorage.setItem('status', JSON.stringify(data));
          localStorage.setItem('system_name', data.system_name);
          localStorage.setItem('logo', data.logo);
          localStorage.setItem('quota_per_unit', data.quota_per_unit);
          localStorage.setItem('display_in_currency', data.display_in_currency);
          document.title = getSystemName();
          const link = document.querySelector<HTMLLinkElement>("link[rel~='icon']");
          if (link && data.logo) {
            link.href = data.logo;
          }
        }
      })
      .catch(() => undefined);
    window.addEventListener('user-changed', refresh);
    return () => window.removeEventListener('user-changed', refresh);
  }, [refresh]);

  const logout = async () => {
    await API.get('/api/user/logout');
    clearUser();
    navigate('/login');
  };

  const initialized = (() => {
    const status = localStorage.getItem('status');
    if (!status) return true;
    return (JSON.parse(status) as { initialized?: boolean }).initialized !== false;
  })();

  if (!initialized) {
    return (
      <div className='flex min-h-screen items-center justify-center p-6'>
        <div className='text-center'>
          <p className='text-lg font-medium'>系统尚未初始化</p>
          <p className='text-muted-foreground mt-2 text-sm'>
            请前往 <a className='underline' href='/admin/setup'>管理端初始化页面</a> 完成安装。
          </p>
        </div>
      </div>
    );
  }

  return (
    <div className='flex min-h-screen flex-col'>
      <header className='bg-background/95 supports-backdrop-filter:bg-background/60 sticky top-0 z-40 border-b backdrop-blur'>
        <div className='mx-auto flex h-14 w-full max-w-6xl items-center justify-between px-4'>
          <a href='/' className='flex items-center gap-2'>
            <img src={getLogo()} alt='logo' className='size-7 object-contain' />
            <span className='text-base font-semibold'>{getSystemName()}</span>
          </a>
          <div className='flex items-center gap-2'>
            {user ? (
              <>
                {isAdmin() && (
                  <Button variant='ghost' size='sm' asChild>
                    <a href='/admin/channel'>管理控制台</a>
                  </Button>
                )}
                <span className='text-muted-foreground text-sm'>{user.username}</span>
                <Button variant='ghost' size='icon-sm' onClick={logout} title='退出登录'>
                  <LogOut />
                </Button>
              </>
            ) : (
              <Button size='sm' onClick={() => navigate('/login')}>
                登录
              </Button>
            )}
          </div>
        </div>
      </header>
      <main className='flex-1'>
        <Routes>
          <Route path='/' element={<Home />} />
          <Route
            path='/chat'
            element={
              <PrivateRoute>
                <Chat />
              </PrivateRoute>
            }
          />
          <Route path='/login' element={<Login />} />
          <Route path='/oauth/github' element={<OAuthCallback provider='github' onSuccess={storeUser} />} />
          <Route path='/oauth/lark' element={<OAuthCallback provider='lark' onSuccess={storeUser} />} />
          <Route path='*' element={<Navigate to='/' replace />} />
        </Routes>
      </main>
    </div>
  );
};

export default App;
