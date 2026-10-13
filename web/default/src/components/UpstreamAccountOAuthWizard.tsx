import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Button,
  Form,
  Header,
  Icon,
  List,
  Modal,
  Segment,
} from 'semantic-ui-react';
import { API, copy, showError, showSuccess } from '../helpers';

interface OAuthSession {
  session_id: string;
  authorize_url: string;
  redirect_uri: string;
  expires_in: number;
}

interface Props {
  open: boolean;
  // Reconnect an existing account; empty starts a fresh connection.
  accountId?: number;
  accountName?: string;
  onClose: () => void;
  onCompleted: () => void;
}

// UpstreamAccountOAuthWizard walks an administrator through the official
// Sign in with ChatGPT flow: the server prepares a pending authorization
// session, the admin signs in in the browser, and pastes the loopback
// callback URL back so the gateway can finish the exchange.
const UpstreamAccountOAuthWizard = ({
  open,
  accountId,
  accountName,
  onClose,
  onCompleted,
}: Props) => {
  const { t } = useTranslation();
  const [name, setName] = useState('');
  const [session, setSession] = useState<OAuthSession | null>(null);
  const [callbackUrl, setCallbackUrl] = useState('');
  const [starting, setStarting] = useState(false);
  const [completing, setCompleting] = useState(false);

  useEffect(() => {
    if (open) {
      setName('');
      setSession(null);
      setCallbackUrl('');
    }
  }, [open]);

  const startAuth = async () => {
    setStarting(true);
    try {
      const res = await API.post(
        '/api/upstream_account/oauth/session',
        accountId ? { account_id: accountId } : {}
      );
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

  return (
    <Modal open={open} onClose={onClose} size='small' closeIcon>
      <Header>
        {accountId
          ? `${t('upstreamAccount.wizard.titleReconnect')}${accountName ? `：${accountName}` : ''}`
          : t('upstreamAccount.wizard.titleConnect')}
      </Header>
      <Modal.Content>
        {!session ? (
          <Form>
            {!accountId && (
              <Form.Input
                label={t('upstreamAccount.wizard.name')}
                placeholder={t('upstreamAccount.wizard.namePlaceholder')}
                value={name}
                onChange={(e, { value }) => setName(value)}
              />
            )}
            <Button
              primary
              loading={starting}
              disabled={starting}
              onClick={startAuth}
            >
              {t('upstreamAccount.wizard.generate')}
            </Button>
          </Form>
        ) : (
          <Segment>
            <List ordered relaxed>
              <List.Item>{t('upstreamAccount.wizard.step1')}</List.Item>
              <List.Item>{t('upstreamAccount.wizard.step2')}</List.Item>
              <List.Item>{t('upstreamAccount.wizard.step3')}</List.Item>
            </List>
            <Button
              primary
              as='a'
              href={session.authorize_url}
              target='_blank'
              rel='noreferrer'
            >
              <Icon name='external' />
              {t('upstreamAccount.wizard.openAuthorize')}
            </Button>
            <Button
              onClick={async () => {
                if (await copy(session.authorize_url)) {
                  showSuccess(t('upstreamAccount.wizard.copySuccess'));
                }
              }}
            >
              <Icon name='copy' />
              {t('upstreamAccount.wizard.copyLink')}
            </Button>
            <p>
              <Icon name='clock outline' />
              {t('upstreamAccount.wizard.expiresIn', { minutes })}
            </p>
            <Form>
              <Form.TextArea
                label={t('upstreamAccount.wizard.callbackLabel')}
                placeholder={t('upstreamAccount.wizard.callbackPlaceholder')}
                value={callbackUrl}
                rows={3}
                onChange={(e, { value }) => setCallbackUrl(String(value))}
              />
            </Form>
          </Segment>
        )}
      </Modal.Content>
      <Modal.Actions>
        {session && (
          <Button
            content={t('upstreamAccount.wizard.regenerate')}
            icon='refresh'
            onClick={() => {
              setSession(null);
              setCallbackUrl('');
            }}
          />
        )}
        {session && (
          <Button
            primary
            loading={completing}
            disabled={completing}
            onClick={completeAuth}
          >
            {t('upstreamAccount.wizard.complete')}
          </Button>
        )}
        <Button onClick={onClose}>{t('upstreamAccount.wizard.cancel')}</Button>
      </Modal.Actions>
    </Modal>
  );
};

export default UpstreamAccountOAuthWizard;
