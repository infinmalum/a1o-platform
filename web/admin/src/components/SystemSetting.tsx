import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Alert,
  Button,
  Checkbox,
  Divider,
  Input,
  Modal,
  Select,
  Space,
  Typography,
} from 'antd';
import { API, removeTrailingSlash, showError } from '../helpers';

const fieldStyle = { marginBottom: 16 };
const labelStyle = { display: 'block', marginBottom: 6, fontWeight: 500 };

const SystemSetting = () => {
  const { t } = useTranslation();
  let [inputs, setInputs] = useState({
    PasswordLoginEnabled: '',
    PasswordRegisterEnabled: '',
    EmailVerificationEnabled: '',
    GitHubOAuthEnabled: '',
    GitHubClientId: '',
    GitHubClientSecret: '',
    LarkClientId: '',
    LarkClientSecret: '',
    Notice: '',
    SMTPServer: '',
    SMTPPort: '',
    SMTPAccount: '',
    SMTPFrom: '',
    SMTPToken: '',
    ServerAddress: '',
    Footer: '',
    MessagePusherAddress: '',
    MessagePusherToken: '',
    TurnstileCheckEnabled: '',
    TurnstileSiteKey: '',
    TurnstileSecretKey: '',
    RegisterEnabled: '',
    EmailDomainRestrictionEnabled: '',
    EmailDomainWhitelist: [] as string[],
  });
  const [originInputs, setOriginInputs] = useState<Record<string, string>>({});
  let [loading, setLoading] = useState(false);
  const [EmailDomainWhitelist, setEmailDomainWhitelist] = useState([]);
  const [restrictedDomainInput, setRestrictedDomainInput] = useState('');
  const [showPasswordWarningModal, setShowPasswordWarningModal] =
    useState(false);

  const getOptions = async () => {
    const res = await API.get('/api/option/');
    const { success, message, data } = res.data;
    if (success) {
      const newInputs: Record<string, string> = {};
      data.forEach((item) => {
        newInputs[item.key] = item.value;
      });
      setInputs({
        ...inputs,
        ...newInputs,
        EmailDomainWhitelist: (newInputs.EmailDomainWhitelist ?? '')
          .split(',')
          .filter((item) => item !== ''),
      });
      setOriginInputs(newInputs);

      setEmailDomainWhitelist(
        (newInputs.EmailDomainWhitelist ?? '')
          .split(',')
          .filter((item) => item !== '')
          .map((item) => {
            return { key: item, label: item, value: item };
          })
      );
    } else {
      showError(message);
    }
  };

  useEffect(() => {
    getOptions().then();
  }, []);

  const updateOption = async (key, value) => {
    setLoading(true);
    switch (key) {
      case 'PasswordLoginEnabled':
      case 'PasswordRegisterEnabled':
      case 'EmailVerificationEnabled':
      case 'GitHubOAuthEnabled':
      case 'TurnstileCheckEnabled':
      case 'EmailDomainRestrictionEnabled':
      case 'RegisterEnabled':
        value = inputs[key] === 'true' ? 'false' : 'true';
        break;
      default:
        break;
    }
    const res = await API.put('/api/option/', {
      key,
      value,
    });
    const { success, message } = res.data;
    if (success) {
      if (key === 'EmailDomainWhitelist') {
        value = value.split(',');
      }
      setInputs((inputs) => ({
        ...inputs,
        [key]: value,
      }));
    } else {
      showError(message);
    }
    setLoading(false);
  };

  const handleInputChange = async (name, value) => {
    if (name === 'PasswordLoginEnabled' && inputs[name] === 'true') {
      // block disabling password login
      setShowPasswordWarningModal(true);
      return;
    }
    if (
      name === 'Notice' ||
      name.startsWith('SMTP') ||
      name === 'ServerAddress' ||
      name === 'GitHubClientId' ||
      name === 'GitHubClientSecret' ||
      name === 'LarkClientId' ||
      name === 'LarkClientSecret' ||
      name === 'TurnstileSiteKey' ||
      name === 'TurnstileSecretKey' ||
      name === 'EmailDomainWhitelist'
    ) {
      setInputs((inputs) => ({ ...inputs, [name]: value }));
    } else {
      await updateOption(name, value);
    }
  };

  const submitServerAddress = async () => {
    let ServerAddress = removeTrailingSlash(inputs.ServerAddress);
    await updateOption('ServerAddress', ServerAddress);
  };

  const submitSMTP = async () => {
    if (originInputs['SMTPServer'] !== inputs.SMTPServer) {
      await updateOption('SMTPServer', inputs.SMTPServer);
    }
    if (originInputs['SMTPAccount'] !== inputs.SMTPAccount) {
      await updateOption('SMTPAccount', inputs.SMTPAccount);
    }
    if (originInputs['SMTPFrom'] !== inputs.SMTPFrom) {
      await updateOption('SMTPFrom', inputs.SMTPFrom);
    }
    if (
      originInputs['SMTPPort'] !== inputs.SMTPPort &&
      inputs.SMTPPort !== ''
    ) {
      await updateOption('SMTPPort', inputs.SMTPPort);
    }
    if (
      originInputs['SMTPToken'] !== inputs.SMTPToken &&
      inputs.SMTPToken !== ''
    ) {
      await updateOption('SMTPToken', inputs.SMTPToken);
    }
  };

  const submitEmailDomainWhitelist = async () => {
    if (
      originInputs['EmailDomainWhitelist'] !==
        inputs.EmailDomainWhitelist.join(',') &&
      inputs.SMTPToken !== ''
    ) {
      await updateOption(
        'EmailDomainWhitelist',
        inputs.EmailDomainWhitelist.join(',')
      );
    }
  };

  const submitMessagePusher = async () => {
    if (originInputs['MessagePusherAddress'] !== inputs.MessagePusherAddress) {
      await updateOption(
        'MessagePusherAddress',
        removeTrailingSlash(inputs.MessagePusherAddress)
      );
    }
    if (
      originInputs['MessagePusherToken'] !== inputs.MessagePusherToken &&
      inputs.MessagePusherToken !== ''
    ) {
      await updateOption('MessagePusherToken', inputs.MessagePusherToken);
    }
  };

  const submitGitHubOAuth = async () => {
    if (originInputs['GitHubClientId'] !== inputs.GitHubClientId) {
      await updateOption('GitHubClientId', inputs.GitHubClientId);
    }
    if (
      originInputs['GitHubClientSecret'] !== inputs.GitHubClientSecret &&
      inputs.GitHubClientSecret !== ''
    ) {
      await updateOption('GitHubClientSecret', inputs.GitHubClientSecret);
    }
  };

  const submitLarkOAuth = async () => {
    if (originInputs['LarkClientId'] !== inputs.LarkClientId) {
      await updateOption('LarkClientId', inputs.LarkClientId);
    }
    if (
      originInputs['LarkClientSecret'] !== inputs.LarkClientSecret &&
      inputs.LarkClientSecret !== ''
    ) {
      await updateOption('LarkClientSecret', inputs.LarkClientSecret);
    }
  };

  const submitTurnstile = async () => {
    if (originInputs['TurnstileSiteKey'] !== inputs.TurnstileSiteKey) {
      await updateOption('TurnstileSiteKey', inputs.TurnstileSiteKey);
    }
    if (
      originInputs['TurnstileSecretKey'] !== inputs.TurnstileSecretKey &&
      inputs.TurnstileSecretKey !== ''
    ) {
      await updateOption('TurnstileSecretKey', inputs.TurnstileSecretKey);
    }
  };

  const addRestrictedDomain = () => {
    const localDomainList = inputs.EmailDomainWhitelist;
    if (
      restrictedDomainInput !== '' &&
      !localDomainList.includes(restrictedDomainInput)
    ) {
      setRestrictedDomainInput('');
      setInputs({
        ...inputs,
        EmailDomainWhitelist: [...localDomainList, restrictedDomainInput],
      });
      setEmailDomainWhitelist([
        ...EmailDomainWhitelist,
        {
          key: restrictedDomainInput,
          label: restrictedDomainInput,
          value: restrictedDomainInput,
        },
      ]);
    }
  };

  return (
    <div style={{ maxWidth: 860 }}>
      <Typography.Title level={5}>
        {t('setting.system.general.title')}
      </Typography.Title>
      <div style={fieldStyle}>
        <label style={labelStyle}>
          {t('setting.system.general.server_address')}
        </label>
        <Input
          placeholder={t('setting.system.general.server_address_placeholder')}
          value={inputs.ServerAddress}
          onChange={(e) => handleInputChange('ServerAddress', e.target.value)}
          style={{ maxWidth: 420 }}
        />
      </div>
      <div style={{ marginBottom: 24 }}>
        <Button loading={loading} onClick={submitServerAddress}>
          {t('setting.system.general.buttons.update')}
        </Button>
      </div>
      <Divider />
      <Typography.Title level={5}>
        {t('setting.system.login.title')}
      </Typography.Title>
      <Space size={24} wrap style={{ display: 'flex', marginBottom: 12 }}>
        <Checkbox
          checked={inputs.PasswordLoginEnabled === 'true'}
          onChange={(e) =>
            handleInputChange('PasswordLoginEnabled', e.target.checked ? 'true' : 'false')
          }
        >
          {t('setting.system.login.password_login')}
        </Checkbox>
        <Checkbox
          checked={inputs.PasswordRegisterEnabled === 'true'}
          onChange={(e) =>
            handleInputChange(
              'PasswordRegisterEnabled',
              e.target.checked ? 'true' : 'false'
            )
          }
        >
          {t('setting.system.login.password_register')}
        </Checkbox>
        <Checkbox
          checked={inputs.EmailVerificationEnabled === 'true'}
          onChange={(e) =>
            handleInputChange(
              'EmailVerificationEnabled',
              e.target.checked ? 'true' : 'false'
            )
          }
        >
          {t('setting.system.login.email_verification')}
        </Checkbox>
        <Checkbox
          checked={inputs.GitHubOAuthEnabled === 'true'}
          onChange={(e) =>
            handleInputChange(
              'GitHubOAuthEnabled',
              e.target.checked ? 'true' : 'false'
            )
          }
        >
          {t('setting.system.login.github_oauth')}
        </Checkbox>
      </Space>
      <Space size={24} wrap style={{ display: 'flex', marginBottom: 24 }}>
        <Checkbox
          checked={inputs.RegisterEnabled === 'true'}
          onChange={(e) =>
            handleInputChange('RegisterEnabled', e.target.checked ? 'true' : 'false')
          }
        >
          {t('setting.system.login.registration')}
        </Checkbox>
        <Checkbox
          checked={inputs.TurnstileCheckEnabled === 'true'}
          onChange={(e) =>
            handleInputChange(
              'TurnstileCheckEnabled',
              e.target.checked ? 'true' : 'false'
            )
          }
        >
          {t('setting.system.login.turnstile')}
        </Checkbox>
      </Space>
      <Modal
        open={showPasswordWarningModal}
        onCancel={() => setShowPasswordWarningModal(false)}
        title={t('setting.system.password_login.warning.title')}
        footer={[
          <Button key='cancel' onClick={() => setShowPasswordWarningModal(false)}>
            {t('setting.system.password_login.warning.buttons.cancel')}
          </Button>,
          <Button
            key='confirm'
            color='gold'
            variant='solid'
            onClick={async () => {
              setShowPasswordWarningModal(false);
              await updateOption('PasswordLoginEnabled', 'false');
            }}
          >
            {t('setting.system.password_login.warning.buttons.confirm')}
          </Button>,
        ]}
      >
        <p>{t('setting.system.password_login.warning.content')}</p>
      </Modal>
      <Divider />
      <Typography.Title level={5}>
        {t('setting.system.email_restriction.title')}
      </Typography.Title>
      <Alert
        type='info'
        showIcon
        message={t('setting.system.email_restriction.subtitle')}
        style={{ marginBottom: 12 }}
      />
      <Space size={24} wrap style={{ display: 'flex', marginBottom: 12 }}>
        <Checkbox
          checked={inputs.EmailDomainRestrictionEnabled === 'true'}
          onChange={(e) =>
            handleInputChange(
              'EmailDomainRestrictionEnabled',
              e.target.checked ? 'true' : 'false'
            )
          }
        >
          {t('setting.system.email_restriction.enable')}
        </Checkbox>
      </Space>
      <div style={fieldStyle}>
        <label style={labelStyle}>
          {t('setting.system.email_restriction.add_domain')}
        </label>
        <Space.Compact style={{ width: 420 }}>
          <Input
            placeholder={t(
              'setting.system.email_restriction.add_domain_placeholder'
            )}
            value={restrictedDomainInput}
            onChange={(e) => setRestrictedDomainInput(e.target.value)}
            onPressEnter={addRestrictedDomain}
          />
          <Button
            onClick={() => {
              if (restrictedDomainInput === '') return;
              setEmailDomainWhitelist([
                ...EmailDomainWhitelist,
                {
                  key: restrictedDomainInput,
                  label: restrictedDomainInput,
                  value: restrictedDomainInput,
                },
              ]);
              setRestrictedDomainInput('');
            }}
          >
            {t('setting.system.email_restriction.buttons.fill')}
          </Button>
        </Space.Compact>
      </div>
      <div style={fieldStyle}>
        <label style={labelStyle}>
          {t('setting.system.email_restriction.allowed_domains')}
        </label>
        <Select
          mode='tags'
          style={{ maxWidth: 420 }}
          placeholder={t('setting.system.email_restriction.allowed_domains')}
          value={EmailDomainWhitelist.map((item) => item.value)}
          options={EmailDomainWhitelist}
          onChange={(value) => {
            if (!Array.isArray(value)) return;
            let newEmailDomainWhitelist = [];
            value.forEach((item) => {
              newEmailDomainWhitelist.push({
                key: String(item),
                label: String(item),
                value: String(item),
              });
            });
            setEmailDomainWhitelist(newEmailDomainWhitelist);
          }}
        />
      </div>
      <div style={{ marginBottom: 24 }}>
        <Button loading={loading} onClick={submitEmailDomainWhitelist}>
          {t('setting.system.email_restriction.buttons.save')}
        </Button>
      </div>

      <Divider />
      <Typography.Title level={5}>
        {t('setting.system.smtp.title')}
      </Typography.Title>
      <Alert
        type='info'
        showIcon
        message={t('setting.system.smtp.subtitle')}
        style={{ marginBottom: 12 }}
      />
      <Space size={24} wrap align='start' style={{ display: 'flex', marginBottom: 12 }}>
        <div>
          <label style={labelStyle}>{t('setting.system.smtp.server')}</label>
          <Input
            placeholder={t('setting.system.smtp.server_placeholder')}
            value={inputs.SMTPServer}
            onChange={(e) => handleInputChange('SMTPServer', e.target.value)}
            style={{ width: 240 }}
          />
        </div>
        <div>
          <label style={labelStyle}>{t('setting.system.smtp.port')}</label>
          <Input
            placeholder={t('setting.system.smtp.port_placeholder')}
            value={inputs.SMTPPort}
            onChange={(e) => handleInputChange('SMTPPort', e.target.value)}
            style={{ width: 160 }}
          />
        </div>
        <div>
          <label style={labelStyle}>{t('setting.system.smtp.account')}</label>
          <Input
            placeholder={t('setting.system.smtp.account_placeholder')}
            value={inputs.SMTPAccount}
            onChange={(e) => handleInputChange('SMTPAccount', e.target.value)}
            style={{ width: 240 }}
          />
        </div>
      </Space>
      <Space size={24} wrap align='start' style={{ display: 'flex', marginBottom: 12 }}>
        <div>
          <label style={labelStyle}>{t('setting.system.smtp.from')}</label>
          <Input
            placeholder={t('setting.system.smtp.from_placeholder')}
            value={inputs.SMTPFrom}
            onChange={(e) => handleInputChange('SMTPFrom', e.target.value)}
            style={{ width: 240 }}
          />
        </div>
        <div>
          <label style={labelStyle}>{t('setting.system.smtp.token')}</label>
          <Input.Password
            placeholder={t('setting.system.smtp.token_placeholder')}
            value={inputs.SMTPToken}
            onChange={(e) => handleInputChange('SMTPToken', e.target.value)}
            style={{ width: 240 }}
          />
        </div>
      </Space>
      <div style={{ marginBottom: 24 }}>
        <Button loading={loading} onClick={submitSMTP}>
          {t('setting.system.smtp.buttons.save')}
        </Button>
      </div>

      <Divider />
      <Typography.Title level={5}>
        {t('setting.system.github.title')}
      </Typography.Title>
      <Typography.Paragraph type='secondary'>
        {t('setting.system.github.subtitle')}
        <a href='https://github.com/settings/developers' target='_blank' rel='noreferrer'>
          {t('setting.system.github.manage_link')}
        </a>
        {t('setting.system.github.manage_text')}
      </Typography.Paragraph>
      <Alert
        type='info'
        showIcon
        message={t('setting.system.github.url_notice', {
          server_url: originInputs.ServerAddress,
          callback_url: `${originInputs.ServerAddress}/oauth/github`,
        })}
        style={{ marginBottom: 12 }}
      />
      <Space size={24} wrap align='start' style={{ display: 'flex', marginBottom: 12 }}>
        <div>
          <label style={labelStyle}>{t('setting.system.github.client_id')}</label>
          <Input
            placeholder={t('setting.system.github.client_id_placeholder')}
            value={inputs.GitHubClientId}
            onChange={(e) =>
              handleInputChange('GitHubClientId', e.target.value)
            }
            style={{ width: 280 }}
          />
        </div>
        <div>
          <label style={labelStyle}>
            {t('setting.system.github.client_secret')}
          </label>
          <Input.Password
            placeholder={t('setting.system.github.client_secret_placeholder')}
            value={inputs.GitHubClientSecret}
            onChange={(e) =>
              handleInputChange('GitHubClientSecret', e.target.value)
            }
            style={{ width: 280 }}
          />
        </div>
      </Space>
      <div style={{ marginBottom: 24 }}>
        <Button loading={loading} onClick={submitGitHubOAuth}>
          {t('setting.system.github.buttons.save')}
        </Button>
      </div>

      <Divider />
      <Typography.Title level={5}>
        {t('setting.system.lark.title')}
        <Typography.Text
          type='secondary'
          style={{ display: 'block', fontSize: '0.85em', fontWeight: 400 }}
        >
          {t('setting.system.lark.subtitle')}
          <a href='https://open.feishu.cn/app' target='_blank' rel='noreferrer'>
            {t('setting.system.lark.manage_link')}
          </a>
          {t('setting.system.lark.manage_text')}
        </Typography.Text>
      </Typography.Title>
      <Alert
        type='info'
        showIcon
        message={t('setting.system.lark.url_notice', {
          server_url: inputs.ServerAddress,
          callback_url: `${inputs.ServerAddress}/oauth/lark`,
        })}
        style={{ marginBottom: 12 }}
      />
      <Space size={24} wrap align='start' style={{ display: 'flex', marginBottom: 12 }}>
        <div>
          <label style={labelStyle}>{t('setting.system.lark.client_id')}</label>
          <Input
            autoComplete='new-password'
            value={inputs.LarkClientId}
            placeholder={t('setting.system.lark.client_id_placeholder')}
            onChange={(e) => handleInputChange('LarkClientId', e.target.value)}
            style={{ width: 280 }}
          />
        </div>
        <div>
          <label style={labelStyle}>
            {t('setting.system.lark.client_secret')}
          </label>
          <Input.Password
            type='password'
            autoComplete='new-password'
            value={inputs.LarkClientSecret}
            placeholder={t('setting.system.lark.client_secret_placeholder')}
            onChange={(e) =>
              handleInputChange('LarkClientSecret', e.target.value)
            }
            style={{ width: 280 }}
          />
        </div>
      </Space>
      <div style={{ marginBottom: 24 }}>
        <Button loading={loading} onClick={submitLarkOAuth}>
          {t('setting.system.lark.buttons.save')}
        </Button>
      </div>

      <Divider />
      <Typography.Title level={5}>
        {t('setting.system.turnstile.title')}
        <Typography.Text
          type='secondary'
          style={{ display: 'block', fontSize: '0.85em', fontWeight: 400 }}
        >
          {t('setting.system.turnstile.subtitle')}
          <a href='https://dash.cloudflare.com/' target='_blank' rel='noreferrer'>
            {t('setting.system.turnstile.manage_link')}
          </a>
          {t('setting.system.turnstile.manage_text')}
        </Typography.Text>
      </Typography.Title>
      <Space size={24} wrap align='start' style={{ display: 'flex', marginBottom: 12 }}>
        <div>
          <label style={labelStyle}>
            {t('setting.system.turnstile.site_key')}
          </label>
          <Input
            autoComplete='new-password'
            value={inputs.TurnstileSiteKey}
            placeholder={t('setting.system.turnstile.site_key_placeholder')}
            onChange={(e) =>
              handleInputChange('TurnstileSiteKey', e.target.value)
            }
            style={{ width: 280 }}
          />
        </div>
        <div>
          <label style={labelStyle}>
            {t('setting.system.turnstile.secret_key')}
          </label>
          <Input.Password
            autoComplete='new-password'
            value={inputs.TurnstileSecretKey}
            placeholder={t('setting.system.turnstile.secret_key_placeholder')}
            onChange={(e) =>
              handleInputChange('TurnstileSecretKey', e.target.value)
            }
            style={{ width: 280 }}
          />
        </div>
      </Space>
      <Button loading={loading} onClick={submitTurnstile}>
        {t('setting.system.turnstile.buttons.save')}
      </Button>
    </div>
  );
};

export default SystemSetting;
