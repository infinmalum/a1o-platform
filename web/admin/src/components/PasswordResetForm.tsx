import React, { useEffect, useState } from 'react';
import { Button, Card, Form, Input } from 'antd';
import { MailOutlined } from '@ant-design/icons';
import { useTranslation } from 'react-i18next';
import { API, getLogo, showError, showInfo, showSuccess } from '../helpers';
import Turnstile from 'react-turnstile';

const PasswordResetForm = () => {
  const { t } = useTranslation();
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(false);
  const [turnstileEnabled, setTurnstileEnabled] = useState(false);
  const [turnstileSiteKey, setTurnstileSiteKey] = useState('');
  const [turnstileToken, setTurnstileToken] = useState('');
  const [disableButton, setDisableButton] = useState(false);
  const [countdown, setCountdown] = useState(30);
  const logo = getLogo();

  useEffect(() => {
    const storedStatus = localStorage.getItem('status');
    if (storedStatus) {
      const status = JSON.parse(storedStatus) as {
        turnstile_check?: boolean;
        turnstile_site_key?: string;
      };
      if (status.turnstile_check) {
        setTurnstileEnabled(true);
        setTurnstileSiteKey(status.turnstile_site_key ?? '');
      }
    }
  }, []);

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

  async function handleSubmit(values) {
    setDisableButton(true);
    if (turnstileEnabled && turnstileToken === '') {
      showInfo('请稍后几秒重试，Turnstile 正在检查用户环境！');
      return;
    }
    setLoading(true);
    const res = await API.get(
      `/api/reset_password?email=${values.email}&turnstile=${turnstileToken}`
    );
    const { success, message } = res.data;
    if (success) {
      showSuccess(t('auth.reset.notice'));
      form.resetFields();
    } else {
      showError(message);
      setDisableButton(false);
      setCountdown(30);
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
          <h2 style={{ margin: 0 }}>{t('auth.reset.title')}</h2>
        </div>
        <Form form={form} size='large' onFinish={handleSubmit}>
          <Form.Item name='email' rules={[{ required: true, message: t('auth.reset.email') }]}>
            <Input prefix={<MailOutlined />} placeholder={t('auth.reset.email')} type='email' />
          </Form.Item>
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
            <Button
              type='primary'
              size='large'
              htmlType='submit'
              loading={loading}
              disabled={disableButton}
              block
            >
              {disableButton
                ? t('auth.register.get_code_retry', { countdown })
                : t('auth.reset.button')}
            </Button>
          </Form.Item>
        </Form>
        <p style={{ fontSize: '0.9em', color: '#666', textAlign: 'center' }}>
          {t('auth.reset.notice')}
        </p>
      </Card>
    </div>
  );
};

export default PasswordResetForm;
