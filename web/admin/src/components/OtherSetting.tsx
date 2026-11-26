import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Divider, Input, Modal, Typography } from 'antd';
import { API, showError, showSuccess, verifyJSON } from '../helpers';
import { marked } from 'marked';

const fieldStyle = { marginBottom: 16 };
const labelStyle = { display: 'block', marginBottom: 6, fontWeight: 500 };

const OtherSetting = () => {
  const { t } = useTranslation();
  let [inputs, setInputs] = useState({
    Footer: '',
    Notice: '',
    About: '',
    SystemName: '',
    Logo: '',
    HomePageContent: '',
  });
  let [loading, setLoading] = useState(false);
  const [showUpdateModal, setShowUpdateModal] = useState(false);
  const [updateData, setUpdateData] = useState({
    tag_name: '',
    content: '',
  });

  const getOptions = async () => {
    const res = await API.get('/api/option/');
    const { success, message, data } = res.data;
    if (success) {
      const newInputs = { ...inputs };
      data.forEach((item) => {
        if (item.key in inputs) {
          newInputs[item.key] = item.value;
        }
      });
      setInputs(newInputs);
    } else {
      showError(message);
    }
  };

  useEffect(() => {
    getOptions().then();
  }, []);

  const updateOption = async (key, value) => {
    setLoading(true);
    const res = await API.put('/api/option/', {
      key,
      value,
    });
    const { success, message } = res.data;
    if (success) {
      setInputs((inputs) => ({ ...inputs, [key]: value }));
    } else {
      showError(message);
    }
    setLoading(false);
  };

  const handleInputChange = (name, value) => {
    setInputs((inputs) => ({ ...inputs, [name]: value }));
  };

  const submitNotice = async () => {
    await updateOption('Notice', inputs.Notice);
  };

  const submitSystemName = async () => {
    await updateOption('SystemName', inputs.SystemName);
  };

  const submitLogo = async () => {
    await updateOption('Logo', inputs.Logo);
  };

  const submitAbout = async () => {
    await updateOption('About', inputs.About);
  };

  const submitOption = async (key) => {
    await updateOption(key, inputs[key]);
  };

  const openGitHubRelease = () => {
    window.location.href = 'https://github.com/infinmalum/a1o-platform/releases/latest';
  };

  const checkUpdate = async () => {
    const res = await API.get(
      'https://api.github.com/repos/infinmalum/a1o-platform/releases/latest'
    );
    const { tag_name, body } = res.data;
    if (tag_name === import.meta.env.REACT_APP_VERSION) {
      showSuccess(`已是最新版本：${tag_name}`);
    } else {
      setUpdateData({
        tag_name: tag_name,
        content: await marked.parse(body),
      });
      setShowUpdateModal(true);
    }
  };

  return (
    <div style={{ maxWidth: 860 }}>
      <Typography.Title level={5}>
        {t('setting.other.notice.title')}
      </Typography.Title>
      <div style={fieldStyle}>
        <label style={labelStyle}>{t('setting.other.notice.content')}</label>
        <Input.TextArea
          placeholder={t('setting.other.notice.content_placeholder')}
          value={inputs.Notice}
          onChange={(e) => handleInputChange('Notice', e.target.value)}
          style={{ fontFamily: 'JetBrains Mono, Consolas' }}
          rows={5}
        />
      </div>
      <div style={{ marginBottom: 24 }}>
        <Button loading={loading} onClick={submitNotice}>
          {t('setting.other.notice.buttons.save')}
        </Button>
      </div>

      <Divider />
      <Typography.Title level={5}>
        {t('setting.other.system.title')}
      </Typography.Title>
      <div style={fieldStyle}>
        <label style={labelStyle}>{t('setting.other.system.name')}</label>
        <Input
          placeholder={t('setting.other.system.name_placeholder')}
          value={inputs.SystemName}
          onChange={(e) => handleInputChange('SystemName', e.target.value)}
          style={{ maxWidth: 420 }}
        />
      </div>
      <div style={{ marginBottom: 24 }}>
        <Button loading={loading} onClick={submitSystemName}>
          {t('setting.other.system.buttons.save_name')}
        </Button>
      </div>
      <div style={fieldStyle}>
        <label style={labelStyle}>{t('setting.other.system.logo')}</label>
        <Input
          placeholder={t('setting.other.system.logo_placeholder')}
          value={inputs.Logo}
          type='url'
          onChange={(e) => handleInputChange('Logo', e.target.value)}
          style={{ maxWidth: 420 }}
        />
      </div>
      <Button loading={loading} onClick={submitLogo}>
        {t('setting.other.system.buttons.save_logo')}
      </Button>

      <Divider />
      <Typography.Title level={5}>
        {t('setting.other.content.title')}
      </Typography.Title>
      <div style={fieldStyle}>
        <label style={labelStyle}>
          {t('setting.other.content.homepage.title')}
        </label>
        <Input.TextArea
          placeholder={t('setting.other.content.homepage.placeholder')}
          value={inputs.HomePageContent}
          onChange={(e) => handleInputChange('HomePageContent', e.target.value)}
          style={{ fontFamily: 'JetBrains Mono, Consolas' }}
          rows={8}
        />
      </div>
      <div style={{ marginBottom: 24 }}>
        <Button loading={loading} onClick={() => submitOption('HomePageContent')}>
          {t('setting.other.content.buttons.save_homepage')}
        </Button>
      </div>
      <div style={fieldStyle}>
        <label style={labelStyle}>{t('setting.other.content.about.title')}</label>
        <Input.TextArea
          placeholder={t('setting.other.content.about.placeholder')}
          value={inputs.About}
          onChange={(e) => handleInputChange('About', e.target.value)}
          style={{ fontFamily: 'JetBrains Mono, Consolas' }}
          rows={8}
        />
      </div>
      <div style={{ marginBottom: 24 }}>
        <Button loading={loading} onClick={submitAbout}>
          {t('setting.other.content.buttons.save_about')}
        </Button>
      </div>
      <Alert
        type='info'
        showIcon
        message={t('setting.other.copyright.notice')}
        style={{ marginBottom: 12 }}
      />
      <div style={fieldStyle}>
        <label style={labelStyle}>{t('setting.other.content.footer.title')}</label>
        <Input
          placeholder={t('setting.other.content.footer.placeholder')}
          value={inputs.Footer}
          onChange={(e) => handleInputChange('Footer', e.target.value)}
          style={{ maxWidth: 420 }}
        />
      </div>
      <Button loading={loading} onClick={() => submitOption('Footer')}>
        {t('setting.other.content.buttons.save_footer')}
      </Button>

      <Modal
        open={showUpdateModal}
        onCancel={() => setShowUpdateModal(false)}
        title={`新版本：${updateData.tag_name}`}
        footer={[
          <Button key='close' onClick={() => setShowUpdateModal(false)}>
            关闭
          </Button>,
          <Button
            key='detail'
            type='primary'
            onClick={() => {
              setShowUpdateModal(false);
              openGitHubRelease();
            }}
          >
            详情
          </Button>,
        ]}
      >
        <div dangerouslySetInnerHTML={{ __html: updateData.content }}></div>
      </Modal>
    </div>
  );
};

export default OtherSetting;
