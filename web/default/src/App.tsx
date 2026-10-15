import React, { lazy, Suspense, useContext, useEffect, useState } from 'react';
import { Route, Routes, useNavigate } from 'react-router-dom';
import Loading from './components/Loading';
import User from './pages/User';
import { PrivateRoute } from './components/PrivateRoute';
import RegisterForm from './components/RegisterForm';
import LoginForm from './components/LoginForm';
import NotFound from './pages/NotFound';
import Setting from './pages/Setting';
import EditUser from './pages/User/EditUser';
import AddUser from './pages/User/AddUser';
import { API, getLogo, getSystemName, isAdmin, showError, showNotice, landingForMode } from './helpers';
import type { UiMode } from './helpers';
import ModeChooser from './components/ModeChooser';
import PasswordResetForm from './components/PasswordResetForm';
import GitHubOAuth from './components/GitHubOAuth';
import PasswordResetConfirm from './components/PasswordResetConfirm';
import { UserContext } from './context/User';
import { StatusContext } from './context/Status';
import Channel from './pages/Channel';
import UpstreamAccount from './pages/UpstreamAccount';
import Token from './pages/Token';
import EditToken from './pages/Token/EditToken';
import EditChannel from './pages/Channel/EditChannel';
import Redemption from './pages/Redemption';
import EditRedemption from './pages/Redemption/EditRedemption';
import TopUp from './pages/TopUp';
import Log from './pages/Log';
import Chat from './pages/Chat';
import ImagePlayground from './pages/Playground/ImagePlayground';
import DecisionPlayground from './pages/Playground/DecisionPlayground';
import LarkOAuth from './components/LarkOAuth';
import Dashboard from './pages/Dashboard';

const Home = lazy(() => import('./pages/Home'));
const About = lazy(() => import('./pages/About'));
const QuickStart = lazy(() => import('./pages/QuickStart'));

function App() {
  const [userState, userDispatch] = useContext(UserContext);
  const [statusState, statusDispatch] = useContext(StatusContext);
  const [modeChooserOpen, setModeChooserOpen] = useState(false);
  const navigate = useNavigate();

  // Ask a new non-admin account once, after sign-in, what it is here for; the
  // choice trims the navigation and is changeable from the settings page.
  useEffect(() => {
    if (userState.user && !isAdmin() && !statusState.uiMode) {
      setModeChooserOpen(true);
    }
  }, [userState.user, statusState.uiMode]);

  const chooseMode = (mode: UiMode) => {
    statusDispatch({ type: 'setUiMode', payload: mode });
    setModeChooserOpen(false);
    navigate(landingForMode(mode));
  };

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
      const { success, message, data } = res.data || {}; // Add default empty object
      if (success && data) {
        // Check data exists
        localStorage.setItem('status', JSON.stringify(data));
        statusDispatch({ type: 'set', payload: data });
        localStorage.setItem('system_name', data.system_name);
        localStorage.setItem('logo', data.logo);
        localStorage.setItem('footer_html', data.footer_html);
        localStorage.setItem('quota_per_unit', data.quota_per_unit);
        localStorage.setItem('display_in_currency', data.display_in_currency);
        if (data.chat_link) {
          localStorage.setItem('chat_link', data.chat_link);
        } else {
          localStorage.removeItem('chat_link');
        }
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
      if (linkElement) {
        linkElement.href = logo;
      }
    }
  }, []);

  return (
    <>
      <ModeChooser open={modeChooserOpen} onChoose={chooseMode} />
      <Routes>
      <Route
        path='/'
        element={
          <Suspense fallback={<Loading></Loading>}>
            <Home />
          </Suspense>
        }
      />
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
        path='/token'
        element={
          <PrivateRoute>
            <Token />
          </PrivateRoute>
        }
      />
      <Route
        path='/token/edit/:id'
        element={
          <PrivateRoute>
            <Suspense fallback={<Loading></Loading>}>
              <EditToken />
            </Suspense>
          </PrivateRoute>
        }
      />
      <Route
        path='/token/add'
        element={
          <PrivateRoute>
            <Suspense fallback={<Loading></Loading>}>
              <EditToken />
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
        path='/topup'
        element={
          <PrivateRoute>
            <Suspense fallback={<Loading></Loading>}>
              <TopUp />
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
        path='/chat'
        element={
          <PrivateRoute>
            <Suspense fallback={<Loading></Loading>}>
              <Chat />
            </Suspense>
          </PrivateRoute>
        }
      />
      <Route
        path='/playground/image'
        element={
          <PrivateRoute>
            <ImagePlayground />
          </PrivateRoute>
        }
      />
      <Route
        path='/playground/decision'
        element={
          <PrivateRoute>
            <DecisionPlayground />
          </PrivateRoute>
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
      <Route
        path='/quickstart'
        element={
          <PrivateRoute>
            <Suspense fallback={<Loading></Loading>}>
              <QuickStart />
            </Suspense>
          </PrivateRoute>
        }
      />
      <Route path='*' element={<NotFound />} />
    </Routes>
    </>
  );
}

export default App;
