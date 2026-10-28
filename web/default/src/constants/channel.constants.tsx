// BASE_URL_INPUT_KINDS lists the channel kinds whose adaptor builds the
// upstream request URL from the channel's configurable Base URL; the console
// shows the generic Base URL input for exactly these. Kinds left out either
// hardcode their endpoint (Replicate), use dedicated credential or deployment
// fields (Azure, AWS, VertexAI, TypeSafe), or are not offered in the console.
export const BASE_URL_INPUT_KINDS = new Set([
  1, 14, 24, 27, 34, 37, 38, 50, 53,
]);

// Filling rule, identical to the Anthropic kind: leave the Base URL empty to
// hit the official endpoint, or paste the vendor's documented address as-is.
// OpenAI-family vendors expose Completions and Responses on different URLs,
// so each URL gets its own channel: Completions-shaped on kind 1,
// Responses-shaped on kind 53. The dropdown `description` stays a short
// right-aligned hint (long sentences collide with the label); the full
// guidance lives in `tip`, rendered below the form field after selection.

export const CHANNEL_OPTIONS = [
  {
    key: 1,
    text: 'OpenAI Chat Completions',
    value: 1,
    color: 'green',
    description: '官方 key / Completions 形地址',
    tip:
      '适用：OpenAI 官方 key（Base URL 留空即官方地址），或厂商文档标注「OpenAI 兼容 / Chat Completions」的地址（完整前缀填入 Base URL，如智谱 https://open.bigmodel.cn/api/paas/v4）。覆盖 chat、embeddings、images、audio；/v1/responses 原样透传。注意：GPT 从 GPT-5.4 开始工具调用只兼容 Responses 线（GPT-6 系完全只认 Responses），GPT-6 模型请建「OpenAI Responses」渠道。',
  },
  {
    key: 53,
    text: 'OpenAI Responses',
    value: 53,
    color: 'green',
    description: '官方 key / Responses 形地址',
    tip:
      '适用：Responses 线地址，OpenAI 官方 key 同样适用——Base URL 留空即官方 Responses（与类型 1 透传的是同一条官方线）。厂商文档标注「Responses」的地址填完整前缀，如智谱 https://open.bigmodel.cn/api/v1。GPT 从 GPT-5.4 开始工具调用只兼容 Responses 线（GPT-6 系完全只认 Responses），GPT-6 模型的工具调用请建在本渠道。区别：53 不接 embeddings、images、audio；厂商的 Completions 与 Responses 是两条不同地址，分别建渠道（类型 1 与 53），计费额度也常常不同。',
  },
  {
    key: 14,
    text: 'Anthropic',
    value: 14,
    color: 'black',
    description: '官方 key / 兼容地址',
    tip:
      '适用：Anthropic 官方 key（Base URL 留空即官方地址 https://api.anthropic.com），或第三方 Anthropic 兼容地址（如智谱 https://open.bigmodel.cn/api/anthropic）。',
  },
  { key: 33, text: 'AWS', value: 33, color: 'black' },
  {key: 3, text: 'Azure', value: 3, color: 'olive'},
  {key: 24, text: 'Gemini', value: 24, color: 'orange'},
  { key: 27, text: 'MiniMax', value: 27, color: 'red' },
  {
    key: 52,
    text: 'TypeSafe',
    value: 52,
    color: 'violet',
    description: 'Jev 决策模型',
    tip: 'TypeSafe Jev 决策模型，通过 /v1/systemone 调用。',
  },
  { key: 34, text: 'Coze', value: 34, color: 'blue' },
  { key: 37, text: 'Cloudflare', value: 37, color: 'orange' },
  { key: 38, text: 'DeepL', value: 38, color: 'black' },
  { key: 42, text: 'VertexAI', value: 42, color: 'blue' },
  { key: 46, text: 'Replicate', value: 46, color: 'blue' },
];
