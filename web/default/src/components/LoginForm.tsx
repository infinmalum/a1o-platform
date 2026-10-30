import React, { useContext, useEffect, useState } from 'react';
import {
  Button,
  Divider,
  Form,
  Grid,
  Header,
  Image,
  Message,
  Segment,
  Card,
} from 'semantic-ui-react';
import { Link, useLocation, useNavigate, useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { UserContext } from '../context/User';
import { API, getLogo, isAdmin, getUiMode, landingForMode, showError, showSuccess } from '../helpers';
import { onGitHubOAuthClicked, onLarkOAuthClicked } from './utils';
import larkIcon from '../images/lark.svg';

const LoginForm = () => {
  const { t } = useTranslation();
  const [inputs, setInputs] = useState({
    username: '',
    password: '',
  });
  const [searchParams, setSearchParams] = useSearchParams();
  const [submitted, setSubmitted] = useState(false);
  const { username, password } = inputs;
  const [userState, userDispatch] = useContext(UserContext);
  let navigate = useNavigate();
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
  // administrators start on the dashboard, and everyone else on the page their
  // chosen mode is about.
  const landingPage = () => {
    if (isAdmin()) {
      return '/dashboard';
    }
    const mode = getUiMode();
    return mode ? landingForMode(mode) : '/quickstart';
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

  function handleChange(e) {
    const { name, value } = e.target;
    setInputs((inputs) => ({ ...inputs, [name]: value }));
  }

  async function handleSubmit(e) {
    setSubmitted(true);
    if (username && password) {
      try {
        const res = await API.post(`/api/user/login`, {
          username,
          password,
        });
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
  }

  return (
    <Grid textAlign='center' style={{ marginTop: '48px' }}>
      <Grid.Column style={{ maxWidth: 450 }}>
        <Card
          fluid
          className='chart-card'
          style={{
            boxShadow: '0 8px 24px rgba(33, 136, 208, 0.10)',
            borderRadius: 16,
            padding: '1em 0.5em',
          }}
        >
          <Card.Content>
            <Card.Header>
              <Header
                as='h2'
                textAlign='center'
                style={{ marginBottom: '1.5em' }}
              >
                <Image
                  src={logo}
                  style={{
                    width: '64px',
                    height: '64px',
                    objectFit: 'contain',
                    margin: '0 auto 10px',
                  }}
                />
                <Header.Content>{t('auth.login.title')}</Header.Content>
              </Header>
            </Card.Header>
            <Form size='large'>
              <Form.Input
                fluid
                icon='user'
                iconPosition='left'
                placeholder={t('auth.login.username')}
                name='username'
                value={username}
                onChange={handleChange}
                style={{ marginBottom: '1em' }}
              />
              <Form.Input
                fluid
                icon='lock'
                iconPosition='left'
                placeholder={t('auth.login.password')}
                name='password'
                type='password'
                value={password}
                onChange={handleChange}
                style={{ marginBottom: '1.5em' }}
              />
              <Button
                fluid
                size='large'
                style={{
                  background: '#2185d0',
                  color: 'white',
                  borderRadius: 8,
                  marginBottom: '1.5em',
                }}
                onClick={handleSubmit}
              >
                {t('auth.login.button')}
              </Button>
            </Form>

            <Divider />
            <Message style={{ background: 'transparent', boxShadow: 'none' }}>
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
                  <Link
                    to='/reset'
                    style={{ color: '#2185d0', marginLeft: '2px' }}
                  >
                    {t('auth.login.reset_password')}
                  </Link>
                </div>
                <div>
                  {t('auth.login.no_account')}
                  <Link
                    to='/register'
                    style={{ color: '#2185d0', marginLeft: '2px' }}
                  >
                    {t('auth.login.register')}
                  </Link>
                </div>
              </div>
            </Message>

            {(status.github_oauth || status.lark_client_id) && (
              <>
                <Divider
                  horizontal
                  style={{ color: '#666', fontSize: '0.9em' }}
                >
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
                      circular
                      color='black'
                      icon='github'
                      onClick={() =>
                        onGitHubOAuthClicked(status.github_client_id)
                      }
                    />
                  )}
                  {status.lark_client_id && (
                    <div
                      style={{
                        background: '#FFFFFF',
                        border: '1px solid rgba(0,0,0,0.08)',
                        width: '36px',
                        height: '36px',
                        borderRadius: '10em',
                        display: 'flex',
                        cursor: 'pointer',
                      }}
                      onClick={() => onLarkOAuthClicked(status.lark_client_id)}
                    >
                      <Image
                        src={larkIcon}
                        avatar
                        style={{
                          width: '36px',
                          height: '36px',
                          cursor: 'pointer',
                          margin: 'auto',
                        }}
                      />
                    </div>
                  )}
                </div>
              </>
            )}
          </Card.Content>
        </Card>
      </Grid.Column>
    </Grid>
  );
};

export default LoginForm;
