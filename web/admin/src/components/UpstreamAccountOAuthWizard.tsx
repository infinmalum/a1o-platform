import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Alert,
  Button,
  Checkbox,
  Input,
  Modal,
  Radio,
  Space,
  Steps,
  Tag,
  Typography,
} from 'antd';
import {
  ClockCircleOutlined,
  CopyOutlined,
  ExportOutlined,
  RedoOutlined,
} from '@ant-design/icons';
import { API, copy, showError, showSuccess } from '../helpers';

interface OAuthSession {
  session_id: string;
  authorize_url: string;
  redirect_uri: string;
  expires_in: number;
}

// PROFILE_OPTIONS mirrors the subscription profiles the backend registers in
// relay/upstream/profile.go.
const PROFILE_OPTIONS = [
  { value: 'chatgpt_plan_public', labelKey: 'chatgpt', color: 'green' },
  { value: 'anthropic_claude_code', labelKey: 'claude', color: 'orange' },
  { value: 'gemini_cli', labelKey: 'gemini', color: 'blue' },
];

interface Props {
  open: boolean;
  // Reconnect an existing account; empty starts a fresh connection.
  accountId?: number;
  accountName?: string;
  // Fixed profile for reconnects; selectable for fresh connections.
  profile?: string;
  onClose: () => void;
  onCompleted: () => void;
}

// UpstreamAccountOAuthWizard walks an administrator through a subscription
// OAuth flow (ChatGPT / Claude Code / Gemini CLI): the server prepares a
// pending authorization session, the admin signs in in the browser, and
// pastes the callback URL (or code) back so the gateway can finish the
// exchange.
const UpstreamAccountOAuthWizard = ({
  open,
  accountId,
  accountName,
  profile,
  onClose,
  onCompleted,
}: Props) => {
  const { t } = useTranslation();
  const [name, setName] = useState('');
  const [selectedProfile, setSelectedProfile] = useState(
    profile || 'chatgpt_plan_public'
  );
  const [disclaimerAgreed, setDisclaimerAgreed] = useState(false);
  const [session, setSession] = useState<OAuthSession | null>(null);
  const [callbackUrl, setCallbackUrl] = useState('');
  const [starting, setStarting] = useState(false);
  const [completing, setCompleting] = useState(false);

  useEffect(() => {
    if (open) {
      setName('');
      setSelectedProfile(profile || 'chatgpt_plan_public');
      setDisclaimerAgreed(false);
      setSession(null);
      setCallbackUrl('');
    }
  }, [open, profile]);

  const startAuth = async () => {
    setStarting(true);
    try {
      const res = await API.post('/api/upstream_account/oauth/session', {
        profile: selectedProfile,
        account_id: accountId || undefined,
      });
      const { success, message, data } = res.data;
      if (success) {
        setSession(data);
        setCallbackUrl('');
      } else {
        showError(message);
      }
    } catch (error) {
      showError(error.message);
    }
    setStarting(false);
  };

  const completeAuth = async () => {
    const pasted = callbackUrl.trim();
    if (!pasted) {
      showError(t('upstreamAccount.wizard.callbackRequired'));
      return;
    }
    setCompleting(true);
    try {
      const res = await API.post('/api/upstream_account/oauth/complete', {
        session_id: session!.session_id,
        callback_url: pasted,
        name: name.trim(),
      });
      const { success, message } = res.data;
      if (success) {
        showSuccess(
          accountId
            ? t('upstreamAccount.wizard.reconnectSuccess')
            : t('upstreamAccount.wizard.success')
        );
        onCompleted();
        onClose();
      } else {
        showError(message);
        // The pending session was consumed by the attempt, successful or
        // not; the administrator needs a fresh authorization link.
        setSession(null);
        setCallbackUrl('');
      }
    } catch (error) {
      showError(error.message);
    }
    setCompleting(false);
  };

  const minutes = session ? Math.round(session.expires_in / 60) : 0;
  const stepKey = (index: number) => {
    const option = PROFILE_OPTIONS.find(
      (candidate) => candidate.value === selectedProfile
    );
    return `upstreamAccount.wizard.steps.${option ? option.labelKey : 'chatgpt'}${index}`;
  };
  const callbackPlaceholder = () => {
    if (selectedProfile === 'anthropic_claude_code') {
      return t('upstreamAccount.wizard.callbackPlaceholderClaude');
    }
    if (selectedProfile === 'gemini_cli') {
      return t('upstreamAccount.wizard.callbackPlaceholderGemini');
    }
    return t('upstreamAccount.wizard.callbackPlaceholder');
  };

  return (
    <Modal
      open={open}
      onCancel={onClose}
      title={
        accountId
          ? `${t('upstreamAccount.wizard.titleReconnect')}${accountName ? `：${accountName}` : ''}`
          : t('upstreamAccount.wizard.titleConnect')
      }
      width={560}
      footer={[
        session && (
          <Button
            key='regenerate'
            icon={<RedoOutlined />}
            onClick={() => {
              setSession(null);
              setCallbackUrl('');
            }}
          >
            {t('upstreamAccount.wizard.regenerate')}
          </Button>
        ),
        session && (
          <Button
            key='complete'
            type='primary'
            loading={completing}
            onClick={completeAuth}
          >
            {t('upstreamAccount.wizard.complete')}
          </Button>
        ),
        <Button key='cancel' onClick={onClose}>
          {t('upstreamAccount.wizard.cancel')}
        </Button>,
      ].filter(Boolean)}
    >
      {!session ? (
        <div>
          {!accountId && (
            <div style={{ marginBottom: 16 }}>
              <div style={{ marginBottom: 8 }}>
                {t('upstreamAccount.wizard.profileLabel')}
              </div>
              <Radio.Group
                value={selectedProfile}
                onChange={(e) => setSelectedProfile(e.target.value)}
              >
                {PROFILE_OPTIONS.map((option) => (
                  <Radio key={option.value} value={option.value}>
                    {t(`upstreamAccount.profiles.${option.labelKey}`)}
                  </Radio>
                ))}
              </Radio.Group>
            </div>
          )}
          {accountId && (
            <div style={{ marginBottom: 16 }}>
              <Tag color='blue'>
                {t(
                  `upstreamAccount.profiles.${
                    (
                      PROFILE_OPTIONS.find(
                        (candidate) => candidate.value === profile
                      ) || PROFILE_OPTIONS[0]
                    ).labelKey
                  }`
                )}
              </Tag>
            </div>
          )}
          {!accountId && (
            <div style={{ marginBottom: 16 }}>
              <div style={{ marginBottom: 8 }}>
                {t('upstreamAccount.wizard.name')}
              </div>
              <Input
                placeholder={t('upstreamAccount.wizard.namePlaceholder')}
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
            </div>
          )}
          <Alert
            type='warning'
            showIcon
            message={<span>{t('upstreamAccount.disclaimer')}</span>}
            style={{ marginBottom: 12 }}
          />
          <Checkbox
            checked={disclaimerAgreed}
            onChange={(e) => setDisclaimerAgreed(e.target.checked)}
            style={{ display: 'block', marginBottom: 12 }}
          >
            {t('upstreamAccount.wizard.disclaimerAgree')}
          </Checkbox>
          <Button
            type='primary'
            loading={starting}
            disabled={starting || !disclaimerAgreed}
            onClick={startAuth}
          >
            {t('upstreamAccount.wizard.generate')}
          </Button>
        </div>
      ) : (
        <div>
          <Steps
            direction='vertical'
            size='small'
            current={-1}
            items={[
              { title: t(stepKey(1)) },
              { title: t(stepKey(2)) },
              { title: t(stepKey(3)) },
            ]}
            style={{ marginBottom: 16 }}
          />
          <Space wrap style={{ marginBottom: 12 }}>
            <Button type='primary' href={session.authorize_url} target='_blank' rel='noreferrer' icon={<ExportOutlined />}>
              {t('upstreamAccount.wizard.openAuthorize')}
            </Button>
            <Button
              icon={<CopyOutlined />}
              onClick={async () => {
                if (await copy(session.authorize_url)) {
                  showSuccess(t('upstreamAccount.wizard.copySuccess'));
                }
              }}
            >
              {t('upstreamAccount.wizard.copyLink')}
            </Button>
          </Space>
          <Typography.Text type='secondary' style={{ display: 'block', marginBottom: 12 }}>
            <ClockCircleOutlined /> {t('upstreamAccount.wizard.expiresIn', { minutes })}
          </Typography.Text>
          <div>
            <div style={{ marginBottom: 8 }}>
              {t('upstreamAccount.wizard.callbackLabel')}
            </div>
            <Input.TextArea
              rows={3}
              placeholder={callbackPlaceholder()}
              value={callbackUrl}
              onChange={(e) => setCallbackUrl(e.target.value)}
            />
          </div>
        </div>
      )}
    </Modal>
  );
};

export default UpstreamAccountOAuthWizard;
