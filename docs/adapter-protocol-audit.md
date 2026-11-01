# 适配器协议检查与修复

日期：2026-10-31，2026-11-01 续修。状态：各阶段结果分别记录；后续续修覆盖此前列出的部分限制，不能只引用早期表格作为最终状态。真实供应商联调未执行。适用范围：当前工作树注册的渠道与现有操作，不是新增全部供应商 API 功能的承诺。本文由本次检查维护，记录实施范围和验证证据；不覆盖 Studio 改造方案的产品决定。

## 判定口径

分别检查供应商接口、认证/URL、请求字段、响应/流事件和版本选择。SDK 版本、模型版本、API 版本不是同一概念；日期较旧的协议版本若官方仍要求使用，不擅自修改。用户补充决定：单独列出的厂商若有独立协议，则使用其独立协议；若没有独立协议、仅有 OpenAI Chat Completions / Responses 或 Anthropic Messages 兼容接口，则删除冗余的厂商专用入口，由用户配置对应通用协议。有专有协议却使用兼容接口的，必须改接专有协议，不能以现有实现兼容为由删除。仅品牌、域名、模型列表不同，不构成独立协议；URL、认证和签名差异要逐项核实，不能仅凭这些差异给兼容 JSON 格式另起协议名称。Cloudflare 的 Workers AI Run、AWS InvokeModel 等须按实际协议核查。用户明确指定保留 Vercel：其 OpenResponses 按独立协议规范核查，不能将它等同于 OpenAI Responses 后删除。此前将 Vercel 列入撤下专用入口范围的口头判断已撤回，尚未执行相应代码变更。

## 已检查的注册与调用边界

`relay/channeltype/define.go` 注册 OpenAI、Azure、Anthropic、Gemini、AWS Bedrock、Coze、Cloudflare、DeepL、Vertex AI、Replicate、TypeSafe、OpenAI Responses、OpenRouter、Vercel。检查同时覆盖 `relay/adaptor`、`relay/native`、`relay/bridge`、`relay/wire`、`relay/lifecycle` 和控制器注册，不能仅按适配器目录判断。

## 逐家结论与本轮实现

| 渠道 | 官方接口与判断 | 本轮处理 |
| --- | --- | --- |
| OpenAI / OpenAI Responses | Chat Completions、Responses 两个接口，不能强制互相替代 | 保留原生 JSON/SSE 与未知字段透传；未下载或升级 SDK |
| Anthropic | 官方最新版为 2026-10-21，移除 temperature/top_p/top_k 等输入 | 无显式版本且请求兼容时采用新版；显式版本优先；含旧版采样参数/不兼容历史块时保留 2023-06-01，不丢弃输入。模型本身是否接受这些参数仍由上游判定 |
| Gemini | 原生 GenerateContent；嵌入 batchEmbedContents | 保留原生生成协议；修复嵌入维度和多结果索引；非 float 编码明确拒绝，避免伪报已转换 |
| TypeSafe | 官方 OpenAPI 0.2.0：SystemOne；已支持 GET /v1/models | 保留 SystemOne；修复此前错误拒绝自动模型列表的实现 |
| Vercel | OpenResponses 独立规范；官方地址 /v1/responses | 保留专用渠道，增加 native.OpenResponses 协议标识；共同消息/事件转换复用已有代码，providerOptions 等原生扩展透传有测试覆盖 |
| OpenRouter | 查阅官方 API overview、文档索引与 OpenAPI：推理格式以 Chat/Responses/Messages 为基础，并有 provider、models、plugins、reasoning 等扩展。官方自称 OpenAI-compatible with additional OpenRouter features；没有据此虚构另一种完全不兼容的聊天格式 | 撤回未经调查的删除判断；本轮保留原生扩展透传与专用渠道，新增完整请求扩展保留回归。此处保留依据是已存在的扩展及多入口功能，不能声称“独立专有聊天 wire 已找到”。若要求把这类扩展也归入纯兼容厂商，仍须先迁移多协议选择与模型价格同步能力，当前未删除 |
| Cloudflare | 存在 Workers AI Run 独立协议，原实现却使用兼容接口 | 已改为 /ai/run/{model}；转换 text 嵌入、result/success/errors 包装、工具格式及流式输出；AI Gateway workers-ai 前缀也走原生模型路径 |
| AWS Bedrock | InvokeModel/InvokeModelWithResponseStream；另有 Messages 接口 | 保留原生 AWS SDK 传输；修复流请求体、ARN 与流关闭/截断错误；增加 config.api_version=messages 可选传输（SigV4 或 Bedrock bearer key），旧版本模型继续用 InvokeModel |
| Vertex AI | 项目/位置/发布商资源、rawPredict/GenerateContent 与服务账号鉴权 | 保留专用传输；修复 global 域名及错误要求额外 API key 的校验 |
| Azure | 日期版部署式路径与认证；另有 v1 | 增加空版本/v1 接口，保留日期版；部署名中的点原样保留，更新表单说明。保留专用类型以承载既有部署路径/认证配置 |
| Coze | /v3/chat 与命名 SSE 事件 | 改为增量消费/输出；读取 nested last_error；done 不能冒充 chat.completed；requires_action 明确报错；保留客户端同步聚合 |
| Replicate | /v1/models/{owner}/{name}/predictions，stream=true 后连接 urls.stream | 创建时明确请求 stream，不再等 succeeded 才连接；保留原始文本空格/引号；error/canceled 不输出成功；轮询响应取消；兼容同步客户端；修正图像纯 base64 输出、201/200 和 n>1 的明确拒绝 |

上述检查覆盖当前注册的 14 种渠道及现有操作，不表示覆盖所有供应商新增 API、所有模型专属 schema 或全部协议兼容测试套件。Replicate 现有图像输入仍是既有 FLUX 风格映射，不能宣传为支持其全部模型最新版参数；新增模型应单独验证 schema。OpenRouter 全部管理/Agent SDK API 不属于现有网关推理能力。

## 能力与数据边界

KEEP：渠道 ID、记录、密钥、价格、账务、客户端 API 和共享路由/结算。REPLACE：Cloudflare 兼容传输改原生 Run；Coze/Replicate 伪流式改增量处理。新增 OpenResponses 协议身份、Azure v1 与 AWS Messages 传输。DELETE：本轮没有执行厂商渠道删除；不得把此状态写成“所有冗余适配器均已删除”。本轮未连接或迁移工作目录内生产数据库。

DeepL 新请求从部署本改动起请求 show_billed_characters，优先使用实际值；上游未返回时按 Unicode code point 数估算，替代原字节数。旧额度/价格倍率沿用，不重算历史账单。多字节输入的新请求费用因此可能下降；旧 token 字段仍用于内部用量承载，不代表真实 LLM token。后续 Studio 翻译接口应采用明确字符字段。

## 配置与验证

- Azure：API 版本留空或 v1 选择 /openai/v1；显式日期版本继续部署式接口。
- AWS：默认继续 InvokeModel；config.api_version=messages 选择新 Messages 传输，模型名必须是该接口实际支持的 ID。配置当前日期版本也选择 Messages。不能把旧模型自动改成新模型。
- Anthropic：显式 anthropic-version 优先；配置 api_version 可固定日期。无显式配置按上述兼容规则选择。未升级 SDK；协议传输通过本项目 HTTP 层实现。
- 仅以假上游和本地请求验证，不读取生产密钥、不调用付费模型。Go 命令设置 GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off，禁止依赖下载。

修改前 relay/controller/router 基线测试通过。修复后结果见下方最终验证记录；需真实厂商账户进一步验证可用模型、套餐、地区和实际用量，不能把单元回归当作真实上游认证成功。

## 官方参考

- [OpenAI Chat](https://developers.openai.com/api/reference/chat-completions/overview)
- [Anthropic 版本与迁移](https://platform.claude.com/docs/en/api/migration-2026-10-21)
- [Gemini 嵌入](https://ai.google.dev/api/embeddings)
- [TypeSafe 官方 OpenAPI](https://api.typesafe.ai/openapi.json)
- [Azure v1](https://learn.microsoft.com/en-us/azure/foundry/openai/api-version-lifecycle)
- [AWS Messages](https://docs.aws.amazon.com/bedrock/latest/userguide/inference-messages-api.html)
- [Vertex 官方全局端点说明](https://cloud.google.com/blog/products/ai-machine-learning/global-endpoint-for-claude-models-generally-available-on-vertex-ai)
- [Cloudflare Run](https://developers.cloudflare.com/workers-ai/get-started/rest-api/)、[工具格式](https://developers.cloudflare.com/workers-ai/features/function-calling/traditional/)
- [Coze v3](https://www.coze.com/open/docs/developer_guides/chat_v3)（国际站页面依赖脚本；本轮官方搜索材料与已有 v3 实现交叉核对，未声称完整实测）
- [Replicate 流式事件](https://replicate.com/docs/topics/predictions/streaming)、[HTTP API](https://replicate.com/docs/reference/http)
- [DeepL 文本翻译](https://developers.deepl.com/api-reference/translate/request-translation)
- [OpenResponses 2026-04-24 规范](https://www.openresponses.org/specification)、[Vercel OpenResponses](https://vercel.com/docs/ai-gateway/sdks-and-apis/openresponses)
- [OpenRouter 协议概览](https://openrouter.ai/docs/api_reference/overview)、[官方 OpenAPI](https://openrouter.ai/openapi.json)、[文档目录](https://openrouter.ai/docs/llms.txt)

## 最终验证记录

2026-10-31 本地验证：`go test ./relay/... ./controller/... ./router/...` 成功；前端 `tsc --noEmit -p web/default/tsconfig.json` 成功；`git diff --check` 成功。新增契约测试覆盖 Cloudflare 原生请求/响应与截断、Vercel 扩展透传、OpenRouter 扩展保留、Anthropic 版本选择、Azure 路径、Bedrock ARN/签名请求、Coze 缺失结束事件、Replicate 失败终止和原始文本、DeepL 字符用量以及共享 SSE 生命周期。日志位于 `/tmp/a1o-adapter-final.log`（临时验证产物）。

上述通过不证明真实供应商认证、计费、全部模型参数或完整官方一致性套件通过。没有安装依赖、升级 SDK、运行数据库迁移、调用真实付费上游或提交 Git。工作区此前已有大量变更，本报告仅归属本轮列出的修复，不把原有改动算作本轮成果。


## Cloudflare AI Gateway 补充验证

Cloudflare 渠道可通过原生 Run 接入 AI Gateway：用户填写 Account ID，勾选“使用 AI Gateway”并填写 Gateway ID，服务端自动生成 https://gateway.ai.cloudflare.com/v1/{account}/{gateway}/workers-ai/{model}。Cloudflare 表单不再显示 Base URL 或代理地址输入，不要求用户拼接官方地址。取消勾选后清除网关配置并使用直连；存量显式 Base URL 仍由后端保留处理，切换模式时表单清除旧地址。可配置 cloudflare_gateway_token，独立发送 cf-aig-authorization: Bearer；渠道 key 仍发送 Authorization: Bearer。两种凭据不混用。前端已提供相应字段，渠道测试与运行链均传递配置。

本地新增回归验证两种路径及路由/认证头；relay/native、relay/lifecycle、controller、router 测试通过。未使用真实 Cloudflare 账号联调。参考 [Workers AI Gateway](https://developers.cloudflare.com/ai-gateway/usage/providers/workersai/) 与 [认证说明](https://developers.cloudflare.com/ai-gateway/configuration/authentication/)。

补充验证：自动地址生成（空 Base URL 和默认官方 Base URL 两种情况）已有回归；前端类型检查与 git diff --check 通过。

## 继续实施：原生请求覆盖核查

用户要求在交付账号联调前完成实现，不能把尚未实现的能力归为联调事项。本阶段先核查既有 `/v1/a1o/proxy/:channelid/*target` 原生代理实际能否覆盖上述限制。该入口既有权限为管理员且不计费，不能将它描述成普通用户计费 API 的替代。KEEP 现有权限和计费语义；REPLACE 统一认证为厂商实际认证/签名，补齐实时流转发；通过假上游验证请求体、查询参数、认证及流的原样保留。转换入口与原生入口分别验收，任何未覆盖项仍需列明。


## 2026-11-01 续修与可执行验收范围

本节补充并更新前述限制表，不代表所有厂商云产品的管理面 API 都需要转换成聊天格式。另一个 agent 新增的 provider_metadata 与协议桥接改动予以保留，本阶段不将它们算作自己的新增实现。

### 已补齐的实现

1. Bedrock 新增可选择的 Converse 原生传输。管理台 AWS 渠道选择“Converse（跨模型）”，对应 config.api_version=converse；经现有模型映射、身份和计费链调用。文本、工具请求/结果、采样参数映射至 Converse，同步结果与 AWS 二进制事件流转换为现有 Messages 下游协议。未知内容块明确拒绝，完整厂商块通过下述原生入口传递，不伪报无损转换。
2. Replicate Chat/Responses/Messages 请求可附加 replicate_input 对象，原样作为 Predictions.input；图像请求也增加 replicate_input。该对象按目标模型官方 schema 填写，不再强制套 FLUX 参数。普通图像接口的 prompt、n 和价格校验仍然适用；调用任意非图像模型应使用原生 Predictions 接口。预测文本响应兼容字符串或字符串数组。
3. 原生代理此前错误把裸 key 填入 Authorization；现按 Coze/Replicate/OpenRouter/Vercel/Cloudflare Bearer、Anthropic x-api-key、Gemini x-goog-api-key、DeepL Auth-Key、Azure api-key、Vertex 服务账号访问令牌、Bedrock SigV4 设置认证。保留上游请求体与查询参数（SigV4 可规范化查询顺序），替换调用者凭据；转发时逐块 Flush。Bedrock/Vertex 默认服务域名按已有区域配置生成。
4. Azure 前端提交原先仍将空版本覆写成旧 preview 日期，现改为 v1，与已实现后端语义一致。

### 原生接口使用与边界

现有入口为 `/v1/a1o/proxy/{channelId}/{上游完整路径}`，请求使用管理员所属的网关 token，密钥和签名由选定渠道提供。该入口保持既有“管理员专用、不计费”语义；不是向普通用户开放的额外免费通道。应用若要求普通用户直接调用全部厂商原生 API，需要单独实现对应资源归属与计费，不能通过放开此入口实现。

| 能力 | 原生入口的目标路径及输入 | 代码覆盖 |
| --- | --- | --- |
| Coze 工具结果提交与恢复 | `/v3/chat/submit_tool_outputs?conversation_id=...&chat_id=...`，正文 tool_outputs、stream | 正文、查询与命名 SSE 原样转发；requires_action 事件不会再被此入口的聊天转换器拒绝。普通聊天转换入口仍不承担端插件工具循环 |
| Replicate 任意模型 schema | `/v1/predictions` 或 `/v1/models/{owner}/{name}/predictions`，原生 input/version | 不执行固定字段映射，支持同一上游主机上的原生查询和取消；跨主机 urls.stream 由现有聊天适配器连接，管理员代理不会自动跟随另一主机的流地址；模型 schema 由厂商校验 |
| Bedrock 原生推理请求 | `/model/{modelId}/converse`、`converse-stream`、`invoke` 等原生路径，模型 ID 中的特殊字符按厂商 URL 规范编码 | 原生请求体、二进制响应与 SigV4 签名；跨模型通用聊天另可选择上述 Converse 转换 |
| Vertex 原生推理 | `/v1/projects/{project}/locations/{location}/publishers/...` | 路径和正文原样保留，服务账号认证；可承载非 GenerateContent 模型的原生请求，无须编造聊天格式 |
| Vercel / OpenRouter 扩展 | `/v1/responses`（Vercel）、`/api/v1/responses` 等（OpenRouter） | 路由、原生扩展、未知事件与响应保持原样；普通原生协议路径也保留已有扩展透传 |
| Cloudflare 原生模型/网关操作 | 官方 Run 或网关原生路径 | 参数和返回媒体不套文本转换；常规聊天/嵌入仍走已实现专用服务 |

### 本地验证与真实账号测试准备

新增测试覆盖：Converse 同步与 AWS event-stream 文本/用量、厂商认证及 SigV4 正文保留、通过实际管理员路由提交工具结果并接收命名事件、Replicate 新模型参数和两种文本输出形态。该验证证明传输链路实现可用；不将假上游响应描述成厂商实际业务调用成功。

后续真实账号联调应验证账号权限、地区/模型 ID、余额与厂商响应差异，不应再以“未知参数被本地白名单吞掉”“原生路由没接认证”作为预期问题。普通用户聊天转换、管理员原生接口的范围在本节明确区分；本轮没有声称普通用户能够使用七家全部控制面/推理接口。


2026-11-01 续修最终验证：`go test ./relay/... ./controller/... ./router/...`、前端 `tsc --noEmit -p web/default/tsconfig.json`、`git diff --check` 均通过。后端日志 `/tmp/a1o-resume-final.log`。上次额度耗尽导致最后一批命令未执行，本次已补执行代码修正及测试；保留其他 agent 的改动，未安装依赖、未运行生产数据库迁移、未提交 Git。
