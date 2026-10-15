import React, { useEffect, useState } from 'react';
import {
  Button,
  Form,
  Grid,
  Header,
  Image,
  Card,
  Message,
} from 'semantic-ui-react';
import { useTranslation } from 'react-i18next';
import { API, getLogo, showError, showSuccess } from '../helpers';
import { useSearchParams } from 'react-router-dom';

const PasswordResetConfirm = () => {
  const { t } = useTranslation();
  const [inputs, setInputs] = useState({
    email: '',
    token: '',
    password: '',
    confirmPassword: '',
  });
  const { email, token, password, confirmPassword } = inputs;
  const [loading, setLoading] = useState(false);
  const [disableButton, setDisableButton] = useState(false);
  const [done, setDone] = useState(false);
  const logo = getLogo();

  const [countdown, setCountdown] = useState(30);

  const [searchParams] = useSearchParams();
  useEffect(() => {
    setInputs((prev) => ({
      ...prev,
      token: searchParams.get('token') || '',
      email: searchParams.get('email') || '',
    }));
  }, [searchParams]);

  useEffect(() => {
    let countdownInterval = null;
    if (disableButton && countdown > 0) {
      countdownInterval = setInterval(() => {
        setCountdown(countdown - 1);
      }, 1000);
    } else if (countdown === 0 && !done) {
      setDisableButton(false);
      setCountdown(30);
    }
    return () => clearInterval(countdownInterval);
  }, [disableButton, countdown, done]);

  async function handleSubmit() {
    if (!email || done) return;
    if (password.length < 8 || password.length > 20) {
      showError(t('auth.reset.confirm.password_rule'));
      return;
    }
    if (password !== confirmPassword) {
      showError(t('auth.reset.confirm.password_mismatch'));
      return;
    }
    setDisableButton(true);
    setLoading(true);
    const res = await API.post('/api/user/reset', {
      email,
      token,
      password,
    });
    const { success, message } = res.data;
    if (success) {
      setDone(true);
      showSuccess(t('auth.reset.confirm.success_notice'));
    } else {
      showError(message);
      if (!done) {
        setDisableButton(false);
        setCountdown(30);
      }
    }
    setLoading(false);
  }

  return (
    <Grid textAlign='center' style={{ marginTop: '48px' }}>
      <Grid.Column style={{ maxWidth: 450 }}>
        <Card
          fluid
          className='chart-card'
          style={{ boxShadow: '0 1px 3px rgba(0,0,0,0.12)' }}
        >
          <Card.Content>
            <Card.Header>
              <Header
                as='h2'
                textAlign='center'
                style={{ marginBottom: '1.5em' }}
              >
                <Image src={logo} style={{ marginBottom: '10px' }} />
                <Header.Content>{t('auth.reset.confirm.title')}</Header.Content>
              </Header>
            </Card.Header>
            <Form size='large'>
              <Form.Input
                fluid
                icon='mail'
                iconPosition='left'
                placeholder={t('auth.reset.email')}
                name='email'
                value={email}
                readOnly
                style={{ marginBottom: '1em' }}
              />
              <Form.Input
                fluid
                icon='lock'
                iconPosition='left'
                type='password'
                placeholder={t('auth.reset.confirm.new_password')}
                name='password'
                value={password}
                autoComplete='new-password'
                disabled={done}
                onChange={(e) =>
                  setInputs({ ...inputs, password: e.target.value })
                }
                style={{ marginBottom: '1em' }}
              />
              <Form.Input
                fluid
                icon='lock'
                iconPosition='left'
                type='password'
                placeholder={t('auth.reset.confirm.confirm_password')}
                name='confirmPassword'
                value={confirmPassword}
                autoComplete='new-password'
                disabled={done}
                onChange={(e) =>
                  setInputs({ ...inputs, confirmPassword: e.target.value })
                }
                style={{ marginBottom: '1em' }}
              />
              <Button
                fluid
                size='large'
                onClick={handleSubmit}
                loading={loading}
                disabled={disableButton}
                style={{
                  background: '#2F73FF',
                  color: 'white',
                  marginBottom: '1.5em',
                }}
              >
                {disableButton
                  ? t('auth.reset.confirm.button_disabled')
                  : t('auth.reset.confirm.button')}
              </Button>
            </Form>
            <Message style={{ background: 'transparent', boxShadow: 'none' }}>
              <p style={{ fontSize: '0.9em', color: '#666' }}>
                {done
                  ? t('auth.reset.confirm.success_notice')
                  : t('auth.reset.confirm.notice')}
              </p>
            </Message>
          </Card.Content>
        </Card>
      </Grid.Column>
    </Grid>
  );
};

export default PasswordResetConfirm;
