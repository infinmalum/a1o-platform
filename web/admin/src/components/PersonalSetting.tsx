import React, { useContext, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Input, Modal, Space, Typography } from 'antd';
import { Link, useNavigate } from 'react-router-dom';
import {
  API,
  copy,
  isAdmin,
  showError,
  showInfo,
  showNotice,
  showSuccess,
} from '../helpers';
import Turnstile from 'react-turnstile';
import { UserContext } from '../context/User';
import { StatusContext } from '../context/Status';
import { onGitHubOAuthClicked, onLarkOAuthClicked } from './utils';

const PersonalSetting = () => {
  const { t } = useTranslation();
  const [userState, userDispatch] = useContext(UserContext);
  let navigate = useNavigate();

  const [inputs, setInputs] = useState({
    email_verification_code: '',
    email: '',
    self_account_deletion_confirmation: '',
  });
  const [status, setStatus] = useState<import('../types/site').SiteStatus>({});
  const [showEmailBindModal, setShowEmailBindModal] = useState(false);
  const [showAccountDeleteModal, setShowAccountDeleteModal] = useState(false);
  const [turnstileEnabled, setTurnstileEnabled] = useState(false);
  const [turnstileSiteKey, setTurnstileSiteKey] = useState('');
  const [turnstileToken, setTurnstileToken] = useState('');
  const [loading, setLoading] = useState(false);
  const [disableButton, setDisableButton] = useState(false);
  const [countdown, setCountdown] = useState(30);
  const [affLink, setAffLink] = useState('');
  const [systemToken, setSystemToken] = useState('');

  useEffect(() => {
    const storedStatus = localStorage.getItem('status');
    if (storedStatus) {
      const status = JSON.parse(storedStatus) as import('../types/site').SiteStatus;
      setStatus(status);
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
    return () => clearInterval(countdownInterval); // Clean up on unmount
  }, [disableButton, countdown]);

  const handleInputChange = (name, value) => {
    setInputs((inputs) => ({ ...inputs, [name]: value }));
  };

  const generateAccessToken = async () => {
    const res = await API.get('/api/user/token');
    const { success, message, data } = res.data;
    if (success) {
      setSystemToken(data);
      setAffLink('');
      await copy(data);
      showSuccess(`令牌已重置并已复制到剪贴板`);
    } else {
      showError(message);
    }
  };

  const getAffLink = async () => {
    const res = await API.get('/api/user/aff');
    const { success, message, data } = res.data;
    if (success) {
      let link = `${window.location.origin}/register?aff=${data}`;
      setAffLink(link);
      setSystemToken('');
      await copy(link);
      showSuccess(`邀请链接已复制到剪切板`);
    } else {
      showError(message);
    }
  };

  const handleAffLinkClick = async (e) => {
    e.target.select();
    await copy(e.target.value);
    showSuccess(`邀请链接已复制到剪切板`);
  };

  const handleSystemTokenClick = async (e) => {
    e.target.select();
    await copy(e.target.value);
    showSuccess(`系统令牌已复制到剪切板`);
  };

  const deleteAccount = async () => {
    if (inputs.self_account_deletion_confirmation !== userState.user.username) {
      showError('请输入你的账户名以确认删除！');
      return;
    }

    const res = await API.delete('/api/user/self');
    const { success, message } = res.data;

    if (success) {
      showSuccess('账户已删除！');
      await API.get('/api/user/logout');
      userDispatch({ type: 'logout' });
      localStorage.removeItem('user');
      navigate('/login');
    } else {
      showError(message);
    }
  };

  const sendVerificationCode = async () => {
    setDisableButton(true);
    if (inputs.email === '') return;
    if (turnstileEnabled && turnstileToken === '') {
      showInfo('请稍后几秒重试，Turnstile 正在检查用户环境！');
      return;
    }
    setLoading(true);
    const res = await API.get(
      `/api/verification?email=${inputs.email}&turnstile=${turnstileToken}`
    );
    const { success, message } = res.data;
    if (success) {
      showSuccess('验证码发送成功，请检查邮箱！');
    } else {
      showError(message);
    }
    setLoading(false);
  };

  const bindEmail = async () => {
    if (inputs.email_verification_code === '') return;
    setLoading(true);
    const res = await API.get(
      `/api/oauth/email/bind?email=${inputs.email}&code=${inputs.email_verification_code}`
    );
    const { success, message } = res.data;
    if (success) {
      showSuccess('邮箱账户绑定成功！');
      setShowEmailBindModal(false);
    } else {
      showError(message);
    }
    setLoading(false);
  };

  return (
    <div>
      <Typography.Title level={5}>
        {t('setting.personal.general.title')}
      </Typography.Title>
      <Alert
        type='info'
        showIcon
        message={t('setting.personal.general.system_token_notice')}
        style={{ marginBottom: 12 }}
      />
      <Space size={12} wrap style={{ marginBottom: 12 }}>
        <Link to={`/user/edit/`}>
          <Button>{t('setting.personal.general.buttons.update_profile')}</Button>
        </Link>
        <Button onClick={generateAccessToken}>
          {t('setting.personal.general.buttons.generate_token')}
        </Button>
        <Button onClick={getAffLink}>
          {t('setting.personal.general.buttons.copy_invite')}
        </Button>
        <Button
          danger
          onClick={() => {
            setShowAccountDeleteModal(true);
          }}
        >
          {t('setting.personal.general.buttons.delete_account')}
        </Button>
      </Space>
      {systemToken && (
        <Input
          readOnly
          value={systemToken}
          onClick={handleSystemTokenClick}
          style={{ maxWidth: 560, marginBottom: 8 }}
        />
      )}
      {affLink && (
        <Input
          readOnly
          value={affLink}
          onClick={handleAffLinkClick}
          style={{ maxWidth: 560 }}
        />
      )}
      <Typography.Title level={5} style={{ marginTop: 24 }}>
        {t('setting.personal.binding.title')}
      </Typography.Title>
      <Space size={12} wrap>
        {status.github_oauth && (
          <Button onClick={() => onGitHubOAuthClicked(status.github_client_id)}>
            {t('setting.personal.binding.buttons.bind_github')}
          </Button>
        )}
        {status.lark_client_id && (
          <Button onClick={() => onLarkOAuthClicked(status.lark_client_id)}>
            {t('setting.personal.binding.buttons.bind_lark')}
          </Button>
        )}
        <Button onClick={() => setShowEmailBindModal(true)}>
          {t('setting.personal.binding.buttons.bind_email')}
        </Button>
      </Space>

      <Modal
        open={showEmailBindModal}
        onCancel={() => setShowEmailBindModal(false)}
        title={t('setting.personal.binding.email.title')}
        footer={null}
      >
        <div style={{ marginBottom: 12 }}>
          <Space.Compact style={{ width: '100%' }}>
            <Input
              placeholder={t('setting.personal.binding.email.email_placeholder')}
              type='email'
              value={inputs.email}
              onChange={(e) => handleInputChange('email', e.target.value)}
            />
            <Button
              onClick={sendVerificationCode}
              disabled={disableButton || loading}
            >
              {disableButton
                ? t('setting.personal.binding.email.get_code_retry', {
                    countdown,
                  })
                : t('setting.personal.binding.email.get_code')}
            </Button>
          </Space.Compact>
        </div>
        <Input
          placeholder={t('setting.personal.binding.email.code_placeholder')}
          value={inputs.email_verification_code}
          onChange={(e) =>
            handleInputChange('email_verification_code', e.target.value)
          }
          style={{ marginBottom: 12 }}
        />
        {turnstileEnabled && (
          <Turnstile
            sitekey={turnstileSiteKey}
            onVerify={(token) => {
              setTurnstileToken(token);
            }}
          />
        )}
        <Space
          style={{ display: 'flex', justifyContent: 'flex-end', marginTop: 16 }}
        >
          <Button onClick={() => setShowEmailBindModal(false)}>
            {t('setting.personal.binding.email.cancel')}
          </Button>
          <Button type='primary' onClick={bindEmail} loading={loading}>
            {t('setting.personal.binding.email.bind')}
          </Button>
        </Space>
      </Modal>

      <Modal
        open={showAccountDeleteModal}
        onCancel={() => setShowAccountDeleteModal(false)}
        title={t('setting.personal.delete_account.title')}
        footer={null}
      >
        <Alert
          type='warning'
          showIcon
          message={t('setting.personal.delete_account.warning')}
          style={{ marginBottom: 12 }}
        />
        <Input
          placeholder={t('setting.personal.delete_account.confirm_placeholder', {
            username: userState?.user?.username,
          })}
          value={inputs.self_account_deletion_confirmation}
          onChange={(e) =>
            handleInputChange(
              'self_account_deletion_confirmation',
              e.target.value
            )
          }
          style={{ marginBottom: 12 }}
        />
        {turnstileEnabled && (
          <Turnstile
            sitekey={turnstileSiteKey}
            onVerify={(token) => {
              setTurnstileToken(token);
            }}
          />
        )}
        <Space
          style={{ display: 'flex', justifyContent: 'flex-end', marginTop: 16 }}
        >
          <Button onClick={() => setShowAccountDeleteModal(false)}>
            {t('setting.personal.delete_account.buttons.cancel')}
          </Button>
          <Button danger type='primary' onClick={deleteAccount} loading={loading}>
            {t('setting.personal.delete_account.buttons.confirm')}
          </Button>
        </Space>
      </Modal>
    </div>
  );
};

export default PersonalSetting;
