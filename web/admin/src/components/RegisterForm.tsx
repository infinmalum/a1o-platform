import React, { useEffect, useState } from 'react';
import { Button, Card, Divider, Form, Input } from 'antd';
import { LockOutlined, MailOutlined, UserOutlined } from '@ant-design/icons';
import { Link, useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { API, getLogo, showError, showInfo, showSuccess } from '../helpers';
import Turnstile from 'react-turnstile';

const RegisterForm = () => {
  const { t } = useTranslation();
  const [form] = Form.useForm();
  const [showEmailVerification, setShowEmailVerification] = useState(false);
  const [turnstileEnabled, setTurnstileEnabled] = useState(false);
  const [turnstileSiteKey, setTurnstileSiteKey] = useState('');
  const [turnstileToken, setTurnstileToken] = useState('');
  const [loading, setLoading] = useState(false);
  const [disableButton, setDisableButton] = useState(false);
  const [countdown, setCountdown] = useState(30);
  const logo = getLogo();
  let affCode = new URLSearchParams(window.location.search).get('aff');
  if (affCode) {
    localStorage.setItem('aff', affCode);
  }

  useEffect(() => {
    const storedStatus = localStorage.getItem('status');
    if (storedStatus) {
      const status = JSON.parse(storedStatus) as {
        email_verification?: boolean;
        turnstile_check?: boolean;
        turnstile_site_key?: string;
      };
      setShowEmailVerification(Boolean(status.email_verification));
      if (status.turnstile_check) {
        setTurnstileEnabled(true);
        setTurnstileSiteKey(status.turnstile_site_key ?? '');
      }
    }
  });

  useEffect(() => {
    let countdownInterval = null;
    if (disableButton && countdown > 0) {
      countdownInterval = setInterval(() => {
        setCountdown(countdown - 1);
      }, 1000);
    } else if (countdown === 0) {
      setDisableButton(false);
      setCountdown(30);
    }
    return () => clearInterval(countdownInterval);
  }, [disableButton, countdown]);

  const navigate = useNavigate();

  async function handleSubmit(values) {
    if (values.password.length < 8) {
      showInfo(t('messages.error.password_length'));
      return;
    }
    if (turnstileEnabled && turnstileToken === '') {
      showInfo(t('messages.error.turnstile_wait'));
      return;
    }
    setLoading(true);
    if (!affCode) {
      affCode = localStorage.getItem('aff');
    }
    let res;
    try {
      res = await API.post(
        `/api/user/register?turnstile=${turnstileToken}`,
        { ...values, aff_code: affCode ?? '' }
      );
    } catch (error) {
      // interceptor toasts the error
      setLoading(false);
      return;
    }
    const { success, message } = res.data;
    if (success) {
      navigate('/login');
      showSuccess(t('messages.success.register'));
    } else {
      showError(message);
    }
    setLoading(false);
  }

  const sendVerificationCode = async () => {
    const email = form.getFieldValue('email');
    if (!email) return;
    if (turnstileEnabled && turnstileToken === '') {
      showInfo(t('messages.error.turnstile_wait'));
      return;
    }
    setDisableButton(true);
    setLoading(true);
    let res;
    try {
      res = await API.get(
        `/api/verification?email=${email}&turnstile=${turnstileToken}`
      );
    } catch (error) {
      // interceptor toasts the error
      setDisableButton(false);
      setLoading(false);
      return;
    }
    const { success, message } = res.data;
    if (success) {
      showSuccess(t('messages.success.verification_code'));
    } else {
      showError(message);
      setDisableButton(false);
      setCountdown(30);
    }
    setLoading(false);
  };

  return (
    <div style={{ display: 'flex', justifyContent: 'center', paddingTop: 48 }}>
      <Card
        style={{
          width: '100%',
          maxWidth: 450,
          boxShadow: '0 1px 3px rgba(0,0,0,0.12)',
        }}
      >
        <div style={{ textAlign: 'center', marginBottom: 24 }}>
          <img src={logo} alt='logo' style={{ marginBottom: 10 }} />
          <h2 style={{ margin: 0 }}>{t('auth.register.title')}</h2>
        </div>
        <Form form={form} size='large' onFinish={handleSubmit}>
          <Form.Item name='username' rules={[{ required: true, message: t('auth.register.username') }]}>
            <Input prefix={<UserOutlined />} placeholder={t('auth.register.username')} />
          </Form.Item>
          <Form.Item
            name='password'
            rules={[
              { required: true, message: t('auth.register.password') },
              { min: 8, message: t('messages.error.password_length') },
            ]}
          >
            <Input.Password prefix={<LockOutlined />} placeholder={t('auth.register.password')} />
          </Form.Item>
          <Form.Item
            name='password2'
            dependencies={['password']}
            rules={[
              { required: true, message: t('auth.register.confirm_password') },
              ({ getFieldValue }) => ({
                validator(_, value) {
                  if (!value || getFieldValue('password') === value) {
                    return Promise.resolve();
                  }
                  return Promise.reject(new Error(t('messages.error.password_mismatch')));
                },
              }),
            ]}
          >
            <Input.Password prefix={<LockOutlined />} placeholder={t('auth.register.confirm_password')} />
          </Form.Item>

          {showEmailVerification && (
            <>
              <Form.Item name='email' rules={[{ required: true, message: t('auth.register.email') }]}>
                <Input
                  prefix={<MailOutlined />}
                  placeholder={t('auth.register.email')}
                  type='email'
                  addonAfter={
                    <Button
                      type='link'
                      size='small'
                      style={{ padding: 0 }}
                      onClick={sendVerificationCode}
                      disabled={loading}
                    >
                      {disableButton
                        ? t('auth.register.get_code_retry', { countdown })
                        : t('auth.register.get_code')}
                    </Button>
                  }
                />
              </Form.Item>
              <Form.Item name='verification_code' rules={[{ required: true, message: t('auth.register.verification_code') }]}>
                <Input
                  prefix={<LockOutlined />}
                  placeholder={t('auth.register.verification_code')}
                />
              </Form.Item>
            </>
          )}

          {turnstileEnabled && (
            <div
              style={{
                marginBottom: '1em',
                display: 'flex',
                justifyContent: 'center',
              }}
            >
              <Turnstile
                sitekey={turnstileSiteKey}
                onVerify={(token) => {
                  setTurnstileToken(token);
                }}
              />
            </div>
          )}

          <Form.Item>
            <Button type='primary' size='large' block htmlType='submit' loading={loading}>
              {t('auth.register.button')}
            </Button>
          </Form.Item>
        </Form>

        <Divider style={{ margin: '12px 0' }} />
        <div style={{ textAlign: 'center', fontSize: '0.9em', color: '#666' }}>
          {t('auth.register.has_account')}
          <Link to='/login' style={{ marginLeft: 2 }}>
            {t('auth.register.login')}
          </Link>
        </div>
      </Card>
    </div>
  );
};

export default RegisterForm;
