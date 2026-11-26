import React, { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Card, Input } from 'antd';
import { API, showError, showSuccess } from '../../helpers';

const fieldStyle = { marginBottom: 16 };
const labelStyle = { display: 'block', marginBottom: 6, fontWeight: 500 };

const AddUser = () => {
  const { t } = useTranslation();
  const originInputs = {
    username: '',
    display_name: '',
    password: '',
  };
  const [inputs, setInputs] = useState(originInputs);

  const handleInputChange = (name, value) => {
    setInputs((inputs) => ({ ...inputs, [name]: value }));
  };

  const submit = async () => {
    if (inputs.username === '' || inputs.password === '') return;
    const res = await API.post(`/api/user/`, inputs);
    const { success, message } = res.data;
    if (success) {
      showSuccess(t('user.messages.create_success'));
      setInputs(originInputs);
    } else {
      showError(message);
    }
  };

  return (
    <Card title={t('user.add.title')}>
      <div style={{ maxWidth: 560 }}>
        <div style={fieldStyle}>
          <label style={labelStyle}>
            {t('user.edit.username')}{' '}
            <span style={{ color: '#ff4d4f' }}>*</span>
          </label>
          <Input
            placeholder={t('user.edit.username_placeholder')}
            onChange={(e) => handleInputChange('username', e.target.value)}
            value={inputs.username}
            autoComplete='off'
          />
        </div>
        <div style={fieldStyle}>
          <label style={labelStyle}>{t('user.edit.display_name')}</label>
          <Input
            placeholder={t('user.edit.display_name_placeholder')}
            onChange={(e) => handleInputChange('display_name', e.target.value)}
            value={inputs.display_name}
            autoComplete='off'
          />
        </div>
        <div style={fieldStyle}>
          <label style={labelStyle}>
            {t('user.edit.password')}{' '}
            <span style={{ color: '#ff4d4f' }}>*</span>
          </label>
          <Input.Password
            placeholder={t('user.edit.password_placeholder')}
            onChange={(e) => handleInputChange('password', e.target.value)}
            value={inputs.password}
            autoComplete='off'
          />
        </div>
        <Button type='primary' onClick={submit}>
          {t('user.edit.buttons.submit')}
        </Button>
      </div>
    </Card>
  );
};

export default AddUser;
