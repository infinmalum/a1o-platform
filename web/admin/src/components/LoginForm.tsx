import React, { useContext, useEffect, useState } from 'react';
import { Button, Card, Divider, Form, Input } from 'antd';
import { GithubOutlined, LockOutlined, UserOutlined } from '@ant-design/icons';
import { Link, useLocation, useNavigate, useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { UserContext } from '../context/User';
import { API, getLogo, isAdmin, showError, showSuccess } from '../helpers';
import { onGitHubOAuthClicked, onLarkOAuthClicked } from './utils';
import larkIcon from '../images/lark.svg';

const LoginForm = () => {
  const { t } = useTranslation();
  const [form] = Form.useForm();
  const [searchParams] = useSearchParams();
  const [userState, userDispatch] = useContext(UserContext);
  const navigate = useNavigate();
  const location = useLocation();
  const [status, setStatus] = useState<import('../types/site').SiteStatus>({});
  const logo = getLogo();

  // PrivateRoute records the page that bounced the visitor to /login, so a
  // sign-in from /chat returns to /chat instead of always landing on /token.
  const redirectAfterLogin = (fallback: string) => {
    const from = (
      location.state as { from?: { pathname?: string; search?: string } } | null
    )?.from;
    if (from?.pathname && from.pathname !== '/login') {
      navigate(`${from.pathname}${from.search ?? ''}`, { replace: true });
      return;
    }
    navigate(fallback);
  };

  // Where a sign-in lands when the visitor was not sent here from a page:
  // administrators start on the dashboard, other accounts on the channels list
  // (this console is operations-only; end users use the Studio app).
  const landingPage = () => {
    return isAdmin() ? '/dashboard' : '/channel';
  };

  useEffect(() => {
    if (searchParams.get('expired')) {
      showError(t('messages.error.login_expired'));
    }
    const storedStatus = localStorage.getItem('status');
    if (storedStatus) {
      setStatus(JSON.parse(storedStatus) as import('../types/site').SiteStatus);
    }
  }, []);

  async function handleSubmit(values) {
    try {
      const res = await API.post(`/api/user/login`, values);
      const { success, message, data } = res.data;
      if (success) {
        userDispatch({ type: 'login', payload: data });
        localStorage.setItem('user', JSON.stringify(data));
        redirectAfterLogin(landingPage());
        showSuccess(t('messages.success.login'));
      } else {
        showError(message);
      }
    } catch (error) {
      // interceptor toasts the error
    }
  }

  return (
    <div style={{ display: 'flex', justifyContent: 'center', paddingTop: 48 }}>
      <Card
        style={{
          width: '100%',
          maxWidth: 450,
          boxShadow: '0 8px 24px rgba(0, 0, 0, 0.08)',
          borderRadius: 16,
        }}
      >
        <div style={{ textAlign: 'center', marginBottom: 24 }}>
          <img
            src={logo}
            alt='logo'
            style={{
              width: 64,
              height: 64,
              objectFit: 'contain',
              marginBottom: 10,
            }}
          />
          <h2 style={{ margin: 0 }}>{t('auth.login.title')}</h2>
        </div>
        <Form form={form} size='large' onFinish={handleSubmit}>
          <Form.Item name='username' rules={[{ required: true, message: t('auth.login.username') }]}>
            <Input
              prefix={<UserOutlined />}
              placeholder={t('auth.login.username')}
              autoComplete='username'
            />
          </Form.Item>
          <Form.Item name='password' rules={[{ required: true, message: t('auth.login.password') }]}>
            <Input.Password
              prefix={<LockOutlined />}
              placeholder={t('auth.login.password')}
              autoComplete='current-password'
            />
          </Form.Item>
          <Form.Item>
            <Button type='primary' htmlType='submit' size='large' block>
              {t('auth.login.button')}
            </Button>
          </Form.Item>
        </Form>

        <Divider style={{ margin: '12px 0' }} />
        <div
          style={{
            display: 'flex',
            justifyContent: 'space-between',
            fontSize: '0.9em',
            color: '#666',
          }}
        >
          <div>
            {t('auth.login.forgot_password')}
            <Link to='/reset' style={{ marginLeft: 2 }}>
              {t('auth.login.reset_password')}
            </Link>
          </div>
          <div>
            {t('auth.login.no_account')}
            <Link to='/register' style={{ marginLeft: 2 }}>
              {t('auth.login.register')}
            </Link>
          </div>
        </div>

        {(status.github_oauth || status.lark_client_id) && (
          <>
            <Divider plain style={{ color: '#666', fontSize: '0.9em' }}>
              {t('auth.login.other_methods')}
            </Divider>
            <div
              style={{
                display: 'flex',
                justifyContent: 'center',
                gap: '1em',
                marginTop: '1em',
              }}
            >
              {status.github_oauth && (
                <Button
                  shape='circle'
                  icon={<GithubOutlined />}
                  size='large'
                  onClick={() => onGitHubOAuthClicked(status.github_client_id)}
                />
              )}
              {status.lark_client_id && (
                <Button
                  shape='circle'
                  size='large'
                  onClick={() => onLarkOAuthClicked(status.lark_client_id)}
                  icon={
                    <img
                      src={larkIcon}
                      alt='lark'
                      style={{ width: 18, height: 18 }}
                    />
                  }
                />
              )}
            </div>
          </>
        )}
      </Card>
    </div>
  );
};

export default LoginForm;
