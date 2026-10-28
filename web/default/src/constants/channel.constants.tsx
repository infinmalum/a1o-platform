export const CHANNEL_OPTIONS = [
  {
    key: 1,
    text: 'OpenAI Chat Completions',
    value: 1,
    color: 'green',
    description:
      'OpenAI 官方 Chat Completions 家族（chat、embeddings、images、audio、moderations），同一渠道可直接透传 /v1/responses',
  },
  {
    key: 50,
    text: 'OpenAI 兼容',
    value: 50,
    color: 'olive',
    description:
      'OpenAI 兼容渠道，Base URL 需填完整接口前缀（含版本号，如 https://api.deepseek.com/v1），创建后可用「获取模型列表」自动拉取模型',
  },
  {
    key: 53,
    text: 'OpenAI Responses',
    value: 53,
    color: 'green',
    description:
      'OpenAI Responses API（/v1/responses），适用于仅提供 Responses 端点的上游（如 Codex 系列）',
  },
  {key: 14, text: 'Anthropic', value: 14, color: 'black'},
  { key: 33, text: 'AWS', value: 33, color: 'black' },
  {key: 3, text: 'Azure', value: 3, color: 'olive'},
  {key: 24, text: 'Gemini', value: 24, color: 'orange'},
  { key: 27, text: 'MiniMax', value: 27, color: 'red' },
  {
    key: 52,
    text: 'TypeSafe',
    value: 52,
    color: 'violet',
    description: 'TypeSafe Jev 决策模型，通过 /v1/systemone 调用',
  },
  { key: 34, text: 'Coze', value: 34, color: 'blue' },
  { key: 37, text: 'Cloudflare', value: 37, color: 'orange' },
  { key: 38, text: 'DeepL', value: 38, color: 'black' },
  { key: 42, text: 'VertexAI', value: 42, color: 'blue' },
  { key: 46, text: 'Replicate', value: 46, color: 'blue' },
];
