import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Card, Checkbox, Input, Select, Space } from 'antd';
import { useNavigate, useParams } from 'react-router-dom';
import { API, getChannelModels, showError, showInfo, showSuccess, verifyJSON } from '../../helpers';
import { BASE_URL_INPUT_KINDS, CHANNEL_OPTIONS, profileFitsChannelType } from '../../constants';
import { renderChannelTip } from '../../helpers/render';

const MODEL_MAPPING_EXAMPLE = {
  'gpt-3.5-turbo-0301': 'gpt-3.5-turbo',
  'gpt-4-0314': 'gpt-4',
  'gpt-4-32k-0314': 'gpt-4-32k',
};

const fieldStyle = { marginBottom: 16 };
const labelStyle = { display: 'block', marginBottom: 6, fontWeight: 500 };

const EditChannel = () => {
  const { t } = useTranslation();
  const params = useParams();
  const navigate = useNavigate();
  const channelId = params.id;
  const isEdit = channelId !== undefined;
  const [loading, setLoading] = useState(isEdit);
  const handleCancel = () => {
    navigate('/channel');
  };

  const originInputs = {
    name: '',
    type: 1,
    key: '',
    base_url: '',
    model_mapping: '',
    system_prompt: '',
    models: [],
    groups: ['default'],
    upstream_account_id: 0,
  };
  const [batch, setBatch] = useState(false);
  const [inputs, setInputs] = useState(originInputs);
  const [originModelOptions, setOriginModelOptions] = useState([]);
  const [modelOptions, setModelOptions] = useState([]);
  const [groupOptions, setGroupOptions] = useState([]);
  const [accountOptions, setAccountOptions] = useState([]);
  const [basicModels, setBasicModels] = useState([]);
  const [fullModels, setFullModels] = useState([]);
  const [customModel, setCustomModel] = useState('');
  const [fetchingModels, setFetchingModels] = useState(false);
  const [config, setConfig] = useState({
    region: '',
    sk: '',
    ak: '',
    user_id: '',
    api_version: '',
    vertex_ai_project_id: '',
    vertex_ai_adc: '',
    cloudflare_gateway_id: '',
    cloudflare_gateway_token: '',
  });

  const changeInputs = (name, value) => {
    setInputs((inputs) => ({ ...inputs, [name]: value }));
    if (name === 'type') {
      let localModels = getChannelModels(value);
      if (inputs.models.length === 0) {
        setInputs((inputs) => ({ ...inputs, models: localModels }));
      }
      setBasicModels(localModels);
    }
  };

  const changeConfig = (name, value) => {
    setConfig((inputs) => ({ ...inputs, [name]: value }));
  };

  const loadChannel = async () => {
    let res = await API.get(`/api/channel/${channelId}`);
    const { success, message, data } = res.data;
    if (success) {
      if (data.models === '') {
        data.models = [];
      } else {
        data.models = data.models.split(',');
      }
      if (data.group === '') {
        data.groups = [];
      } else {
        data.groups = data.group.split(',');
      }
      if (data.model_mapping !== '') {
        data.model_mapping = JSON.stringify(
          JSON.parse(data.model_mapping),
          null,
          2
        );
      }
      setInputs({
        ...data,
        upstream_account_id: data.upstream_account_id || 0,
      });
      if (data.config !== '') {
        setConfig(JSON.parse(data.config));
      }
      setBasicModels(getChannelModels(data.type));
    } else {
      showError(message);
    }
    setLoading(false);
  };

  const fetchModels = async () => {
    try {
      let res = await API.get(`/api/channel/models`);
      // A malformed or null payload must degrade to an empty option list
      // instead of crashing the form.
      if (!Array.isArray(res.data.data)) {
        return;
      }
      let localModelOptions = res.data.data.map((model) => ({
        key: model.id,
        label: model.id,
        value: model.id,
      }));
      setOriginModelOptions(localModelOptions);
      setFullModels(res.data.data.map((model) => model.id));
    } catch (error) {
      showError(error.message);
    }
  };

  const fetchGroups = async () => {
    try {
      let res = await API.get(`/api/group/`);
      if (res.data.success && Array.isArray(res.data.data)) {
        setGroupOptions(
          res.data.data.map((group) => ({
            key: group,
            label: group,
            value: group,
          }))
        );
      }
    } catch (error) {
      showError(error.message);
    }
  };

  const fetchUpstreamAccounts = async () => {
    try {
      let res = await API.get(`/api/upstream_account/?p=0`);
      if (res.data.success && Array.isArray(res.data.data)) {
        setAccountOptions(res.data.data);
      }
    } catch (error) {
      showError(error.message);
    }
  };

  useEffect(() => {
    let localModelOptions = [...originModelOptions];
    inputs.models.forEach((model) => {
      if (!localModelOptions.find((option) => option.key === model)) {
        localModelOptions.push({
          key: model,
          label: model,
          value: model,
        });
      }
    });
    setModelOptions(localModelOptions);
  }, [originModelOptions, inputs.models]);

  useEffect(() => {
    if (isEdit) {
      loadChannel().then();
    } else {
      let localModels = getChannelModels(inputs.type);
      setBasicModels(localModels);
    }
    fetchModels().then();
    fetchGroups().then();
    fetchUpstreamAccounts().then();
  }, []);

  const submit = async () => {
    if (inputs.key === '') {
      if (config.ak !== '' && config.sk !== '' && config.region !== '') {
        inputs.key = `${config.ak}|${config.sk}|${config.region}`;
      } else if (
        config.region !== '' &&
        config.vertex_ai_project_id !== '' &&
        config.vertex_ai_adc !== ''
      ) {
        inputs.key = `${config.region}|${config.vertex_ai_project_id}|${config.vertex_ai_adc}`;
      }
    }
    if (!isEdit && inputs.name === '') {
      showInfo(t('channel.edit.messages.name_required'));
      return;
    }
    if (!isEdit && inputs.key === '' && !(inputs.upstream_account_id > 0)) {
      showInfo(t('channel.edit.messages.name_required'));
      return;
    }
    if (inputs.models.length === 0) {
      showInfo(t('channel.edit.messages.models_required'));
      return;
    }
    if (inputs.model_mapping !== '' && !verifyJSON(inputs.model_mapping)) {
      showInfo(t('channel.edit.messages.model_mapping_invalid'));
      return;
    }
    let localInputs = { ...inputs };
    if (localInputs.key === 'undefined|undefined|undefined') {
      localInputs.key = ''; // prevent potential bug
    }
    if (localInputs.base_url && localInputs.base_url.endsWith('/')) {
      localInputs.base_url = localInputs.base_url.slice(
        0,
        localInputs.base_url.length - 1
      );
    }
    const submitConfig = { ...config };
    if (localInputs.type === 3 && submitConfig.api_version === '') {
      submitConfig.api_version = 'v1';
    }
    let res;
    const payload = {
      ...localInputs,
      models: localInputs.models.join(','),
      group: localInputs.groups.join(','),
      config: JSON.stringify(submitConfig),
      // 0 means an ordinary API-key channel; the backend rejects 0 as a
      // binding, so send null to leave the channel unbound.
      upstream_account_id:
        localInputs.upstream_account_id > 0
          ? localInputs.upstream_account_id
          : null,
    };
    if (isEdit) {
      res = await API.put(`/api/channel/`, {
        ...payload,
        id: parseInt(channelId),
      });
    } else {
      res = await API.post(`/api/channel/`, payload);
    }
    const { success, message } = res.data;
    if (success) {
      if (isEdit) {
        showSuccess(t('channel.edit.messages.update_success'));
      } else {
        showSuccess(t('channel.edit.messages.create_success'));
        setInputs(originInputs);
      }
    } else {
      showError(message);
    }
  };

  const addCustomModel = () => {
    if (customModel.trim() === '') return;
    if (inputs.models.includes(customModel)) return;
    let localModels = [...inputs.models];
    localModels.push(customModel);
    let localModelOptions = [];
    localModelOptions.push({
      key: customModel,
      label: customModel,
      value: customModel,
    });
    setModelOptions((modelOptions) => {
      return [...modelOptions, ...localModelOptions];
    });
    setCustomModel('');
    changeInputs('models', localModels);
  };

  // Pulls the vendor's live model list so the picker never goes stale. For
  // an unsaved channel the form's type/base_url/key travel with the request.
  const fetchUpstreamModels = async () => {
    setFetchingModels(true);
    try {
      const payload = isEdit
        ? { id: parseInt(channelId) }
        : {
            type: inputs.type,
            base_url: inputs.base_url,
            key: inputs.key,
          };
      const res = await API.post('/api/channel/upstream_models', payload);
      const { success, message, data, synced_prices: syncedPrices } = res.data;
      if (success && Array.isArray(data)) {
        const merged = Array.from(new Set([...inputs.models, ...data]));
        changeInputs('models', merged);
        setModelOptions((options) => {
          const known = new Set(options.map((option) => option.value));
          const fresh = data
            .filter((model) => !known.has(model))
            .map((model) => ({ key: model, label: model, value: model }));
          return [...options, ...fresh];
        });
        // Vendors that publish prices (OpenRouter) sync them alongside the
        // list; the count tells the operator the price book grew too.
        if (typeof syncedPrices === 'number' && syncedPrices > 0) {
          showSuccess(
            t('channel.edit.messages.upstream_models_fetched_with_prices', {
              count: data.length,
              synced: syncedPrices,
            }),
          );
        } else {
          showSuccess(t('channel.edit.messages.upstream_models_fetched', { count: data.length }));
        }
      } else {
        showError(message);
      }
    } catch (error) {
      showError(error.message);
    }
    setFetchingModels(false);
  };

  const cloudflareGateway = Boolean(
    config.cloudflare_gateway_id ||
      (inputs.base_url || '').includes('gateway.ai.cloudflare.com')
  );

  return (
    <Card
      title={isEdit ? t('channel.edit.title_edit') : t('channel.edit.title_create')}
      loading={loading}
    >
      <div style={{ maxWidth: 760 }}>
        <div style={fieldStyle}>
          <label style={labelStyle}>
            {t('channel.edit.type')} <span style={{ color: '#ff4d4f' }}>*</span>
          </label>
          <Select
            showSearch
            style={{ width: '100%' }}
            options={CHANNEL_OPTIONS.map((option) => ({
              key: option.value,
              label: option.text,
              value: option.value,
            }))}
            optionFilterProp='label'
            value={inputs.type}
            onChange={(value) => changeInputs('type', value)}
          />
        </div>
        <div style={fieldStyle}>
          <label style={labelStyle}>
            {t('channel.edit.name')}{' '}
            <span style={{ color: '#ff4d4f' }}>*</span>
          </label>
          <Input
            placeholder={t('channel.edit.name_placeholder')}
            onChange={(e) => changeInputs('name', e.target.value)}
            value={inputs.name}
          />
        </div>
        <div style={fieldStyle}>
          <label style={labelStyle}>
            {t('channel.edit.group')}{' '}
            <span style={{ color: '#ff4d4f' }}>*</span>
          </label>
          <Select
            mode='tags'
            style={{ width: '100%' }}
            placeholder={t('channel.edit.group_placeholder')}
            value={inputs.groups}
            onChange={(value) => changeInputs('groups', value)}
            options={groupOptions}
          />
        </div>
        {renderChannelTip(inputs.type)}

        <div style={fieldStyle}>
          <label style={labelStyle}>{t('channel.edit.upstream_account')}</label>
          <Select
            showSearch
            optionFilterProp='label'
            style={{ width: '100%' }}
            options={[
              {
                key: 0,
                label: t('channel.edit.upstream_account_none'),
                value: 0,
              },
              ...accountOptions
                .filter((account) =>
                  profileFitsChannelType(account.profile, inputs.type)
                )
                .map((account) => ({
                  key: account.id,
                  label: `${account.name}（${account.profile}）`,
                  value: account.id,
                })),
            ]}
            value={inputs.upstream_account_id || 0}
            onChange={(value) => changeInputs('upstream_account_id', value)}
          />
        </div>
        {inputs.upstream_account_id > 0 && (
          <Alert
            type='info'
            showIcon
            message={t('channel.edit.upstream_account_tip')}
            style={{ marginBottom: 16 }}
          />
        )}

        {/* Azure OpenAI specific fields */}
        {inputs.type === 3 && (
          <>
            <Alert
              type='info'
              showIcon
              message='模型映射的目标值填写实际部署名称，名称中的点会原样保留。API 版本留空或填写 v1 使用 Azure v1；填写日期版本使用部署式接口。'
              style={{ marginBottom: 16 }}
            />
            <div style={fieldStyle}>
              <label style={labelStyle}>AZURE_OPENAI_ENDPOINT</label>
              <Input
                placeholder='请输入 AZURE_OPENAI_ENDPOINT，例如：https://docs-test-001.openai.azure.com'
                onChange={(e) => changeInputs('base_url', e.target.value)}
                value={inputs.base_url}
                autoComplete='new-password'
              />
            </div>
            <div style={fieldStyle}>
              <label style={labelStyle}>默认 API 版本</label>
              <Input
                placeholder='留空或 v1；旧部署可填写受支持的日期版本'
                onChange={(e) => changeConfig('api_version', e.target.value)}
                value={config.api_version}
                autoComplete='new-password'
              />
            </div>
          </>
        )}

        {BASE_URL_INPUT_KINDS.has(inputs.type) && (
          <div style={fieldStyle}>
            <label style={labelStyle}>{t('channel.edit.base_url')}</label>
            <Input
              placeholder={t('channel.edit.base_url_placeholder')}
              onChange={(e) => changeInputs('base_url', e.target.value)}
              value={inputs.base_url}
              autoComplete='new-password'
            />
          </div>
        )}

        <div style={fieldStyle}>
          <label style={labelStyle}>
            {t('channel.edit.models')}{' '}
            <span style={{ color: '#ff4d4f' }}>*</span>
          </label>
          <Select
            mode='multiple'
            showSearch
            style={{ width: '100%' }}
            placeholder={t('channel.edit.models_placeholder')}
            value={inputs.models}
            onChange={(value) => changeInputs('models', value)}
            options={modelOptions}
          />
        </div>
        <div style={{ marginBottom: 12 }}>
          <Space wrap>
            {basicModels.length > 0 && (
              <Button
                onClick={() => {
                  changeInputs('models', basicModels);
                }}
              >
                {t('channel.edit.buttons.fill_models')}
              </Button>
            )}
            <Button
              onClick={() => {
                changeInputs('models', fullModels);
              }}
            >
              {t('channel.edit.buttons.fill_all')}
            </Button>
            <Button loading={fetchingModels} onClick={fetchUpstreamModels}>
              {t('channel.edit.buttons.fetch_models')}
            </Button>
            <Button
              onClick={() => {
                changeInputs('models', []);
              }}
            >
              {t('channel.edit.buttons.clear')}
            </Button>
          </Space>
          <Space.Compact style={{ width: '100%', marginTop: 8 }}>
            <Input
              placeholder={t('channel.edit.buttons.custom_placeholder')}
              value={customModel}
              onChange={(e) => setCustomModel(e.target.value)}
              onPressEnter={() => {
                addCustomModel();
              }}
            />
            <Button onClick={addCustomModel}>
              {t('channel.edit.buttons.add_custom')}
            </Button>
          </Space.Compact>
        </div>
        <div style={fieldStyle}>
          <label style={labelStyle}>{t('channel.edit.model_mapping')}</label>
          <Input.TextArea
            placeholder={`${t('channel.edit.model_mapping_placeholder')}\n${JSON.stringify(MODEL_MAPPING_EXAMPLE, null, 2)}`}
            onChange={(e) => changeInputs('model_mapping', e.target.value)}
            value={inputs.model_mapping}
            rows={6}
            style={{ fontFamily: 'JetBrains Mono, Consolas' }}
            autoComplete='new-password'
          />
        </div>
        <div style={fieldStyle}>
          <label style={labelStyle}>{t('channel.edit.system_prompt')}</label>
          <Input.TextArea
            placeholder={t('channel.edit.system_prompt_placeholder')}
            onChange={(e) => changeInputs('system_prompt', e.target.value)}
            value={inputs.system_prompt}
            rows={6}
            style={{ fontFamily: 'JetBrains Mono, Consolas' }}
            autoComplete='new-password'
          />
        </div>
        {inputs.type === 33 && (
          <>
            <div style={fieldStyle}>
              <label style={labelStyle}>Bedrock 推理接口</label>
              <Select
                style={{ width: '100%' }}
                value={config.api_version || ''}
                onChange={(value) => changeConfig('api_version', value)}
                options={[
                  { key: 'invoke', label: 'Claude InvokeModel', value: '' },
                  { key: 'converse', label: 'Converse（跨模型）', value: 'converse' },
                  { key: 'messages', label: 'Claude Messages', value: 'messages' },
                ]}
              />
            </div>
            <div style={fieldStyle}>
              <label style={labelStyle}>
                Region <span style={{ color: '#ff4d4f' }}>*</span>
              </label>
              <Input
                placeholder={t('channel.edit.aws_region_placeholder')}
                onChange={(e) => changeConfig('region', e.target.value)}
                value={config.region}
              />
            </div>
            <div style={fieldStyle}>
              <label style={labelStyle}>
                AK <span style={{ color: '#ff4d4f' }}>*</span>
              </label>
              <Input
                placeholder={t('channel.edit.aws_ak_placeholder')}
                onChange={(e) => changeConfig('ak', e.target.value)}
                value={config.ak}
              />
            </div>
            <div style={fieldStyle}>
              <label style={labelStyle}>
                SK <span style={{ color: '#ff4d4f' }}>*</span>
              </label>
              <Input
                placeholder={t('channel.edit.aws_sk_placeholder')}
                onChange={(e) => changeConfig('sk', e.target.value)}
                value={config.sk}
              />
            </div>
          </>
        )}
        {inputs.type === 42 && (
          <>
            <div style={fieldStyle}>
              <label style={labelStyle}>
                Region <span style={{ color: '#ff4d4f' }}>*</span>
              </label>
              <Input
                placeholder={t('channel.edit.vertex_region_placeholder')}
                onChange={(e) => changeConfig('region', e.target.value)}
                value={config.region}
              />
            </div>
            <div style={fieldStyle}>
              <label style={labelStyle}>
                {t('channel.edit.vertex_project_id')}{' '}
                <span style={{ color: '#ff4d4f' }}>*</span>
              </label>
              <Input
                placeholder={t('channel.edit.vertex_project_id_placeholder')}
                onChange={(e) =>
                  changeConfig('vertex_ai_project_id', e.target.value)
                }
                value={config.vertex_ai_project_id}
              />
            </div>
            <div style={fieldStyle}>
              <label style={labelStyle}>
                {t('channel.edit.vertex_credentials')}{' '}
                <span style={{ color: '#ff4d4f' }}>*</span>
              </label>
              <Input
                placeholder={t('channel.edit.vertex_credentials_placeholder')}
                onChange={(e) => changeConfig('vertex_ai_adc', e.target.value)}
                value={config.vertex_ai_adc}
              />
            </div>
          </>
        )}
        {inputs.type === 34 && (
          <div style={fieldStyle}>
            <label style={labelStyle}>
              {t('channel.edit.user_id')}{' '}
              <span style={{ color: '#ff4d4f' }}>*</span>
            </label>
            <Input
              placeholder={t('channel.edit.user_id_placeholder')}
              onChange={(e) => changeConfig('user_id', e.target.value)}
              value={config.user_id}
            />
          </div>
        )}
        {inputs.type !== 33 &&
          inputs.type !== 42 &&
          (batch ? (
            <div style={fieldStyle}>
              <label style={labelStyle}>
                {t('channel.edit.key')}{' '}
                <span style={{ color: '#ff4d4f' }}>*</span>
              </label>
              <Input.TextArea
                placeholder={t('channel.edit.batch_placeholder')}
                onChange={(e) => changeInputs('key', e.target.value)}
                value={inputs.key}
                rows={6}
                style={{ fontFamily: 'JetBrains Mono, Consolas' }}
                autoComplete='new-password'
              />
            </div>
          ) : (
            <div style={fieldStyle}>
              <label style={labelStyle}>
                {t('channel.edit.key')}{' '}
                <span style={{ color: '#ff4d4f' }}>*</span>
              </label>
              <Input.Password
                placeholder={t('channel.edit.key_prompts.default')}
                onChange={(e) => changeInputs('key', e.target.value)}
                value={inputs.key}
                autoComplete='new-password'
              />
            </div>
          ))}
        {inputs.type === 37 && (
          <>
            <div style={fieldStyle}>
              <label style={labelStyle}>
                Account ID <span style={{ color: '#ff4d4f' }}>*</span>
              </label>
              <Input
                placeholder='请输入 Account ID，例如：d8d7c61dbc334c32d3ced580e4bf42b4'
                onChange={(e) => changeConfig('user_id', e.target.value)}
                value={config.user_id}
              />
            </div>
            <Checkbox
              checked={cloudflareGateway}
              onChange={(e) => {
                const checked = e.target.checked;
                setConfig((current) => ({
                  ...current,
                  cloudflare_gateway_id: checked
                    ? current.cloudflare_gateway_id || 'default'
                    : '',
                  cloudflare_gateway_token: checked
                    ? current.cloudflare_gateway_token
                    : '',
                }));
                setInputs((current) => ({ ...current, base_url: '' }));
              }}
              style={{ display: 'block', marginBottom: 12 }}
            >
              使用 AI Gateway
            </Checkbox>
            {cloudflareGateway && (
              <>
                <div style={fieldStyle}>
                  <label style={labelStyle}>
                    Gateway ID <span style={{ color: '#ff4d4f' }}>*</span>
                  </label>
                  <Input
                    value={config.cloudflare_gateway_id || ''}
                    onChange={(e) =>
                      changeConfig('cloudflare_gateway_id', e.target.value)
                    }
                    placeholder='例如 default'
                  />
                </div>
                <div style={fieldStyle}>
                  <label style={labelStyle}>Gateway Token（可选）</label>
                  <Input.Password
                    value={config.cloudflare_gateway_token || ''}
                    onChange={(e) =>
                      changeConfig('cloudflare_gateway_token', e.target.value)
                    }
                    autoComplete='new-password'
                  />
                </div>
                <Alert
                  type='info'
                  showIcon
                  message='网关地址由 Account ID 和 Gateway ID 自动生成，无需填写。'
                  style={{ marginBottom: 16 }}
                />
              </>
            )}
          </>
        )}
        {inputs.type !== 33 && !isEdit && (
          <Checkbox
            checked={batch}
            onChange={(e) => setBatch(e.target.checked)}
            style={{ display: 'block', marginBottom: 12 }}
          >
            {t('channel.edit.batch')}
          </Checkbox>
        )}
        {inputs.type !== 3 &&
          inputs.type !== 33 &&
          inputs.type !== 37 &&
          inputs.type !== 50 &&
          inputs.type !== 53 && (
            <div style={fieldStyle}>
              <label style={labelStyle}>{t('channel.edit.proxy_url')}</label>
              <Input
                placeholder={t('channel.edit.proxy_url_placeholder')}
                onChange={(e) => changeInputs('base_url', e.target.value)}
                value={inputs.base_url}
                autoComplete='new-password'
              />
            </div>
          )}
        <Space>
          <Button onClick={handleCancel}>
            {t('channel.edit.buttons.cancel')}
          </Button>
          <Button type='primary' onClick={submit} loading={loading}>
            {t('channel.edit.buttons.submit')}
          </Button>
        </Space>
      </div>
    </Card>
  );
};

export default EditChannel;
