import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Card, Input, InputNumber, Space } from 'antd';
import { useParams, useNavigate } from 'react-router-dom';
import { API, downloadTextAsFile, showError, showSuccess } from '../../helpers';
import QuotaInput from '../../components/QuotaInput';

const fieldStyle = { marginBottom: 16 };
const labelStyle = { display: 'block', marginBottom: 6, fontWeight: 500 };

const EditRedemption = () => {
  const { t } = useTranslation();
  const params = useParams();
  const navigate = useNavigate();
  const redemptionId = params.id;
  const isEdit = redemptionId !== undefined;
  const [loading, setLoading] = useState(isEdit);
  const originInputs = {
    name: '',
    quota: 100000,
    count: 1,
  };
  const [inputs, setInputs] = useState(originInputs);

  const handleCancel = () => {
    navigate('/redemption');
  };

  const handleInputChange = (name, value) => {
    setInputs((inputs) => ({ ...inputs, [name]: value }));
  };

  const loadRedemption = async () => {
    let res = await API.get(`/api/redemption/${redemptionId}`);
    const { success, message, data } = res.data;
    if (success) {
      setInputs(data);
    } else {
      showError(message);
    }
    setLoading(false);
  };
  useEffect(() => {
    if (isEdit) {
      loadRedemption().then();
    }
  }, []);

  const submit = async () => {
    if (!isEdit && inputs.name === '') return;
    const localInputs = {
      ...inputs,
      count: Number(inputs.count),
      quota: Number(inputs.quota),
    };
    let res;
    if (isEdit) {
      res = await API.put(`/api/redemption/`, {
        ...localInputs,
        id: parseInt(redemptionId),
      });
    } else {
      res = await API.post(`/api/redemption/`, {
        ...localInputs,
      });
    }
    const { success, message, data } = res.data;
    if (success) {
      if (isEdit) {
        showSuccess(t('redemption.messages.update_success'));
      } else {
        showSuccess(t('redemption.messages.create_success'));
        setInputs(originInputs);
      }
    } else {
      showError(message);
    }
    if (!isEdit && data) {
      let text = '';
      for (let i = 0; i < data.length; i++) {
        text += data[i] + '\n';
      }
      downloadTextAsFile(text, `${inputs.name}.txt`);
    }
  };

  return (
    <Card
      title={
        isEdit ? t('redemption.edit.title_edit') : t('redemption.edit.title_create')
      }
      loading={loading}
    >
      <div style={{ maxWidth: 560 }}>
        <div style={fieldStyle}>
          <label style={labelStyle}>
            {t('redemption.edit.name')}{' '}
            {!isEdit && <span style={{ color: '#ff4d4f' }}>*</span>}
          </label>
          <Input
            placeholder={t('redemption.edit.name_placeholder')}
            onChange={(e) => handleInputChange('name', e.target.value)}
            value={inputs.name}
            autoComplete='new-password'
          />
        </div>
        <div style={fieldStyle}>
          <QuotaInput
            label={t('redemption.edit.quota')}
            name='quota'
            quota={inputs.quota}
            onQuotaChange={(name, value) => handleInputChange(name, value)}
            placeholder={t('redemption.edit.quota_placeholder')}
          />
        </div>
        {!isEdit && (
          <div style={fieldStyle}>
            <label style={labelStyle}>{t('redemption.edit.count')}</label>
            <InputNumber
              min={1}
              style={{ width: '100%' }}
              placeholder={t('redemption.edit.count_placeholder')}
              onChange={(value) => handleInputChange('count', value)}
              value={inputs.count}
            />
          </div>
        )}
        <Space>
          <Button type='primary' onClick={submit}>
            {t('redemption.edit.buttons.submit')}
          </Button>
          <Button onClick={handleCancel}>
            {t('redemption.edit.buttons.cancel')}
          </Button>
        </Space>
      </div>
    </Card>
  );
};

export default EditRedemption;
