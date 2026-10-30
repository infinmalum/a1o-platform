import React, {useEffect, useState} from 'react';
import {useTranslation} from 'react-i18next';
import {Button, Card, Form, Input, Message} from 'semantic-ui-react';
import {useNavigate, useParams} from 'react-router-dom';
import {API, copy, getChannelModels, showError, showInfo, showSuccess, verifyJSON,} from '../../helpers';
import {BASE_URL_INPUT_KINDS, CHANNEL_OPTIONS} from '../../constants';
import {renderChannelTip} from '../../helpers/render';

const MODEL_MAPPING_EXAMPLE = {
  'gpt-3.5-turbo-0301': 'gpt-3.5-turbo',
  'gpt-4-0314': 'gpt-4',
  'gpt-4-32k-0314': 'gpt-4-32k',
};

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
  });
  const handleInputChange = (e, { name, value }) => {
    setInputs((inputs) => ({ ...inputs, [name]: value }));
    if (name === 'type') {
      let localModels = getChannelModels(value);
      if (inputs.models.length === 0) {
        setInputs((inputs) => ({ ...inputs, models: localModels }));
      }
      setBasicModels(localModels);
    }
  };

  const handleConfigChange = (e, { name, value }) => {
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
        text: model.id,
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
            text: group,
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
        setAccountOptions(
          res.data.data.map((account) => ({
            key: account.id,
            text: `${account.name}（${account.profile}）`,
            value: account.id,
          }))
        );
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
          text: model,
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
      submitConfig.api_version = '2024-03-01-preview';
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
      text: customModel,
      value: customModel,
    });
    setModelOptions((modelOptions) => {
      return [...modelOptions, ...localModelOptions];
    });
    setCustomModel('');
    handleInputChange(null, { name: 'models', value: localModels });
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
        handleInputChange(null, { name: 'models', value: merged });
        setModelOptions((options) => {
          const known = new Set(options.map((option) => option.value));
          const fresh = data
            .filter((model) => !known.has(model))
            .map((model) => ({ key: model, text: model, value: model }));
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

  return (
    <div className='dashboard-container'>
      <Card fluid className='chart-card'>
        <Card.Content>
          <Card.Header className='header'>
            {isEdit
              ? t('channel.edit.title_edit')
              : t('channel.edit.title_create')}
          </Card.Header>
          <Form loading={loading} autoComplete='new-password'>
            <Form.Field>
              <Form.Select
                label={t('channel.edit.type')}
                name='type'
                required
                search
                options={CHANNEL_OPTIONS}
                value={inputs.type}
                onChange={handleInputChange}
              />
            </Form.Field>
            <Form.Field>
              <Form.Input
                label={t('channel.edit.name')}
                name='name'
                placeholder={t('channel.edit.name_placeholder')}
                onChange={handleInputChange}
                value={inputs.name}
                required
              />
            </Form.Field>
            <Form.Field>
              <Form.Dropdown
                label={t('channel.edit.group')}
                placeholder={t('channel.edit.group_placeholder')}
                name='groups'
                required
                fluid
                multiple
                selection
                allowAdditions
                additionLabel={t('channel.edit.group_addition')}
                onChange={handleInputChange}
                value={inputs.groups}
                autoComplete='new-password'
                options={groupOptions}
              />
            </Form.Field>
            {renderChannelTip(inputs.type)}

            <Form.Field>
              <Form.Select
                label={t('channel.edit.upstream_account')}
                name='upstream_account_id'
                search
                options={[
                  {
                    key: 0,
                    text: t('channel.edit.upstream_account_none'),
                    value: 0,
                  },
                  ...accountOptions,
                ]}
                value={inputs.upstream_account_id || 0}
                onChange={handleInputChange}
              />
            </Form.Field>
            {inputs.upstream_account_id > 0 && (
              <Message info>
                {t('channel.edit.upstream_account_tip')}
              </Message>
            )}

            {/* Azure OpenAI specific fields */}
            {inputs.type === 3 && (
              <>
                <Message>
                  注意，<strong>模型部署名称必须和模型名称保持一致</strong>
                  ，因为网关会把请求体中的 model
                  参数替换为你的部署名称（模型名称中的点会被剔除）。
                </Message>
                <Form.Field>
                  <Form.Input
                    label='AZURE_OPENAI_ENDPOINT'
                    name='base_url'
                    placeholder='请输入 AZURE_OPENAI_ENDPOINT，例如：https://docs-test-001.openai.azure.com'
                    onChange={handleInputChange}
                    value={inputs.base_url}
                    autoComplete='new-password'
                  />
                </Form.Field>
                <Form.Field>
                  <Form.Input
                    label='默认 API 版本'
                    name='api_version'
                    placeholder='请输入默认 API 版本，例如：2024-03-01-preview，该配置可以被实际的请求查询参数所覆盖'
                    onChange={handleConfigChange}
                    value={config.api_version}
                    autoComplete='new-password'
                  />
                </Form.Field>
              </>
            )}

            {/* Coze's domestic and international platforms are separate
                accounts with separate tokens; the operator picks the site the
                token was issued on instead of typing a base URL. */}
            {inputs.type === 34 && (
              <Form.Field>
                <Form.Select
                  label='Coze 站点'
                  placeholder='选择令牌所属的 Coze 平台'
                  options={[
                    {key: 'cn', text: '国内站（coze.cn）', value: 'cn'},
                    {key: 'com', text: '国际站（coze.com）', value: 'com'},
                  ]}
                  value={
                    (inputs.base_url || '').includes('coze.cn') ? 'cn' : 'com'
                  }
                  onChange={(e, {value}) => {
                    handleInputChange(e, {
                      name: 'base_url',
                      value:
                        value === 'cn'
                          ? 'https://api.coze.cn'
                          : 'https://api.coze.com',
                    });
                  }}
                />
              </Form.Field>
            )}

            {/* DeepL's free and pro accounts live on separate fixed hosts;
                the operator picks the tier the auth key was issued for
                instead of typing a base URL. */}
            {inputs.type === 38 && (
              <Form.Field>
                <Form.Select
                  label='DeepL 账号类型'
                  placeholder='选择令牌所属的 DeepL 账号'
                  options={[
                    {
                      key: 'free',
                      text: '免费版（api-free.deepl.com）',
                      value: 'free',
                    },
                    {
                      key: 'pro',
                      text: 'Pro 版（api.deepl.com）',
                      value: 'pro',
                    },
                  ]}
                  value={
                    (inputs.base_url || '').includes('api.deepl.com') &&
                    !(inputs.base_url || '').includes('api-free')
                      ? 'pro'
                      : 'free'
                  }
                  onChange={(e, {value}) => {
                    handleInputChange(e, {
                      name: 'base_url',
                      value:
                        value === 'free'
                          ? 'https://api-free.deepl.com'
                          : 'https://api.deepl.com',
                    });
                  }}
                />
              </Form.Field>
            )}

            {BASE_URL_INPUT_KINDS.has(inputs.type) && (
              <Form.Field>
                <Form.Input
                  label={t('channel.edit.base_url')}
                  name='base_url'
                  placeholder={t('channel.edit.base_url_placeholder')}
                  onChange={handleInputChange}
                  value={inputs.base_url}
                  autoComplete='new-password'
                />
              </Form.Field>
            )}

            {inputs.type === 34 && (
              <Message>{t('channel.edit.coze_notice')}</Message>
            )}
            <Form.Field>
              <Form.Dropdown
                label={t('channel.edit.models')}
                placeholder={t('channel.edit.models_placeholder')}
                name='models'
                required
                fluid
                multiple
                search
                onLabelClick={(e, { value }) => {
                  copy(value).then();
                }}
                selection
                onChange={handleInputChange}
                value={inputs.models}
                autoComplete='new-password'
                options={modelOptions}
              />
            </Form.Field>
            <div style={{ lineHeight: '40px', marginBottom: '12px' }}>
              {basicModels.length > 0 && (
                <Button
                  type={'button'}
                  onClick={() => {
                    handleInputChange(null, {
                      name: 'models',
                      value: basicModels,
                    });
                  }}
                >
                  {t('channel.edit.buttons.fill_models')}
                </Button>
              )}
              <Button
                type={'button'}
                onClick={() => {
                  handleInputChange(null, {
                    name: 'models',
                    value: fullModels,
                  });
                }}
              >
                {t('channel.edit.buttons.fill_all')}
              </Button>
              <Button
                type={'button'}
                loading={fetchingModels}
                onClick={fetchUpstreamModels}
              >
                {t('channel.edit.buttons.fetch_models')}
              </Button>
              <Button
                type={'button'}
                onClick={() => {
                  handleInputChange(null, { name: 'models', value: [] });
                }}
              >
                {t('channel.edit.buttons.clear')}
              </Button>
              <Input
                action={
                  <Button type={'button'} onClick={addCustomModel}>
                    {t('channel.edit.buttons.add_custom')}
                  </Button>
                }
                placeholder={t('channel.edit.buttons.custom_placeholder')}
                value={customModel}
                onChange={(e, { value }) => {
                  setCustomModel(value);
                }}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    addCustomModel();
                    e.preventDefault();
                  }
                }}
              />
            </div>
            <>
              <Form.Field>
                <Form.TextArea
                  label={t('channel.edit.model_mapping')}
                  placeholder={`${t(
                    'channel.edit.model_mapping_placeholder'
                  )}\n${JSON.stringify(MODEL_MAPPING_EXAMPLE, null, 2)}`}
                  name='model_mapping'
                  onChange={handleInputChange}
                  value={inputs.model_mapping}
                  style={{
                    minHeight: 150,
                    fontFamily: 'JetBrains Mono, Consolas',
                  }}
                  autoComplete='new-password'
                />
              </Form.Field>
              <Form.Field>
                <Form.TextArea
                  label={t('channel.edit.system_prompt')}
                  placeholder={t('channel.edit.system_prompt_placeholder')}
                  name='system_prompt'
                  onChange={handleInputChange}
                  value={inputs.system_prompt}
                  style={{
                    minHeight: 150,
                    fontFamily: 'JetBrains Mono, Consolas',
                  }}
                  autoComplete='new-password'
                />
              </Form.Field>
            </>
            {inputs.type === 33 && (
              <Form.Field>
                <Form.Input
                  label='Region'
                  name='region'
                  required
                  placeholder={t('channel.edit.aws_region_placeholder')}
                  onChange={handleConfigChange}
                  value={config.region}
                  autoComplete=''
                />
                <Form.Input
                  label='AK'
                  name='ak'
                  required
                  placeholder={t('channel.edit.aws_ak_placeholder')}
                  onChange={handleConfigChange}
                  value={config.ak}
                  autoComplete=''
                />
                <Form.Input
                  label='SK'
                  name='sk'
                  required
                  placeholder={t('channel.edit.aws_sk_placeholder')}
                  onChange={handleConfigChange}
                  value={config.sk}
                  autoComplete=''
                />
              </Form.Field>
            )}
            {inputs.type === 42 && (
              <Form.Field>
                <Form.Input
                  label='Region'
                  name='region'
                  required
                  placeholder={t('channel.edit.vertex_region_placeholder')}
                  onChange={handleConfigChange}
                  value={config.region}
                  autoComplete=''
                />
                <Form.Input
                  label={t('channel.edit.vertex_project_id')}
                  name='vertex_ai_project_id'
                  required
                  placeholder={t('channel.edit.vertex_project_id_placeholder')}
                  onChange={handleConfigChange}
                  value={config.vertex_ai_project_id}
                  autoComplete=''
                />
                <Form.Input
                  label={t('channel.edit.vertex_credentials')}
                  name='vertex_ai_adc'
                  required
                  placeholder={t('channel.edit.vertex_credentials_placeholder')}
                  onChange={handleConfigChange}
                  value={config.vertex_ai_adc}
                  autoComplete=''
                />
              </Form.Field>
            )}
            {inputs.type === 34 && (
              <Form.Input
                label={t('channel.edit.user_id')}
                name='user_id'
                required
                placeholder={t('channel.edit.user_id_placeholder')}
                onChange={handleConfigChange}
                value={config.user_id}
                autoComplete=''
              />
            )}
            {inputs.type !== 33 &&
              inputs.type !== 42 &&
              (batch ? (
                <Form.Field>
                  <Form.TextArea
                    label={t('channel.edit.key')}
                    name='key'
                    required
                    placeholder={t('channel.edit.batch_placeholder')}
                    onChange={handleInputChange}
                    value={inputs.key}
                    style={{
                      minHeight: 150,
                      fontFamily: 'JetBrains Mono, Consolas',
                    }}
                    autoComplete='new-password'
                  />
                </Form.Field>
              ) : (
                <Form.Field>
                  <Form.Input
                    label={t('channel.edit.key')}
                    name='key'
                    required
                    placeholder={t('channel.edit.key_prompts.default')}
                    onChange={handleInputChange}
                    value={inputs.key}
                    autoComplete='new-password'
                  />
                </Form.Field>
              ))}
            {inputs.type === 37 && (
              <Form.Field>
                <Form.Input
                  label='Account ID'
                  name='user_id'
                  required
                  placeholder={
                    '请输入 Account ID，例如：d8d7c61dbc334c32d3ced580e4bf42b4'
                  }
                  onChange={handleConfigChange}
                  value={config.user_id}
                  autoComplete=''
                />
              </Form.Field>
            )}
            {inputs.type !== 33 && !isEdit && (
              <Form.Checkbox
                checked={batch}
                label={t('channel.edit.batch')}
                name='batch'
                onChange={() => setBatch(!batch)}
              />
            )}
            {inputs.type !== 3 &&
              inputs.type !== 33 &&
              inputs.type !== 50 &&
              inputs.type !== 53 && (
                <Form.Field>
                  <Form.Input
                      label={t('channel.edit.proxy_url')}
                    name='base_url'
                      placeholder={t('channel.edit.proxy_url_placeholder')}
                    onChange={handleInputChange}
                    value={inputs.base_url}
                    autoComplete='new-password'
                  />
                </Form.Field>
              )}
            <Button onClick={handleCancel}>
              {t('channel.edit.buttons.cancel')}
            </Button>
            <Button
              type={isEdit ? 'button' : 'submit'}
              positive
              onClick={submit}
            >
              {t('channel.edit.buttons.submit')}
            </Button>
          </Form>
        </Card.Content>
      </Card>
    </div>
  );
};

export default EditChannel;
