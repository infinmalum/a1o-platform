import React, { useContext, useEffect, useState } from 'react';
import { Button, Card, Form, Input } from 'antd';
import { LockOutlined, MailOutlined, UserOutlined } from '@ant-design/icons';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { UserContext } from '../context/User';
import { API, getLogo, showError, showInfo, showSuccess } from '../helpers';

// SetupForm runs once on a fresh installation: it creates the first root
// account and signs the operator in. The backend refuses once any user
// exists, and the guard below bounces late visitors to /login.
const SetupForm = () => {
  const { t } = useTranslation();
  const [form] = Form.useForm();
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

  async function handleSubmit(values) {
    if (values.password.length < 8) {
      // The same floor the register form and the backend validate entry
      // enforce; without this guard a short password reached the server
      // and only failed there.
      showInfo(t('messages.error.password_length'));
      return;
    }
    setLoading(true);
    try {
      const res = await API.post('/api/setup', {
        username: values.username,
        password: values.password,
        email: values.email,
      });
      const { success, message } = res.data;
      if (!success) {
        showError(message);
        return;
      }
      // Sign in with the credentials just created, so the operator lands on
      // a working console instead of a login form.
      const login = await API.post('/api/user/login', {
        username: values.username,
        password: values.password,
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
      // interceptor toasts the error
    } finally {
      setLoading(false);
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
        <div style={{ textAlign: 'center', marginBottom: 16 }}>
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
          <h2 style={{ margin: 0 }}>{t('setup.title')}</h2>
        </div>
        <p style={{ color: '#666', marginBottom: '1.5em', textAlign: 'center' }}>
          {t('setup.subtitle')}
        </p>
        <Form form={form} size='large' onFinish={handleSubmit}>
          <Form.Item name='username' rules={[{ required: true, message: t('setup.username') }]}>
            <Input prefix={<UserOutlined />} placeholder={t('setup.username')} />
          </Form.Item>
          <Form.Item
            name='password'
            rules={[
              { required: true, message: t('setup.password') },
              { min: 8, message: t('messages.error.password_length') },
            ]}
          >
            <Input.Password prefix={<LockOutlined />} placeholder={t('setup.password')} />
          </Form.Item>
          <Form.Item
            name='confirm'
            dependencies={['password']}
            rules={[
              { required: true, message: t('setup.confirmPassword') },
              ({ getFieldValue }) => ({
                validator(_, value) {
                  if (!value || getFieldValue('password') === value) {
                    return Promise.resolve();
                  }
                  return Promise.reject(new Error(t('setup.passwordMismatch')));
                },
              }),
            ]}
          >
            <Input.Password prefix={<LockOutlined />} placeholder={t('setup.confirmPassword')} />
          </Form.Item>
          <Form.Item name='email'>
            <Input prefix={<MailOutlined />} placeholder={t('setup.email')} type='email' />
          </Form.Item>
          <Form.Item>
            <Button type='primary' size='large' block htmlType='submit' loading={loading}>
              {t('setup.submit')}
            </Button>
          </Form.Item>
        </Form>
      </Card>
    </div>
  );
};

export default SetupForm;
