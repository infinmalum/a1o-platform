# Agent 运行时 R0 契约基线

日期：2026-11-01。状态：R0 交付，作为 [Agent 运行时重构方案](agent-runtime-reconstruction.md) R1 起的实施契约。本文记录的是已核实的代码事实与固定下来的决策；后续阶段修改契约时必须更新本文。

基线提交：`cd0f66e`（父提交 `43d7581 feat: modernize native provider protocols and streaming bridges`）。全量 `go test ./...` 基线：全部包 ok，0 FAIL（完整输出保存于实施会话 `/tmp/r0-test-baseline.txt`）。

## 1. OpenResponses 规范固定

采用 [openresponses.org 2026-04-24 规范](https://www.openresponses.org/specification)（前一版本 2026-01-15）。以下类型与事件名取自规范页原文（2026-11-01 抓取），不是从 OpenAI 文档类推。

首批支持的 Item（type 判别值，均要求 `id`/`type`/`status` 顶层字段；status ∈ `in_progress` / `completed` / `incomplete`，扩展状态 `failed` 仅用于失败终态）：

| Item | 已核实字段 |
| --- | --- |
| `message` | `role`（user / assistant）、`content[]`；内容部件 `input_text` / `output_text`，均含 `text` |
| `function_call` | `name`、`call_id`、`arguments`（JSON 编码字符串） |
| `function_call_output` | `call_id`、`output` |
| `reasoning` | `content`（原始思考）、`encrypted_content`（不透明，规范要求按不可检视内容对待）、`summary[]`（部件 `summary_text`，含 `text`）；三者均可选 |

首批支持的流式事件名（verbatim）：`response.in_progress`、`response.completed`、`response.failed`、`response.incomplete`、`response.output_item.added`、`response.output_item.done`、`response.content_part.added`、`response.content_part.done`、`response.output_text.delta`、`response.output_text.done`、`response.function_call_arguments.delta`（done 形态按规范占位模式 `response.<content_type>.done`）。扩展事件/Item 必须带实现方前缀（规范示例 `acme:*`，本项目为 `a1o:*`）。

首批不支持、遇到即明确拒绝（不允许未知字段透传冒充支持）：服务端工具 Item（web_search / file_search / image_generation 等调用类 Item）、WebSocket 传输、消息内图像/二进制内容部件（规范正文提到用户内容可含图像与二进制，但页面未给出部件类型名；实现前先取 Reference 核实，不在 R1 猜测字段名）。

与现有代码的关系：`relay/native` 已有 `OpenResponses` 协议常量，当前仅作为 Vercel 网关透传线（候选渠道仅 Vercel，请求体准备与 OpenAI Responses 共用 `prepareOpenAIResponsesBody`）。这是既有透传事实，不是内部标准。内部标准 Item 是 a1o 自有 schema，对齐上表语义；不得因名称相近把内部标准与 `native.OpenResponses` 或 `native.OpenAIResponses` 合并配置。

## 2. 内部标准结构

模型腿的标准单元是 Item；模型事件载荷对齐上表事件语义。本节为 R1 实现的 Go 级契约。

```go
// agent/item.go
type Item struct {
    ID     string // resp 层生成或沿用上游 id
    Type   string // message | function_call | function_call_output | reasoning
    Status string // in_progress | completed | incomplete | failed

    Role    string        // message 专用：user | assistant
    Content []ContentPart // message 内容；reasoning 的原始思考记入 ReasoningText

    Name      string // function_call
    CallID    string // function_call / function_call_output
    Arguments string // function_call
    Output    string // function_call_output

    Summary          []SummaryPart // reasoning
    ReasoningText    string        // reasoning 可见原始思考
    EncryptedContent string        // reasoning 密封思考
    Signature        string        // a1o 扩展：供应商思考签名（如 Anthropic signature_delta）

    ProviderState []OpaqueBlock // a1o 扩展：带归属的不透明原生块
}

type OpaqueBlock struct {
    Kind             string // vendor 定义的载荷种类，如 anthropic.redacted_thinking / openai.encrypted_content / gemini.thought_signature
    Vendor           string // 供应商标识（渠道 kind 名）
    Protocol         string // 产生该块的线上协议
    ChannelID        int
    UpstreamAccountID int   // 订阅账号渠道记录租约账号；0 表示无
    Version          string // 载荷格式版本，首版 "1"
    Payload          string // 不透明载荷本体（内联；超大载荷后续引入引用）
}
```

Run 事件信封为 a1o 自有版本化结构（模型事件携带 `invocation_id` 与标准事件载荷）：

```go
type RunEvent struct {
    RunID         int64
    EventID       string // 事件自身唯一 id
    Sequence      int64  // run 内单调递增，重连重放游标
    Type          string // run 级：run.queued / run.started / run.completed / run.failed / run.cancelled / invocation.started / invocation.completed / invocation.failed / input.appended；模型事件：上表 response.* 名
    SchemaVersion string // 首版 "1"
    InvocationID  int64  // 模型事件必填
    Payload       JSON
}
```

`/v1/responses` 既有契约不变；Studio 不冒充标准 OpenResponses 端点。

## 3. lifecycle 计费事务边界盘点（已核实）

`lifecycle.Forward`（relay/lifecycle/forward.go）拥有渠道选择、重试、预扣与结算；调用方共四处：`controller/native.go:320`（/v1 原生入口）、`chat/leg.go:75`（内置聊天模型腿）、`controller/background.go:64`（后台 Responses 任务）、`relay/controller/provider.go`（经 `ForwardAdapter` 的 Replicate/Coze/图像供应商路径）。

- **预扣是每 attempt 一次**：进入上游 HTTP 调用前 `reserveAttempt` 创建 `billing.NativeReservation`，额度 = ceil((PreConsumedQuota + MaxOutputTokens) × modelRatio × groupRatio)。重试时旧 reservation 先 `Refund`，新 attempt 重新预扣；锁定具体渠道（SpecificChannel）不重试。
- **预扣落库点**：`billing.Reserve` 先 `model.PreConsumeTokenQuota`（持久扣减 token 额度），再 `model.CacheDecreaseUserQuota`（缓存）；第二步失败回退第一步。两者不是一个事务。
- **结算落库点**：响应完成后 `reservation.Settle(usage)`；上游失败/非 2xx/非事件流 `Refund`。结算用 `context.WithoutCancel`，不随客户端断连取消。`usage.Seen` 时按实际用量计费，否则保留预扣；流中断时 `max(实际, 预扣)`。
- **结算不是一个事务**：`Settle` 内部依次执行 quota delta 调整 → `RecordConsumeLog` → `UpdateUserUsedQuotaAndRequestCount` → `UpdateChannelUsedQuota`，四段独立写库。
- **崩溃窗口（R0 最重要的发现）**：reservation 状态只在内存（`finished` 标志 + 互斥锁），没有任何持久预留记录、TTL 或对账清理。进程在 `PreConsumeTokenQuota` 之后、结算/退款之前崩溃，预扣额度将永久滞留在 token 额度里。现有消费日志只在结算时写入，无法事后区分"已结算"与"预扣后崩溃"。
- **推论**：R1 的 ModelInvocation 记录请求意图与结束状态，使"预扣后崩溃"在事后可被识别（Invocation 停留在 pending/unknown 而消费日志缺失），但 R1 不实现自动对账或自动重放；此类运行进入 `needs_reconciliation`。不能宣称新增 invocation_id 就实现了崩溃安全结算（与重构方案 §4 一致）。

## 4. Provider 原生状态绑定策略（R0 决策，R1 实施时收紧）

- Item 上的不透明原生块（`ProviderState`）记录产生它的 vendor、协议、channel_id、upstream_account_id 与载荷版本。
- **回送规则**：仅当本次调用的目标渠道与绑定一致（有 upstream_account_id 时要求同账号，否则同渠道）才把不透明块放回请求；可见思考文本、函数调用等可移植内容照常携带。
- **执行机制（R1 收紧为全部锁定）**：ModelExecutor 每次调用都以 `lifecycle.SelectForRequest` 按模型家族偏好顺序预选渠道（Responses 家族 → Anthropic → Gemini → Chat），随后以 `Principal.SpecificChannel` 锁定该渠道发起调用，禁止 lifecycle 跨渠道重试——无论请求是否携带绑定块。这从机制上保证绑定状态不可能被送到不兼容的回退渠道，也保证为特定上游协议构建的请求体（如仅 Responses 透传携带的 `store`/`include`）不会被重试到不兼容的渠道。锁定渠道失败由 Run 层决定是否发起新的 Invocation（全新选择、全新重试预算），内核不隐式故障转移。R1 实际携带的不透明载荷走 Item 专属槽位：reasoning 的 `signature`/`encrypted_content`、function_call 的 `thought_signature`；`ProviderState` 通用列表是 R2+ 扩展点（服务端工具结果等）。
- **归属记录**：一次 Invocation 只经过一个渠道，因此调用级归属（ModelInvocation 的 channel_id/channel_type/upstream_account_id/upstream_protocol 列）与逐块归属等价；R1 用调用级记录。
- bridge 层的跨供应商响亮拒绝保持为最后防线，两处规则独立生效。

## 5. 错误与幂等策略（R0 决策）

- ModelInvocation 状态：`pending`（已记录意图）→ `succeeded` / `failed` / `cancelled` / `unknown`（崩溃恢复后判定）。`unknown` 或 pending 超时的调用不自动重发；上游已受理与否不可证时一律待核对。
- 内核不叠加模型层重试：单次模型调用的重试完全归属 lifecycle（渠道级）；Run 层只决定"是否发起新的 Invocation"，不在同一 Invocation 失败后隐式重放。
- 幂等键：Run 创建（owner 范围唯一键，同键不同请求返回冲突）与 RunInput（owner+run 范围唯一键）首版落库；工具执行幂等留待 R2/R3 的 ToolExecutor。
- 计费关联：`RecordConsumeLog` 增量改为返回日志 ID，`NativeReservation` 暴露本次结算的消费日志 ID，`lifecycle.Request` 增加可选 `OnSettlement` 回调；ModelInvocation 记录 `consume_log_id` 与用量快照。既有账目字段与算法不动。

## 6. R1 验收补充（在重构方案 §7 基础上）

- 假上游覆盖：Responses 推理 Item（summary/encrypted_content）与 function_call 续接、Claude thinking/signature 经桥往返、Gemini thoughtSignature 往返；持久化后下一轮请求重新携带上述块，且绑定块不落到未绑定渠道。
- 每次真实模型调用恰好一条消费日志，Invocation 与之关联；流中断结算保留预扣的行为不变。
- 消息内图像等首批不支持形状：明确拒绝并留痕，不做未知字段透传。

## 7. R1 实施记录（2026-11-01）

交付物：`agent/item.go`（标准 Item）、`agent/collector.go`（Responses SSE → Item）、`agent/executor.go`（ModelExecutor）、`model/agent_run.go`（AgentRun / ModelInvocation / RunEvent 及序号、幂等、事件辅助函数，已注册 AutoMigrate）、计费关联管线（`NativeReservation.ConsumeLogID` + `lifecycle.Request.OnSettlement`）。

- 四种假上游集成测试（`agent/executor_test.go`）覆盖：OpenAI Responses 透传（含 `store:false` + `include:["reasoning.encrypted_content"]` 与密封思考回读）、Vercel OpenResponses 透传、Anthropic thinking/signature 经桥往返、Gemini thoughtSignature 往返；每例两腿——第一腿产出生成物，第二腿断言持久化上下文连回不透明块一起重放，且每腿恰好一条消费日志并与 Invocation 关联。
- **修复遗留丢弃**：`relay/bridge/stream_responses_gemini.go` 的 Gemini→Responses 流式方向原样跳过 thought 部件与 thoughtSignature（非流式方向已携带）。现补齐：thought 文本与签名进 reasoning item，functionCall 签名挂 function_call 项，缺失 call_id 时与非流式方向一致地生成。
- **"保留 Chat 路径"的落地口径**：既有 `chat/leg.go`（inbound OpenAIChat）一行未动、测试全绿；Responses→Chat 桥由 bridge 包既有测试覆盖。agent 腿的 Chat 上游在现行路由契约下仅 Cloudflare kind 可达（其 Workers AI 专有格式经既有 `prepareCloudflareRun` 转换），OpenAI/Azure/OpenRouter kind 的 base 按现行契约视为原生提供 /v1/responses。
- 计费关联：`RecordConsumeLog` 的 GORM Create 回填 `log.Id`，`NativeReservation` 在结算后捕获之，`OnSettlement` 在 Forward 的三条结算路径与 `ForwardAdapter` 的两处、以及固定额度路径统一触发；退款路径无日志，回调收到 id 0。
