# TypeSafe Jev（SystemOne）接入指南

网关原生支持 TypeSafe AI 的 System One 决策模型（Jev）。Jev 不生成文本：请求携带一个 `state` 和若干带类型的 `questions`，返回带校准概率的结构化决策（`choice` / `score` / `noul`），始终非流式。

## 端点与协议

客户端请求与上游 TypeSafe 官方 wire 完全一致，因此官方 SDK 只需把 base URL 指向网关、API Key 换成网关令牌即可直连：

```http
POST /v1/systemone
Authorization: Bearer sk-<gateway-token>
Content-Type: application/json

{
  "model": "jev-latest",
  "state": "Help! My payouts have been failing for 3 days.",
  "questions": {
    "department": {
      "type": "choice",
      "instructions": "Which team should handle this?",
      "criteria": {"billing": "Payments, invoicing, refunds", "technical": "Bugs, outages", "sales": "Pricing, upgrades"}
    },
    "frustration": {"type": "score", "instructions": "How frustrated?", "criteria": ["Calm", "Frustrated", "Very angry"]},
    "is_urgent": {"type": "noul", "instructions": "Does this convey urgency?"}
  }
}
```

响应（含每个问题的结构化答案与用量）原样透传：

```json
{
  "model": "jev-1.13.0",
  "answers": {
    "department": {"type": "choice", "choice": "billing", "probabilities": {"billing": 0.88}, "confidence": 0.81},
    "frustration": {"type": "score", "score": 1.05, "legend": {"0": "Calm"}, "probabilities": {"1": 0.95}, "confidence": 0.92},
    "is_urgent": {"type": "noul", "noul": 0.95}
  },
  "usage": {"input_tokens": 307, "output_tokens": 20}
}
```

行为要点：

- `model`、`state`、至少一个 `questions` 条目为必填；令牌的模型白名单对该端点生效。
- 非流式；上游非 2xx 错误（401 / 422 / 429 / 529）原样转发给客户端。上游限速时建议客户端按指数退避重试。
- 上游限制：单请求上下文 64K tokens（state + 全部问题），80 req/s、100K tokens/s。网关不做额外校验，超限由上游 422/429 反馈。

## 渠道配置

1. 管理界面 → 渠道 → 新建，类型选择 **TypeSafe**（默认上游 `https://api.typesafe.ai`，可自定义 Base URL 覆盖）。
2. 密钥填 TypeSafe 控制台生成的 API Key。
3. 模型列表填 `jev-latest`、`jev-1.13.0`、`jev-preview` 中需要暴露的名字（自由输入）；分组决定哪些用户可用。
4. 可选模型映射：例如把对外名 `jev-latest` 映射到固定版本 `jev-1.13.0`（计费按映射后的名字取比率，建议对外名与上游名保持一致）。

TypeSafe 渠道只服务 `/v1/systemone`：不会参与 Chat/Responses/Embeddings 等任何其它协议的选路。

## 计费

- 计费走与 `/v1` 相同的 lifecycle（预扣 → 按实际用量结算 → 消费日志）。
- 默认模型比率：`0.021`（对应官方价格 **$0.042 / 1M 输入 tokens**，比率 1 = $0.002/1K）。
- 输出 tokens 官方免费，完成比率（completion ratio）为 `0`。
- 结算公式：`quota = ceil(input_tokens × 0.021 × 分组比率)`。
- 比率可在 运营设置 → 模型比例 / 完成比例 中按需覆盖（JSON，键为模型名）。

## 测试覆盖

- `relay/native/transport_test.go`：URL/认证/模型映射回写/state 必填/用量观测。
- `relay/native/error_test.go`：网关错误信封。
- `router/systemone_test.go`：集成——透传、按用量精确扣费、消费日志、上游 422 原样转发且退款、参数校验、无可用渠道。
