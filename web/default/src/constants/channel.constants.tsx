// BASE_URL_INPUT_KINDS lists the channel kinds whose console form shows the
// generic Base URL input. The operator persona is a reseller who only pastes
// what a vendor handed them (base URL + key), so a field exists only where a
// vendor address is genuinely pasted: the OpenAI/Anthropic/Gemini wire kinds,
// Cloudflare's account-specific gateway URL, and TypeSafe when pointed at a
// non-official SystemOne endpoint. Kinds with a fixed official endpoint
// (OpenRouter), a preset choice (Coze's site, DeepL's account tier), or
// dedicated credential fields (Azure, AWS, VertexAI) get no free-form URL to
// misfill.
export const BASE_URL_INPUT_KINDS = new Set([1, 14, 24, 37, 52, 53]);

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
      '适用：OpenAI 官方 key（Base URL 留空即官方地址），或厂商文档标注「OpenAI 兼容 / Chat Completions」的地址（完整前缀填入 Base URL，含版本号，如智谱 https://open.bigmodel.cn/api/paas/v4）。覆盖 chat、embeddings、images、audio；/v1/responses 原样透传。OpenRouter 请用专属的「OpenRouter」渠道，无需填地址。注意：GPT 从 GPT-5.4 开始工具调用只兼容 Responses 线（GPT-6 系完全只认 Responses），GPT-6 模型请建「OpenAI Responses」渠道。',
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
  {
    key: 54,
    text: 'OpenRouter',
    value: 54,
    color: 'blue',
    description: '统一协议，无需 Base URL',
    tip:
      'OpenRouter 官方统一协议：一个地址（https://openrouter.ai/api/v1）同时服务 OpenAI Chat、OpenAI Responses、Anthropic Messages 和 SystemOne（Jev 决策）四种线，平台自动对接，只需填 OpenRouter 的 Key 并选择模型。「获取模型」按钮直接列出 OpenRouter 的全部模型，并把官方价格同步进价册（只补缺，不覆盖你改过的价）。',
  },
  { key: 33, text: 'AWS', value: 33, color: 'black' },
  {key: 3, text: 'Azure', value: 3, color: 'olive'},
  {key: 24, text: 'Gemini', value: 24, color: 'orange'},
  {
    key: 52,
    text: 'TypeSafe',
    value: 52,
    color: 'violet',
    description: 'Jev 决策模型',
    tip:
      'TypeSafe Jev 决策模型，通过 /v1/systemone 调用。Base URL 留空即官方 https://api.typesafe.ai；指向其他实现了 SystemOne 协议的服务时，填写该服务的完整接口前缀。',
  },
  {
    key: 34,
    text: 'Coze',
    value: 34,
    color: 'blue',
    description: '国内站 / 国际站',
    tip:
      'Coze 国内站（coze.cn）与国际站（coze.com）是两个独立平台，令牌互不通用：在国内站签发的令牌选「国内站」，国际站签发的选「国际站」，无需填写 Base URL。',
  },
  { key: 37, text: 'Cloudflare', value: 37, color: 'orange' },
  {
    key: 38,
    text: 'DeepL',
    value: 38,
    color: 'black',
    description: '免费版 / Pro 版',
    tip:
      'DeepL 翻译渠道：免费账号与 Pro 账号各有一个固定官方地址，按令牌所属账号选择即可，无需填写 Base URL。模型名填目标语言代码（如 EN-US、DE、ZH），「获取模型」按钮会列出全部可选语言。',
  },
  { key: 42, text: 'VertexAI', value: 42, color: 'blue' },
  { key: 46, text: 'Replicate', value: 46, color: 'blue' },
];
