import React, { lazy, Suspense, useContext, useEffect } from 'react';
import { Navigate, Route, Routes, useNavigate } from 'react-router-dom';
import Loading from './components/Loading';
import ErrorBoundary from './components/ErrorBoundary';
import User from './pages/User';
import { PrivateRoute } from './components/PrivateRoute';
import RegisterForm from './components/RegisterForm';
import LoginForm from './components/LoginForm';
import SetupForm from './components/SetupForm';
import NotFound from './pages/NotFound';
import Setting from './pages/Setting';
import EditUser from './pages/User/EditUser';
import AddUser from './pages/User/AddUser';
import { API, getLogo, getSystemName, showError, showNotice } from './helpers';
import PasswordResetForm from './components/PasswordResetForm';
import GitHubOAuth from './components/GitHubOAuth';
import PasswordResetConfirm from './components/PasswordResetConfirm';
import { UserContext } from './context/User';
import Channel from './pages/Channel';
import EditChannel from './pages/Channel/EditChannel';
import UpstreamAccount from './pages/UpstreamAccount';
import Redemption from './pages/Redemption';
import EditRedemption from './pages/Redemption/EditRedemption';
import Log from './pages/Log';
import LarkOAuth from './components/LarkOAuth';
import Dashboard from './pages/Dashboard';

const About = lazy(() => import('./pages/About'));

function App() {
  const [, userDispatch] = useContext(UserContext);
  const navigate = useNavigate();

  const loadUser = () => {
    let user = localStorage.getItem('user');
    if (user) {
      let data = JSON.parse(user);
      userDispatch({ type: 'login', payload: data });
    }
  };
  const loadStatus = async () => {
    try {
      const res = await API.get('/api/status');
      const { success, message, data } = res.data || {};
      if (success && data) {
        localStorage.setItem('status', JSON.stringify(data));
        // A fresh installation routes every page into the setup wizard; the
        // backend re-checks before the setup endpoint does anything.
        if (data.initialized === false && window.location.pathname !== '/admin/setup') {
          navigate('/setup', { replace: true });
          return;
        }
        localStorage.setItem('system_name', data.system_name);
        localStorage.setItem('logo', data.logo);
        localStorage.setItem('footer_html', data.footer_html);
        localStorage.setItem('quota_per_unit', data.quota_per_unit);
        localStorage.setItem('display_in_currency', data.display_in_currency);
        if (
          data.version !== import.meta.env.REACT_APP_VERSION &&
          data.version !== 'v0.0.0' &&
          import.meta.env.REACT_APP_VERSION !== ''
        ) {
          showNotice(
            `新版本可用：${data.version}，请使用快捷键 Shift + F5 刷新页面`
          );
        }
      } else {
        showError(message || '无法正常连接至服务器！');
      }
    } catch (error) {
      showError(error.message || '无法正常连接至服务器！');
    }
  };

  useEffect(() => {
    loadUser();
    loadStatus().then();
    let systemName = getSystemName();
    if (systemName) {
      document.title = systemName;
    }
    let logo = getLogo();
    if (logo) {
      let linkElement = document.querySelector<HTMLLinkElement>("link[rel~='icon']");
      linkElement.href = logo;
    }
  }, []);

  return (
    <ErrorBoundary>
      <Routes>
      <Route path='/' element={<Navigate to='/channel' replace />} />
      <Route
        path='/channel'
        element={
          <PrivateRoute>
            <Channel />
          </PrivateRoute>
        }
      />
      <Route
        path='/upstream_account'
        element={
          <PrivateRoute>
            <UpstreamAccount />
          </PrivateRoute>
        }
      />
      <Route
        path='/channel/edit/:id'
        element={
          <PrivateRoute>
            <Suspense fallback={<Loading></Loading>}>
              <EditChannel />
            </Suspense>
          </PrivateRoute>
        }
      />
      <Route
        path='/channel/add'
        element={
          <PrivateRoute>
            <Suspense fallback={<Loading></Loading>}>
              <EditChannel />
            </Suspense>
          </PrivateRoute>
        }
      />
      <Route
        path='/redemption'
        element={
          <PrivateRoute>
            <Redemption />
          </PrivateRoute>
        }
      />
      <Route
        path='/redemption/edit/:id'
        element={
          <PrivateRoute>
            <Suspense fallback={<Loading></Loading>}>
              <EditRedemption />
            </Suspense>
          </PrivateRoute>
        }
      />
      <Route
        path='/redemption/add'
        element={
          <PrivateRoute>
            <Suspense fallback={<Loading></Loading>}>
              <EditRedemption />
            </Suspense>
          </PrivateRoute>
        }
      />
      <Route
        path='/user'
        element={
          <PrivateRoute>
            <User />
          </PrivateRoute>
        }
      />
      <Route
        path='/user/edit/:id'
        element={
          <PrivateRoute>
            <Suspense fallback={<Loading></Loading>}>
              <EditUser />
            </Suspense>
          </PrivateRoute>
        }
      />
      <Route
        path='/user/edit'
        element={
          <PrivateRoute>
            <Suspense fallback={<Loading></Loading>}>
              <EditUser />
            </Suspense>
          </PrivateRoute>
        }
      />
      <Route
        path='/user/add'
        element={
          <PrivateRoute>
            <Suspense fallback={<Loading></Loading>}>
              <AddUser />
            </Suspense>
          </PrivateRoute>
        }
      />
      <Route
        path='/user/reset'
        element={
          <Suspense fallback={<Loading></Loading>}>
            <PasswordResetConfirm />
          </Suspense>
        }
      />
      <Route
        path='/login'
        element={
          <Suspense fallback={<Loading></Loading>}>
            <LoginForm />
          </Suspense>
        }
      />
      <Route
        path='/setup'
        element={
          <Suspense fallback={<Loading></Loading>}>
            <SetupForm />
          </Suspense>
        }
      />
      <Route
        path='/register'
        element={
          <Suspense fallback={<Loading></Loading>}>
            <RegisterForm />
          </Suspense>
        }
      />
      <Route
        path='/reset'
        element={
          <Suspense fallback={<Loading></Loading>}>
            <PasswordResetForm />
          </Suspense>
        }
      />
      <Route
        path='/oauth/github'
        element={
          <Suspense fallback={<Loading></Loading>}>
            <GitHubOAuth />
          </Suspense>
        }
      />
      <Route
        path='/oauth/lark'
        element={
          <Suspense fallback={<Loading></Loading>}>
            <LarkOAuth />
          </Suspense>
        }
      />
      <Route
        path='/setting'
        element={
          <PrivateRoute>
            <Suspense fallback={<Loading></Loading>}>
              <Setting />
            </Suspense>
          </PrivateRoute>
        }
      />
      <Route
        path='/log'
        element={
          <PrivateRoute>
            <Log />
          </PrivateRoute>
        }
      />
      <Route
        path='/about'
        element={
          <Suspense fallback={<Loading></Loading>}>
            <About />
          </Suspense>
        }
      />
      <Route
        path='/dashboard'
        element={
          <PrivateRoute>
            <Dashboard />
          </PrivateRoute>
        }
      />
      <Route path='*' element={<NotFound />} />
    </Routes>
    </ErrorBoundary>
  );
}

export default App;
