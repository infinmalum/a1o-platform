import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Checkbox, Divider, Input, InputNumber, Space, Typography } from 'antd';
import PriceEditor from './PriceEditor';
import QuotaInput from './QuotaInput';
import {
  API,
  showError,
  showSuccess,
  timestamp2string,
  verifyJSON,
} from '../helpers';

const fieldStyle = { marginBottom: 16 };
const knowledgeLimitsKey = 'KnowledgeLimits';
const labelStyle = { display: 'block', marginBottom: 6, fontWeight: 500 };

const OperationSetting = () => {
  const { t } = useTranslation();
  let now = new Date();
  let [inputs, setInputs] = useState({
    QuotaForNewUser: 0,
    QuotaForInviter: 0,
    QuotaForInvitee: 0,
    QuotaRemindThreshold: 0,
    PreConsumedQuota: 0,
    ModelRatio: '',
    CompletionRatio: '',
    ModelCachePrices: '{}',
    GroupRatio: '',
    TopUpLink: '',
    ChatLink: '',
    QuotaPerUnit: 0,
    AutomaticDisableChannelEnabled: '',
    AutomaticEnableChannelEnabled: '',
    ChannelDisableThreshold: 0,
    LogConsumeEnabled: '',
    DisplayInCurrencyEnabled: '',
    DisplayTokenStatEnabled: '',
    ApproximateTokenEnabled: '',
    RetryTimes: 0,
  });
  const [knowledgeLimits, setKnowledgeLimits] = useState<{ fileMiB: number | null; libraryMiB: number | null }>({
    fileMiB: 16, libraryMiB: 256,
  });
  const [knowledgeReady, setKnowledgeReady] = useState(false);
  const [savingKnowledgeLimits, setSavingKnowledgeLimits] = useState(false);
  const fileMiB = knowledgeLimits.fileMiB;
  const libraryMiB = knowledgeLimits.libraryMiB;
  const canSaveKnowledgeLimits = fileMiB !== null && libraryMiB !== null &&
    Number.isInteger(fileMiB) && Number.isInteger(libraryMiB) && fileMiB >= 1 && fileMiB <= 64 &&
    libraryMiB >= fileMiB && libraryMiB <= 1024;
  const [originInputs, setOriginInputs] = useState({});
  let [loading, setLoading] = useState(false);
  const [advanced, setAdvanced] = useState(false);
  let [historyTimestamp, setHistoryTimestamp] = useState(
    timestamp2string(now.getTime() / 1000 - 30 * 24 * 3600)
  ); // a month ago

  const getOptions = async () => {
    const res = await API.get('/api/option/');
    const { success, message, data } = res.data;
    if (success) {
      let newInputs = { ...inputs };
      data.forEach((item) => {
        if (item.key === knowledgeLimitsKey) {
          setKnowledgeLimits(JSON.parse(item.value));
          setKnowledgeReady(true);
          return;
        }
        if (
          item.key === 'ModelRatio' ||
          item.key === 'GroupRatio' ||
          item.key === 'ModelCachePrices' ||
          item.key === 'CompletionRatio'
        ) {
          item.value = JSON.stringify(JSON.parse(item.value), null, 2);
        }
        if (item.value === '{}' && item.key !== 'ModelCachePrices') {
          item.value = '';
        }
        newInputs[item.key] = item.value;
      });
      setInputs(newInputs);
      setOriginInputs(newInputs);
    } else {
      showError(message);
    }
  };

  useEffect(() => {
    getOptions().then();
  }, []);

  const updateOption = async (key, value) => {
    setLoading(true);
    if (key.endsWith('Enabled')) {
      value = inputs[key] === 'true' ? 'false' : 'true';
    }
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

  const handleInputChange = async (name, value) => {
    if (name.endsWith('Enabled')) {
      await updateOption(name, value);
    } else {
      setInputs((inputs) => ({ ...inputs, [name]: value }));
    }
  };

  // Quota options travel to the API as the raw string in inputs, so keep the
  // integer QuotaInput commits as a string.
  const handleQuotaOptionChange = (name, value) => {
    setInputs((inputs) => ({ ...inputs, [name]: String(value) }));
  };

  const submitConfig = async (group) => {
    switch (group) {
      case 'monitor':
        if (
          originInputs['ChannelDisableThreshold'] !==
          inputs.ChannelDisableThreshold
        ) {
          await updateOption(
            'ChannelDisableThreshold',
            inputs.ChannelDisableThreshold
          );
        }
        if (
          originInputs['QuotaRemindThreshold'] !== inputs.QuotaRemindThreshold
        ) {
          await updateOption(
            'QuotaRemindThreshold',
            inputs.QuotaRemindThreshold
          );
        }
        break;
      case 'ratio':
        if (originInputs['ModelRatio'] !== inputs.ModelRatio) {
          if (!verifyJSON(inputs.ModelRatio)) {
            showError('模型倍率不是合法的 JSON 字符串');
            return;
          }
          await updateOption('ModelRatio', inputs.ModelRatio);
        }
        if (originInputs['GroupRatio'] !== inputs.GroupRatio) {
          if (!verifyJSON(inputs.GroupRatio)) {
            showError('分组倍率不是合法的 JSON 字符串');
            return;
          }
          await updateOption('GroupRatio', inputs.GroupRatio);
        }
        if (originInputs['CompletionRatio'] !== inputs.CompletionRatio) {
          if (!verifyJSON(inputs.CompletionRatio)) {
            showError('补全倍率不是合法的 JSON 字符串');
            return;
          }
          await updateOption('CompletionRatio', inputs.CompletionRatio);
        }
        if (originInputs['ModelCachePrices'] !== inputs.ModelCachePrices) {
          await updateOption('ModelCachePrices', inputs.ModelCachePrices);
        }
        break;
      case 'quota':
        if (originInputs['QuotaForNewUser'] !== inputs.QuotaForNewUser) {
          await updateOption('QuotaForNewUser', inputs.QuotaForNewUser);
        }
        if (originInputs['QuotaForInvitee'] !== inputs.QuotaForInvitee) {
          await updateOption('QuotaForInvitee', inputs.QuotaForInvitee);
        }
        if (originInputs['QuotaForInviter'] !== inputs.QuotaForInviter) {
          await updateOption('QuotaForInviter', inputs.QuotaForInviter);
        }
        if (originInputs['PreConsumedQuota'] !== inputs.PreConsumedQuota) {
          await updateOption('PreConsumedQuota', inputs.PreConsumedQuota);
        }
        break;
      case 'general':
        if (originInputs['TopUpLink'] !== inputs.TopUpLink) {
          await updateOption('TopUpLink', inputs.TopUpLink);
        }
        if (originInputs['ChatLink'] !== inputs.ChatLink) {
          await updateOption('ChatLink', inputs.ChatLink);
        }
        if (originInputs['QuotaPerUnit'] !== inputs.QuotaPerUnit) {
          await updateOption('QuotaPerUnit', inputs.QuotaPerUnit);
        }
        if (originInputs['RetryTimes'] !== inputs.RetryTimes) {
          await updateOption('RetryTimes', inputs.RetryTimes);
        }
        break;
    }
  };

  const deleteHistoryLogs = async () => {
    const res = await API.delete(
      `/api/log/?target_timestamp=${Date.parse(historyTimestamp) / 1000}`
    );
    const { success, message, data } = res.data;
    if (success) {
      showSuccess(`${data} 条日志已清理！`);
      return;
    }
    showError('日志清理失败：' + message);
  };

  return (
    <div style={{ maxWidth: 860 }}>
      <Typography.Title level={5}>
        {t('setting.operation.quota.title')}
      </Typography.Title>
      <Space size={24} wrap align='start' style={{ marginBottom: 16 }}>
        <QuotaInput
          label={t('setting.operation.quota.new_user')}
          name='QuotaForNewUser'
          quota={Number(inputs.QuotaForNewUser) || 0}
          onQuotaChange={handleQuotaOptionChange}
          placeholder={t('setting.operation.quota.new_user_placeholder')}
        />
        <QuotaInput
          label={t('setting.operation.quota.pre_consume')}
          name='PreConsumedQuota'
          quota={Number(inputs.PreConsumedQuota) || 0}
          onQuotaChange={handleQuotaOptionChange}
          placeholder={t('setting.operation.quota.pre_consume_placeholder')}
        />
        <QuotaInput
          label={t('setting.operation.quota.inviter_reward')}
          name='QuotaForInviter'
          quota={Number(inputs.QuotaForInviter) || 0}
          onQuotaChange={handleQuotaOptionChange}
          placeholder={t('setting.operation.quota.inviter_reward_placeholder')}
        />
        <QuotaInput
          label={t('setting.operation.quota.invitee_reward')}
          name='QuotaForInvitee'
          quota={Number(inputs.QuotaForInvitee) || 0}
          onQuotaChange={handleQuotaOptionChange}
          placeholder={t('setting.operation.quota.invitee_reward_placeholder')}
        />
      </Space>
      <div style={{ marginBottom: 24 }}>
        <Button
          type='primary'
          loading={loading}
          onClick={() => submitConfig('quota').then()}
        >
          {t('setting.operation.quota.buttons.save')}
        </Button>
      </div>
      <Divider />
      <Typography.Title level={5}>{t('setting.operation.knowledge.title')}</Typography.Title>
      <Typography.Paragraph type='secondary'>{t('setting.operation.knowledge.help')}</Typography.Paragraph>
      <Space size={24} wrap align='start' style={fieldStyle}>
        <div>
          <label style={labelStyle} htmlFor='knowledge-file-mib'>{t('setting.operation.knowledge.file')}</label>
          <InputNumber id='knowledge-file-mib' min={1} max={64} precision={0} value={fileMiB}
            disabled={!knowledgeReady || savingKnowledgeLimits}
            onChange={(value) => setKnowledgeLimits((limits) => ({ ...limits, fileMiB: value }))} />
        </div>
        <div>
          <label style={labelStyle} htmlFor='knowledge-library-mib'>{t('setting.operation.knowledge.library')}</label>
          <InputNumber id='knowledge-library-mib' min={1} max={1024} precision={0} value={libraryMiB}
            disabled={!knowledgeReady || savingKnowledgeLimits}
            onChange={(value) => setKnowledgeLimits((limits) => ({ ...limits, libraryMiB: value }))} />
        </div>
      </Space>
      <div>
        <Button type='primary' loading={savingKnowledgeLimits} disabled={!knowledgeReady || !canSaveKnowledgeLimits}
          onClick={async () => {
            setSavingKnowledgeLimits(true);
            try {
              const { data } = await API.put('/api/option/', {
                key: knowledgeLimitsKey, value: JSON.stringify(knowledgeLimits),
              });
              if (data.success) showSuccess(t('setting.operation.knowledge.saved'));
              else showError(t('setting.operation.knowledge.failed'));
            } catch {
              showError(t('setting.operation.knowledge.failed'));
            } finally {
              setSavingKnowledgeLimits(false);
            }
          }}>
          {t('setting.operation.knowledge.save')}
        </Button>
      </div>
      <Divider />
      <Typography.Title level={5}>
        {t('setting.operation.ratio.title')}
      </Typography.Title>
      <PriceEditor
        modelRatio={inputs.ModelRatio}
        completionRatio={inputs.CompletionRatio}
        modelCachePrices={inputs.ModelCachePrices}
        groupRatio={inputs.GroupRatio}
        onChange={(patch) => setInputs((current) => ({ ...current, ...patch }))}
      />
      <Checkbox
        checked={advanced}
        onChange={(e) => setAdvanced(e.target.checked)}
        style={{ display: 'block', margin: '16px 0' }}
      >
        {t('setting.operation.ratio.advanced')}
      </Checkbox>
      {advanced && (
        <>
          <div style={fieldStyle}>
            <label style={labelStyle}>
              {t('setting.operation.ratio.model.title')}
            </label>
            <Input.TextArea
              onChange={(e) => handleInputChange('ModelRatio', e.target.value)}
              style={{ fontFamily: 'JetBrains Mono, Consolas' }}
              autoComplete='new-password'
              value={inputs.ModelRatio}
              placeholder={t('setting.operation.ratio.model.placeholder')}
              rows={10}
            />
          </div>
          <div style={fieldStyle}>
            <label style={labelStyle}>
              {t('setting.operation.ratio.completion.title')}
            </label>
            <Input.TextArea
              onChange={(e) =>
                handleInputChange('CompletionRatio', e.target.value)
              }
              style={{ fontFamily: 'JetBrains Mono, Consolas' }}
              autoComplete='new-password'
              value={inputs.CompletionRatio}
              placeholder={t('setting.operation.ratio.completion.placeholder')}
              rows={10}
            />
          </div>
          <div style={fieldStyle}>
            <label style={labelStyle}>
              {t('setting.operation.ratio.group.title')}
            </label>
            <Input.TextArea
              onChange={(e) => handleInputChange('GroupRatio', e.target.value)}
              style={{ fontFamily: 'JetBrains Mono, Consolas' }}
              autoComplete='new-password'
              value={inputs.GroupRatio}
              placeholder={t('setting.operation.ratio.group.placeholder')}
              rows={10}
            />
          </div>
        </>
      )}
      <div style={{ marginBottom: 24 }}>
        <Button
          type='primary'
          loading={loading}
          onClick={() => submitConfig('ratio').then()}
        >
          {t('setting.operation.ratio.buttons.save')}
        </Button>
      </div>
      <Divider />
      <Typography.Title level={5}>
        {t('setting.operation.log.title')}
      </Typography.Title>
      <Checkbox
        checked={inputs.LogConsumeEnabled === 'true'}
        onChange={(e) => handleInputChange('LogConsumeEnabled', e.target.checked ? 'true' : 'false')}
        style={{ display: 'block', marginBottom: 12 }}
      >
        {t('setting.operation.log.enable_consume')}
      </Checkbox>
      <div style={fieldStyle}>
        <label style={labelStyle}>{t('setting.operation.log.target_time')}</label>
        <Input
          value={historyTimestamp}
          type='datetime-local'
          style={{ maxWidth: 250 }}
          onChange={(e) => setHistoryTimestamp(e.target.value)}
        />
      </div>
      <div style={{ marginBottom: 24 }}>
        <Button
          danger
          loading={loading}
          onClick={() => deleteHistoryLogs().then()}
        >
          {t('setting.operation.log.buttons.clean')}
        </Button>
      </div>
      <Divider />
      <Typography.Title level={5}>
        {t('setting.operation.monitor.title')}
      </Typography.Title>
      <Space size={24} wrap align='start' style={{ marginBottom: 12 }}>
        <div>
          <label style={labelStyle}>
            {t('setting.operation.monitor.max_response_time')}
          </label>
          <Input
            autoComplete='new-password'
            value={inputs.ChannelDisableThreshold}
            type='number'
            min={0}
            style={{ width: 200 }}
            placeholder={t(
              'setting.operation.monitor.max_response_time_placeholder'
            )}
            onChange={(e) =>
              handleInputChange('ChannelDisableThreshold', e.target.value)
            }
          />
        </div>
        <QuotaInput
          label={t('setting.operation.monitor.quota_reminder')}
          name='QuotaRemindThreshold'
          quota={Number(inputs.QuotaRemindThreshold) || 0}
          onQuotaChange={handleQuotaOptionChange}
          placeholder={t('setting.operation.monitor.quota_reminder_placeholder')}
        />
      </Space>
      <Space size={24} wrap style={{ display: 'flex', marginBottom: 12 }}>
        <Checkbox
          checked={inputs.AutomaticDisableChannelEnabled === 'true'}
          onChange={(e) =>
            handleInputChange(
              'AutomaticDisableChannelEnabled',
              e.target.checked ? 'true' : 'false'
            )
          }
        >
          {t('setting.operation.monitor.auto_disable')}
        </Checkbox>
        <Checkbox
          checked={inputs.AutomaticEnableChannelEnabled === 'true'}
          onChange={(e) =>
            handleInputChange(
              'AutomaticEnableChannelEnabled',
              e.target.checked ? 'true' : 'false'
            )
          }
        >
          {t('setting.operation.monitor.auto_enable')}
        </Checkbox>
      </Space>
      <div style={{ marginBottom: 24 }}>
        <Button
          type='primary'
          loading={loading}
          onClick={() => submitConfig('monitor').then()}
        >
          {t('setting.operation.monitor.buttons.save')}
        </Button>
      </div>
      <Divider />
      <Typography.Title level={5}>
        {t('setting.operation.general.title')}
      </Typography.Title>
      <Space size={24} wrap align='start' style={{ marginBottom: 12 }}>
        <div>
          <label style={labelStyle}>
            {t('setting.operation.general.topup_link')}
          </label>
          <Input
            autoComplete='new-password'
            value={inputs.TopUpLink}
            placeholder={t('setting.operation.general.topup_link_placeholder')}
            onChange={(e) => handleInputChange('TopUpLink', e.target.value)}
            style={{ width: 280 }}
          />
        </div>
        <div>
          <label style={labelStyle}>
            {t('setting.operation.general.chat_link')}
          </label>
          <Input
            autoComplete='new-password'
            value={inputs.ChatLink}
            placeholder={t('setting.operation.general.chat_link_placeholder')}
            onChange={(e) => handleInputChange('ChatLink', e.target.value)}
            style={{ width: 280 }}
          />
        </div>
        <div>
          <label style={labelStyle}>
            {t('setting.operation.general.quota_per_unit')}
          </label>
          <Input
            autoComplete='new-password'
            value={inputs.QuotaPerUnit}
            type='number'
            step='0.01'
            placeholder={t('setting.operation.general.quota_per_unit_placeholder')}
            onChange={(e) => handleInputChange('QuotaPerUnit', e.target.value)}
            style={{ width: 160 }}
          />
        </div>
        <div>
          <label style={labelStyle}>
            {t('setting.operation.general.retry_times')}
          </label>
          <Input
            type='number'
            step='1'
            min={0}
            autoComplete='new-password'
            value={inputs.RetryTimes}
            placeholder={t('setting.operation.general.retry_times_placeholder')}
            onChange={(e) => handleInputChange('RetryTimes', e.target.value)}
            style={{ width: 120 }}
          />
        </div>
      </Space>
      <Space size={24} wrap style={{ display: 'flex', marginBottom: 12 }}>
        <Checkbox
          checked={inputs.DisplayInCurrencyEnabled === 'true'}
          onChange={(e) =>
            handleInputChange(
              'DisplayInCurrencyEnabled',
              e.target.checked ? 'true' : 'false'
            )
          }
        >
          {t('setting.operation.general.display_in_currency')}
        </Checkbox>
        <Checkbox
          checked={inputs.DisplayTokenStatEnabled === 'true'}
          onChange={(e) =>
            handleInputChange(
              'DisplayTokenStatEnabled',
              e.target.checked ? 'true' : 'false'
            )
          }
        >
          {t('setting.operation.general.display_token_stat')}
        </Checkbox>
        <Checkbox
          checked={inputs.ApproximateTokenEnabled === 'true'}
          onChange={(e) =>
            handleInputChange(
              'ApproximateTokenEnabled',
              e.target.checked ? 'true' : 'false'
            )
          }
        >
          {t('setting.operation.general.approximate_token')}
        </Checkbox>
      </Space>
      <Button
        type='primary'
        loading={loading}
        onClick={() => submitConfig('general').then()}
      >
        {t('setting.operation.general.buttons.save')}
      </Button>
    </div>
  );
};

export default OperationSetting;
