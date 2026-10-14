// 图像生成操场：用当前用户自己的令牌直接调用原生 /v1/images/generations
// 中继，便于在不离开管理界面的情况下验证渠道、模型与计费配置。

import React from 'react';
import {
  Button,
  Card,
  Dropdown,
  Form,
  Image,
  Message,
} from 'semantic-ui-react';
import { API, showError } from '../../helpers';
import {
  loadPlaygroundTokens,
  relayPost,
  type PlaygroundToken,
} from './relay';

const toOptions = (values: string[]) =>
  values.map((value) => ({ key: value, text: value, value }));

const SIZE_OPTIONS = toOptions([
  'auto',
  '1024x1024',
  '1536x1024',
  '1024x1536',
  '1792x1024',
  '1024x1792',
  '512x512',
  '256x256',
]);
const QUALITY_OPTIONS = toOptions([
  'auto',
  'standard',
  'hd',
  'low',
  'medium',
  'high',
]);
const STYLE_OPTIONS = toOptions(['vivid', 'natural']);
const FORMAT_OPTIONS = [
  { key: 'default', text: '默认（不传）', value: '' },
  { key: 'url', text: 'url', value: 'url' },
  { key: 'b64_json', text: 'b64_json', value: 'b64_json' },
];

interface GeneratedImage {
  url?: string;
  b64_json?: string;
  revised_prompt?: string;
}

interface ImageUsage {
  input_tokens?: number;
  output_tokens?: number;
  total_tokens?: number;
}

const ImagePlayground = () => {
  const [tokens, setTokens] = React.useState<PlaygroundToken[]>([]);
  const [tokenId, setTokenId] = React.useState<number | null>(null);
  const [models, setModels] = React.useState<string[]>([]);
  const [extraModels, setExtraModels] = React.useState<string[]>([]);
  const [model, setModel] = React.useState('');
  const [prompt, setPrompt] = React.useState('');
  const [count, setCount] = React.useState(1);
  const [size, setSize] = React.useState('');
  const [quality, setQuality] = React.useState('');
  const [style, setStyle] = React.useState('');
  const [responseFormat, setResponseFormat] = React.useState('');
  const [submitting, setSubmitting] = React.useState(false);
  const [images, setImages] = React.useState<GeneratedImage[]>([]);
  const [usage, setUsage] = React.useState<ImageUsage | null>(null);

  React.useEffect(() => {
    (async () => {
      try {
        const [tokenRows] = await Promise.all([
          loadPlaygroundTokens(),
          API.get('/api/user/available_models').then((res) => {
            const { success, data } = res.data;
            if (success) {
              const list: string[] = data || [];
              setModels(list);
              setModel(
                list.includes('dall-e-3') ? 'dall-e-3' : list[0] || ''
              );
            }
            return null;
          }),
        ]);
        setTokens(tokenRows);
        if (tokenRows.length > 0) {
          setTokenId(tokenRows[0].id);
        }
      } catch (error) {
        showError(error);
      }
    })();
  }, []);

  const submit = async () => {
    const token = tokens.find((item) => item.id === tokenId);
    if (!token) {
      showError('请先选择一个启用中的令牌');
      return;
    }
    if (!model.trim() || !prompt.trim()) {
      showError('请填写模型与提示词');
      return;
    }
    const body: Record<string, unknown> = {
      model: model.trim(),
      prompt: prompt.trim(),
      n: count,
    };
    if (size) {
      body.size = size;
    }
    if (quality) {
      body.quality = quality;
    }
    if (style) {
      body.style = style;
    }
    if (responseFormat) {
      body.response_format = responseFormat;
    }
    setSubmitting(true);
    setImages([]);
    setUsage(null);
    try {
      const payload = await relayPost(
        '/v1/images/generations',
        token.key,
        body
      );
      setImages(payload?.data || []);
      setUsage(payload?.usage || null);
    } catch (error) {
      showError(error);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div style={{ padding: '1rem' }}>
      <Form loading={submitting}>
        <Form.Group widths='equal'>
          <Form.Field required>
            <label>令牌</label>
            <Dropdown
              selection
              placeholder='选择启用中的令牌'
              value={tokenId ?? ''}
              onChange={(_, { value }) => setTokenId(Number(value))}
              options={tokens.map((token) => ({
                key: token.id,
                text: token.name,
                value: token.id,
              }))}
            />
          </Form.Field>
          <Form.Field required>
            <label>模型（可输入任意模型名）</label>
            <Dropdown
              search
              selection
              allowAdditions
              noResultsMessage={null}
              placeholder='dall-e-3'
              value={model}
              onAddItem={(_, { value }) => {
                const added = String(value).trim();
                if (added) {
                  setExtraModels((prev) =>
                    prev.includes(added) ? prev : [...prev, added]
                  );
                }
              }}
              onChange={(_, { value }) => setModel(String(value || ''))}
              options={toOptions(
                Array.from(new Set([...models, ...extraModels])).sort()
              )}
            />
          </Form.Field>
        </Form.Group>
        <Form.TextArea
          label='提示词'
          required
          placeholder='描述要生成的图像…'
          value={prompt}
          onChange={(_, { value }) => setPrompt(String(value || ''))}
        />
        <Form.Group widths='equal'>
          <Form.Field>
            <label>尺寸 size</label>
            <Dropdown
              selection
              clearable
              placeholder='默认（不传）'
              value={size}
              onChange={(_, { value }) => setSize(String(value || ''))}
              options={SIZE_OPTIONS}
            />
          </Form.Field>
          <Form.Field>
            <label>质量 quality</label>
            <Dropdown
              selection
              clearable
              placeholder='默认（不传）'
              value={quality}
              onChange={(_, { value }) => setQuality(String(value || ''))}
              options={QUALITY_OPTIONS}
            />
          </Form.Field>
          <Form.Field>
            <label>风格 style（dall-e-3）</label>
            <Dropdown
              selection
              clearable
              placeholder='默认（不传）'
              value={style}
              onChange={(_, { value }) => setStyle(String(value || ''))}
              options={STYLE_OPTIONS}
            />
          </Form.Field>
          <Form.Field>
            <label>响应格式 response_format</label>
            <Dropdown
              selection
              value={responseFormat}
              onChange={(_, { value }) =>
                setResponseFormat(String(value || ''))
              }
              options={FORMAT_OPTIONS}
            />
          </Form.Field>
          <Form.Input
            label='数量 n'
            type='number'
            min={1}
            max={10}
            value={count}
            onChange={(_, { value }) =>
              setCount(Math.min(10, Math.max(1, Number(value) || 1)))
            }
          />
        </Form.Group>
        <Button primary onClick={submit} disabled={submitting}>
          生成图像
        </Button>
      </Form>
      {usage && (
        <Message
          info
          style={{ marginTop: '1rem' }}
          content={`本次用量：输入 ${usage.input_tokens ?? '-'} / 输出 ${
            usage.output_tokens ?? '-'
          } / 总计 ${usage.total_tokens ?? '-'} tokens（实际扣费以日志页为准）`}
        />
      )}
      {images.length > 0 && (
        <Card.Group
          itemsPerRow={Math.min(3, images.length) as any}
          style={{ marginTop: '1rem' }}
        >
          {images.map((image, index) => (
            <Card key={index}>
              <Image
                src={image.url || `data:image/png;base64,${image.b64_json}`}
                wrapped
                ui={false}
              />
              {image.revised_prompt ? (
                <Card.Content>
                  <Card.Meta>{image.revised_prompt}</Card.Meta>
                </Card.Content>
              ) : null}
            </Card>
          ))}
        </Card.Group>
      )}
    </div>
  );
};

export default ImagePlayground;
