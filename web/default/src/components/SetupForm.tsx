import React, { useContext, useEffect, useState } from 'react';
import { Button, Card, Form, Grid, Header, Image, Message } from 'semantic-ui-react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { UserContext } from '../context/User';
import { API, getLogo, showError, showSuccess } from '../helpers';

// SetupForm runs once on a fresh installation: it creates the first root
// account and signs the operator in. The backend refuses once any user
// exists, and the guard below bounces late visitors to /login.
const SetupForm = () => {
  const { t } = useTranslation();
  const [inputs, setInputs] = useState({ username: '', password: '', confirm: '', email: '' });
  const [submitted, setSubmitted] = useState(false);
  const [loading, setLoading] = useState(false);
  const [, userDispatch] = useContext(UserContext);
  const navigate = useNavigate();
  const logo = getLogo();

  useEffect(() => {
    const storedStatus = localStorage.getItem('status');
    if (storedStatus) {
      try {
        const status = JSON.parse(storedStatus);
        // An already-initialized instance has nothing to set up.
        if (status.initialized !== false) {
          navigate('/login', { replace: true });
        }
      } catch {
        // fresh visit without status: fall through to the form
      }
    }
  }, [navigate]);

  const handleChange = (event, { name, value }) => {
    setInputs((inputs) => ({ ...inputs, [name]: value }));
  };

  const passwordMismatch = inputs.confirm !== inputs.password;

  async function handleSubmit() {
    setSubmitted(true);
    if (!inputs.username || !inputs.password || passwordMismatch) {
      return;
    }
    setLoading(true);
    try {
      const res = await API.post('/api/setup', {
        username: inputs.username,
        password: inputs.password,
        email: inputs.email,
      });
      const { success, message } = res.data;
      if (!success) {
        showError(message);
        return;
      }
      // Sign in with the credentials just created, so the operator lands on
      // a working console instead of a login form.
      const login = await API.post('/api/user/login', {
        username: inputs.username,
        password: inputs.password,
      });
      if (login.data.success) {
        userDispatch({ type: 'login', payload: login.data.data });
        localStorage.setItem('user', JSON.stringify(login.data.data));
        showSuccess(t('setup.success'));
        navigate('/dashboard', { replace: true });
      } else {
        showSuccess(t('setup.success'));
        showError(login.data.message);
        navigate('/login', { replace: true });
      }
    } catch (error) {
      showError(error.message);
    } finally {
      setLoading(false);
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
              <Header as='h2' textAlign='center' style={{ marginBottom: '1em' }}>
                <Image
                  src={logo}
                  style={{
                    width: '64px',
                    height: '64px',
                    objectFit: 'contain',
                    margin: '0 auto 10px',
                  }}
                />
                <Header.Content>{t('setup.title')}</Header.Content>
              </Header>
            </Card.Header>
            <p style={{ color: '#666', marginBottom: '1.5em' }}>{t('setup.subtitle')}</p>
            <Form size='large' loading={loading}>
              <Form.Input
                fluid
                icon='user'
                iconPosition='left'
                placeholder={t('setup.username')}
                name='username'
                value={inputs.username}
                onChange={handleChange}
                error={submitted && !inputs.username}
                style={{ marginBottom: '1em' }}
              />
              <Form.Input
                fluid
                icon='lock'
                iconPosition='left'
                placeholder={t('setup.password')}
                name='password'
                type='password'
                value={inputs.password}
                onChange={handleChange}
                error={submitted && (!inputs.password || inputs.password.length < 8)}
                style={{ marginBottom: '1em' }}
              />
              <Form.Input
                fluid
                icon='lock'
                iconPosition='left'
                placeholder={t('setup.confirmPassword')}
                name='confirm'
                type='password'
                value={inputs.confirm}
                onChange={handleChange}
                error={submitted && passwordMismatch}
                style={{ marginBottom: '1em' }}
              />
              <Form.Input
                fluid
                icon='mail'
                iconPosition='left'
                placeholder={t('setup.email')}
                name='email'
                value={inputs.email}
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
                  marginBottom: '1em',
                }}
                onClick={handleSubmit}
              >
                {t('setup.submit')}
              </Button>
            </Form>
            {submitted && passwordMismatch && (
              <Message negative size='small'>
                {t('setup.passwordMismatch')}
              </Message>
            )}
          </Card.Content>
        </Card>
      </Grid.Column>
    </Grid>
  );
};

export default SetupForm;
