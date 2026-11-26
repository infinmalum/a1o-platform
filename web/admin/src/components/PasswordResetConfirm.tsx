import React, { useEffect, useState } from 'react';
import { Button, Card, Form, Input } from 'antd';
import { LockOutlined, MailOutlined } from '@ant-design/icons';
import { useTranslation } from 'react-i18next';
import { API, getLogo, showError, showSuccess } from '../helpers';
import { useSearchParams } from 'react-router-dom';

const PasswordResetConfirm = () => {
  const { t } = useTranslation();
  const [form] = Form.useForm();
  const [email, setEmail] = useState('');
  const [loading, setLoading] = useState(false);
  const [disableButton, setDisableButton] = useState(false);
  const [done, setDone] = useState(false);
  const logo = getLogo();

  const [countdown, setCountdown] = useState(30);

  const [searchParams] = useSearchParams();
  useEffect(() => {
    setEmail(searchParams.get('email') || '');
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

  async function handleSubmit(values) {
    if (!email || done) return;
    if (values.password.length < 8 || values.password.length > 20) {
      showError(t('auth.reset.confirm.password_rule'));
      return;
    }
    setDisableButton(true);
    setLoading(true);
    const res = await API.post('/api/user/reset', {
      email,
      token: searchParams.get('token') || '',
      password: values.password,
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
          <h2 style={{ margin: 0 }}>{t('auth.reset.confirm.title')}</h2>
        </div>
        <Form form={form} size='large' onFinish={handleSubmit}>
          <Form.Item name='email' initialValue={email}>
            <Input prefix={<MailOutlined />} placeholder={t('auth.reset.email')} readOnly />
          </Form.Item>
          <Form.Item
            name='password'
            rules={[{ required: true, message: t('auth.reset.confirm.new_password') }]}
          >
            <Input.Password
              prefix={<LockOutlined />}
              placeholder={t('auth.reset.confirm.new_password')}
              autoComplete='new-password'
              disabled={done}
            />
          </Form.Item>
          <Form.Item
            name='confirmPassword'
            dependencies={['password']}
            rules={[
              { required: true, message: t('auth.reset.confirm.confirm_password') },
              ({ getFieldValue }) => ({
                validator(_, value) {
                  if (!value || getFieldValue('password') === value) {
                    return Promise.resolve();
                  }
                  return Promise.reject(
                    new Error(t('auth.reset.confirm.password_mismatch'))
                  );
                },
              }),
            ]}
          >
            <Input.Password
              prefix={<LockOutlined />}
              placeholder={t('auth.reset.confirm.confirm_password')}
              autoComplete='new-password'
              disabled={done}
            />
          </Form.Item>
          <Form.Item>
            <Button
              type='primary'
              size='large'
              htmlType='submit'
              loading={loading}
              disabled={disableButton}
              block
            >
              {disableButton
                ? t('auth.reset.confirm.button_disabled')
                : t('auth.reset.confirm.button')}
            </Button>
          </Form.Item>
        </Form>
        <p style={{ fontSize: '0.9em', color: '#666', textAlign: 'center' }}>
          {done
            ? t('auth.reset.confirm.success_notice')
            : t('auth.reset.confirm.notice')}
        </p>
      </Card>
    </div>
  );
};

export default PasswordResetConfirm;
