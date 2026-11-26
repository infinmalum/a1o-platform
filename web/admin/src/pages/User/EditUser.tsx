import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Card, Input, Select, Space } from 'antd';
import { useParams, useNavigate } from 'react-router-dom';
import { API, showError, showSuccess } from '../../helpers';
import QuotaInput from '../../components/QuotaInput';

const fieldStyle = { marginBottom: 16 };
const labelStyle = { display: 'block', marginBottom: 6, fontWeight: 500 };

const EditUser = () => {
  const { t } = useTranslation();
  const params = useParams();
  const userId = params.id;
  const [loading, setLoading] = useState(true);
  const [inputs, setInputs] = useState({
    username: '',
    display_name: '',
    password: '',
    github_id: '',
    email: '',
    quota: 0,
    group: 'default',
  });
  const [groupOptions, setGroupOptions] = useState([]);
  const handleInputChange = (name, value) => {
    setInputs((inputs) => ({ ...inputs, [name]: value }));
  };
  const fetchGroups = async () => {
    try {
      let res = await API.get(`/api/group/`);
      setGroupOptions(
        res.data.data.map((group) => ({
          key: group,
          label: group,
          value: group,
        }))
      );
    } catch (error) {
      showError(error.message);
    }
  };
  const navigate = useNavigate();
  const handleCancel = () => {
    navigate('/setting');
  };
  const loadUser = async () => {
    let res = undefined;
    if (userId) {
      res = await API.get(`/api/user/${userId}`);
    } else {
      res = await API.get(`/api/user/self`);
    }
    const { success, message, data } = res.data;
    if (success) {
      data.password = '';
      setInputs(data);
    } else {
      showError(message);
    }
    setLoading(false);
  };
  useEffect(() => {
    loadUser().then();
    if (userId) {
      fetchGroups().then();
    }
  }, []);

  const submit = async () => {
    let res = undefined;
    if (userId) {
      // quota stays an integer: QuotaInput commits whole quota numbers, so no
      // truncating parse here.
      res = await API.put(`/api/user/`, { ...inputs, id: parseInt(userId) });
    } else {
      res = await API.put(`/api/user/self`, inputs);
    }
    const { success, message } = res.data;
    if (success) {
      showSuccess(t('user.messages.update_success'));
    } else {
      showError(message);
    }
  };

  return (
    <Card title={t('user.edit.title')} loading={loading}>
      <div style={{ maxWidth: 560 }}>
        <div style={fieldStyle}>
          <label style={labelStyle}>{t('user.edit.username')}</label>
          <Input
            placeholder={t('user.edit.username_placeholder')}
            onChange={(e) => handleInputChange('username', e.target.value)}
            value={inputs.username}
            autoComplete='new-password'
          />
        </div>
        <div style={fieldStyle}>
          <label style={labelStyle}>{t('user.edit.password')}</label>
          <Input.Password
            placeholder={t('user.edit.password_placeholder')}
            onChange={(e) => handleInputChange('password', e.target.value)}
            value={inputs.password}
            autoComplete='new-password'
          />
        </div>
        <div style={fieldStyle}>
          <label style={labelStyle}>{t('user.edit.display_name')}</label>
          <Input
            placeholder={t('user.edit.display_name_placeholder')}
            onChange={(e) => handleInputChange('display_name', e.target.value)}
            value={inputs.display_name}
            autoComplete='new-password'
          />
        </div>
        {userId && (
          <>
            <div style={fieldStyle}>
              <label style={labelStyle}>{t('user.edit.group')}</label>
              <Select
                mode='tags'
                style={{ width: '100%' }}
                placeholder={t('user.edit.group_placeholder')}
                value={[inputs.group]}
                onChange={(value) => handleInputChange('group', value[0] ?? '')}
                options={groupOptions}
              />
            </div>
            <div style={fieldStyle}>
              <QuotaInput
                label={t('user.edit.quota')}
                name='quota'
                quota={inputs.quota}
                onQuotaChange={(name, value) => handleInputChange(name, value)}
                placeholder={t('user.edit.quota_placeholder')}
              />
            </div>
          </>
        )}
        <div style={fieldStyle}>
          <label style={labelStyle}>{t('user.edit.github_id')}</label>
          <Input
            value={inputs.github_id}
            autoComplete='new-password'
            placeholder={t('user.edit.github_id_placeholder')}
            readOnly
          />
        </div>
        <div style={fieldStyle}>
          <label style={labelStyle}>{t('user.edit.email')}</label>
          <Input
            value={inputs.email}
            autoComplete='new-password'
            placeholder={t('user.edit.email_placeholder')}
            readOnly
          />
        </div>
        <Space>
          <Button onClick={handleCancel}>{t('user.edit.buttons.cancel')}</Button>
          <Button type='primary' onClick={submit}>
            {t('user.edit.buttons.submit')}
          </Button>
        </Space>
      </div>
    </Card>
  );
};

export default EditUser;
