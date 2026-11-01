# Agent 运行时重构：自研内核与 OpenResponses

日期：2026-11-01。面向本项目实现者。状态：重构方案，尚未实现；不是上线能力声明。

用户要求重构现有 Chat 运行时，并重新考虑自研、OpenResponses 及 Hermes / Cherry Studio / ZCode 的实现。本方案建议采用 Go 自研运行内核，以 OpenResponses 作为模型交互的标准基础，不引入 Eino 或 Google ADK 为核心依赖。该建议替代 Studio 旧方案的 Eino 迁移路线；Chat / Work、内置与个人智能体广场、DeepL、图像及现有 API 保留要求继续有效。详细表结构与新增接口仍是拟议契约，实施前按本方案门槛验证。

## 1. 检查范围与结论依据

本轮检查本仓库 `chat/runtime.go`、`leg.go`、`context.go`、`events.go`、`tools.go`、`mcp.go`、`sandbox.go`，以及 `controller/chat.go`、`model/chat.go`、`model/main.go`、`router/api.go`、前端 `Chat/chatApi.ts`、lifecycle 的请求入口和 Studio 方案。未声称穷尽全部间接依赖。

观察到的限制：控制器以 HTTP 请求 context 驱动执行；`sync.Map` 仅提供进程内会话互斥；Leg 固定 `native.OpenAIChat`；结果主要累积文本、函数调用和 token；历史依赖 ChatMessage；八类 SSE 事件无持久游标。现有实现有工具循环、MCP、摘要和代码执行，不应描述为完全没有 Agent 能力，但它不足以承担云端持久、多任务运行。

外部参考通过公开源码及仓库文档静态阅读，没有安装、运行或全面审计这些项目；main 链接会变化，借用代码前必须固定提交并核对许可证。ZCode 指智谱 `zai-org/ZCode`，不是其他同名项目。

| 参考与已读入口 | 借鉴点 | 本项目适用边界 |
| --- | --- | --- |
| [Hermes turn facade](https://github.com/NousResearch/hermes-agent/blob/main/agent/turn_facade.py)、[conversation loop](https://github.com/NousResearch/hermes-agent/blob/main/agent/conversation_loop.py)、[runner](https://github.com/NousResearch/hermes-agent/blob/main/run_agent.py) | 循环外围独立处理租约、上下文、持久化、中断、预算；模型与工具步骤分别处理 | 不照搬 Python mixin 或个人助手的宿主环境假设；云端需按用户和运行隔离 |
| [Cherry 运行时类型](https://github.com/CherryHQ/cherry-studio/blob/main/src/main/ai/runtime/types.ts)、[Host 服务](https://github.com/CherryHQ/cherry-studio/blob/main/src/main/ai/agentSession/AgentSessionRuntimeService.ts)、[对应说明](https://github.com/CherryHQ/cherry-studio/blob/main/docs/references/ai/agent-session-runtime.md) | Host/Driver 分工、追加输入、恢复 token、模型调用级用量归属 | 主参考桌面仓库的 agent-session 路线；不把其他分支历史文档当当前契约；不迁入其 Electron/SDK 体系 |
| [ZCode CLI 依赖边界](https://github.com/zai-org/ZCode/blob/main/apps/zcode-cli/packages/cli/package.json)、[配置](https://github.com/zai-org/ZCode/blob/main/apps/zcode-cli/docs/usage/03-config.md)、[会话](https://github.com/zai-org/ZCode/blob/main/apps/zcode-cli/docs/usage/06-sessions.md)、[批量任务](https://github.com/zai-org/ZCode/blob/main/apps/zcode-cli/docs/usage/13-automation.md) | core/contracts/adapters/provider 分层；模型配置与执行策略分开；显式 session 恢复和目标；环境与轨迹独立保存 | 本轮没有逐行验证 core 内核；恢复会话不代表恢复文件系统，不依据产品介绍宣称其开源实现完整支持所有能力 |

## 2. OpenResponses 的边界

采用 [2026-04-24 规范](https://www.openresponses.org/specification) 作为首版协议基线。OpenResponses 定义模型请求/响应、Item、内容与流式生命周期，不提供任务调度器、工具执行器、工作区或持久存储。OpenResponses 与 OpenAI Responses 保持独立协议标识；不得因名称相近合并配置或默认等价。

模型内部标准结构优先对齐 OpenResponses 的 Item / Content / tool-call 语义，并保存规范版本；模型调用仍经 lifecycle 按渠道选择原生协议。Responses / Messages / Gemini / Bedrock 等不被强制先转 Chat Completions。协议不支持某项能力时明确拒绝或按用户选择降级；未知字段透传不等于能够参与推理或工具执行。

所有无法标准化的原生块保存有版本的 provider/protocol/channel/account 归属和不透明载荷引用，例如推理签名、加密推理内容、服务端工具结果。回送前核验原生身份与能力，禁止将绑定状态送到不兼容的回退渠道。新模型切换可重建可移植上下文，但必须记录不能携带的部分；不能静默丢弃。

模型 Response 与 Agent Run 是不同对象：一个 Run 可以产生多次模型 Response、工具步骤及产物，模型响应完成不代表任务完成。不要把所有模型响应拼成一条伪造的 OpenResponses 响应。

Studio 采用单独、有版本的 Run 事件信封：`run_id / event_id / sequence / type / schema_version / payload`，其中模型事件携带 `invocation_id` 和标准模型事件载荷。Run 的排队、租约、工作区等属于平台控制事件。未来若直接暴露 OpenResponses 流，非标准事件必须按规范使用 `a1o:` 前缀且不改变核心语义；Studio 控制事件不冒充标准事件。现有 `/v1/responses` 不因本方案改变含义。

## 3. 目标结构与职责

建议新增 `agent/` 包，按依赖单向分层；名称是建议，不要求先创建空目录。

| 层 | 必须拥有的职责 | 不应拥有的职责 |
| --- | --- | --- |
| Host / RunService | owner 校验、冻结智能体版本/配置、输入接纳、取消与查询、产品事件投影 | 供应商密钥、模型 wire 解析 |
| Scheduler / Worker | 数据库队列、公平性与并发额度、心跳租约、fencing token、恢复核对 | 对模型输出解释为成功、重复执行未知副作用 |
| Engine | 从持久状态推进模型/工具步骤；检查预算；在安全边界消费追加输入；判定等待与结束 | HTTP 连接寿命、直接操作前端、直接扣费 |
| ModelExecutor | 原生请求/事件到标准 Item 的映射、能力选择和原生块保留；通过 lifecycle 调用 | 另写供应商认证、重试与账本 |
| ToolExecutor | schema 校验、执行时授权、超时与取消、执行记录、结构化结果与产物引用 | 自动继承智能体作者凭据或宿主完整文件权限 |
| ContextBuilder | 指令层级、历史与摘要、附件和 Skill；保留不透明块的有效边界 | 删除原始历史、重复摘要扣费 |
| Workspace / Artifact 服务 | 按任务隔离目录和资源；按需代码环境；持久产物与引用计数 | 浏览智能体就创建沙箱；释放环境时连带删历史素材 |
| EventStore / Projections | 追加事件、提交检查点、消息/任务视图、重连游标 | 直接把供应商调试数据或凭据发给浏览器 |

Chat 和 Work 使用同一 Engine。Chat 默认不启用代码工具，断开即取消；Work 启动后不受观察连接断开影响。新运行模式及断开策略启动时冻结，不能执行途中暗中改为 Work。HTTP context 只控制请求与订阅，Work worker 使用受 Run 控制的 context。

智能体定义回答“以什么规则与能力执行”，运行记录回答“这一次做什么”。广场只含内置与我的，不增加销售。普通生成、翻译、图像仍可直接调用应用服务，不要求经 Agent 推理，也不自动申请代码环境。保留将这些服务包装为 Agent 工具的边界。

### 3.1 拟议服务入口

下列为登录态 Studio 内部接口，实施时新增到用户认证路由，不替代模型 API：`POST /api/studio/runs` 创建 Chat/Work 运行，`GET /api/studio/runs/:id` 查询状态与结果，`GET /api/studio/runs/:id/events?after=<sequence>` 观察与重放事件，`POST /api/studio/runs/:id/inputs` 提交追加输入，`POST /api/studio/runs/:id/cancel` 请求取消。全部按 owner 授权；创建和追加输入必须携带幂等键，同键不同请求返回冲突。取消是幂等状态请求，不承诺外部执行瞬时停止。创建成功仅表示已接纳，不表示执行成功。

Run 创建时冻结 mode、agent_version 或显式配置、来源会话/任务、输入和文件引用、模型能力需求、步数/时间/费用预算及断开策略；用户不能覆盖服务端 owner、渠道密钥或提高管理员预算上限。新事件 GET 断开只取消观察，旧 Chat POST 断开由兼容控制器显式转为取消请求；不得依靠观察者数量推断运行寿命。Work 与 Chat 的具体路由页面可以演进，后端资源权限不能依赖导航可见性。

## 4. 持久对象与运行状态

拟议对象：AgentDefinition / 不可变 AgentVersion、Run、RunInput、RunStep、ModelInvocation、ToolExecution、RunEvent、Checkpoint、Workspace、ArtifactRef。Run 关联原 ChatSession 或 WorkTask；沿用 StudioRun 表达时应作为同一运行记录实现，不能重复建两套 Run 事实源。WorkTask 是用户目标，Run 是一次执行，Invocation 是一次计费模型调用。已有消费日志仍是结算事实源，新对象只保存关联。

首版单个 Run 串行推进；不同 Run 可并行。RunInput 使用 owner/run 范围的幂等键与稳定顺序，客户端重试不重复创建消息或步骤。已有会话同一时间只允许一个活跃 writer，使用跨进程约束；后续输入入队，在模型或工具批次结束后的安全边界消费，不并发改写已发送上下文。

运行状态拟为 queued、running、waiting_input、waiting_approval、cancel_requested、completed、failed、cancelled、needs_reconciliation。waiting_approval 仅在产品审批入口接通后开放；首版不增加通用审批产品。外部结果不明进入 needs_reconciliation，不能报 completed 或盲目重放。完成条件由 Run 策略定义：模型无工具调用只是当前推理步骤结束；Work 还需核对用户要求的产物/验证条件，无法验证时明确给出未验证结果。

Worker 领取需事务比较状态与租约；每次领取产生递增 fencing token，写步骤结果、事件与检查点都核验它。失去租约的 worker 停止推进和申请新副作用。fencing 不能撤销已发出的外部请求，恢复时必须核对旧请求状态。

模型请求前保存 Invocation 与请求意图，调用后保存结束状态、响应与用量关联。崩溃发生在上游受理之后时，除非有可验证查询/幂等机制，不能假设未收费并重新调用。lifecycle 的预扣、结算及重试需先盘点现有事务边界，再建立与 Invocation 的幂等关联；不宣称新增 invocation_id 就能实现崩溃安全结算。

工具执行前保存规范化参数、权限检查结果与执行意图，工具描述声明只读/幂等/有副作用、超时和输出上限。声明可重试不是足够证据；只有执行器提供经验证的幂等键或可查询外部执行 ID 时才允许自动恢复。否则未知结果等待核对。有副作用工具默认顺序执行；取消先停止新步骤，再请求中止在途执行并记录实际结果。

持久事件先提交后发送，状态转移、最终 Item、检查点及事件在可行范围同事务提交；大文件先入临时存储并核对完整性再提交引用。delta 可分批持久，但未提交不提前发布。重连通过单调 sequence 重放并由客户端去重；游标过期返回明确的重新同步指示。保留独立事件保留期与快照版本，不把它混成模型的 sequence_number。

## 5. KEEP / MOVE / REPLACE / DELETE 范围

| 决策 | 现有载体与调用方 | 新位置及替换条件 |
| --- | --- | --- |
| REPLACE | `chat/runtime.go` Run/runLoop，`controller/chat.go` RunChatTurn，`chat/leg.go` 的固定协议腿 | 新 Engine/ModelExecutor 直接接通 `/api/chat/.../run`：文本流、工具、预算、取消、持久化、结算对齐保留行为测试覆盖与前端联调后，切换与删除旧私有循环在同一阶段完成 |
| REPLACE | 八类旧 SSE 事件与前端旧事件消费 | 不建兼容投影、不设运行时双引擎开关（项目无外部旧客户端，对外 `/v1` 契约不变）：前端 chatApi 同阶段迁移到版本化 Run 事件接口；会话/消息/文件/Skill/MCP 管理 CRUD 是数据接口，原样保留并留在裁剪后的运维端（`/admin`），面向用户的 Chat 界面随前端拆分迁入新 Studio 用户端 |
| MOVE + KEEP | `chat/context.go`、`tools.go`、MCPManager、Skill 过滤 | 抽取共享上下文/工具能力；保留摘要计费、工具并集限制、全局/个人可见性、虚拟名称和个人仅 HTTP 的约束 |
| KEEP / 适配 | `chat/sandbox*.go` 的 Python/Node、超时和限制 | 作为可选执行后端保留；不把宿主 UID 子进程宣传为强隔离。扩大公众代码开放前验证独立执行环境；未具备时不静默回退宿主执行 |
| KEEP + 增量扩展 | ChatSession、ChatMessage、ChatFile、Skill/MCP 表；`model/main.go` 注册 | 保留主键/owner/历史/附件/tool-call 关联；新增标准 Item 与运行关联，不伪造旧数据中的推理块或执行状态 |
| KEEP | relay lifecycle/native/bridge、渠道、订阅账号、余额、日志、全部 `/v1` 与 `/v1beta` 入口 | 复用并补能力约束与 Invocation 关联；不让运行内核变成第二个无计费网关；管理员原生代理不作为普通 Agent 的模型入口 |
| KEEP + 新增 | 既有上传与图像/翻译；Studio Chat/Work/智能体广场 | 新入口可用后迁移导航；素材引用共享时删除会话不得误删他处仍在使用的文件 |
| DELETE | 本轮无公共能力、数据或测试删除 | 仅在替换能力连接、迁移及回归通过，确认无调用者后删除旧私有实现 |

新增字段/表仅增量迁移。旧消息经可重复运行的导入或按需读取生成标准表示，并保存来源 ID/版本及去重约束；来源记录不删除。切换后单一引擎写入会话，不存在新旧引擎并行写入或回退旧引擎的路径；升级前产生的旧消息按需生成标准表示，不静默截断历史。应用回滚保留新增数据，禁止通过删表回滚。受支持数据库需在副本验证迁移、备份恢复与回退。

## 6. 实施顺序与门槛

1. **R0 固定契约与基线**：实现者固定仓库提交；补查实际调用方和 lifecycle 计费事务边界；保存现有测试基线。固定 OpenResponses 规范版本，列明支持 Item/事件/扩展；定义 provider 状态绑定、错误及幂等策略。此阶段不删旧循环、不迁移生产数据库。
2. **R1 模型与存储纵向闭环**：实现标准 Item、Invocation、事件存储和 ModelExecutor；分别打通 OpenResponses、OpenAI Responses、Messages、Gemini，并保留 Chat 路径。先验证一轮流式及工具续接的持久回读，再做 UI。不得用一轮收齐冒充实时流。生产状态绑定不清时不开放跨渠道自动回退。
3. **R2 新 Engine 接通现有 Chat（直接切换）**：挂接真实旧路由、MCP、Skill、上下文/附件/摘要、Python/Node；前端 chatApi 同阶段迁移到版本化 Run 事件接口，不建旧事件投影、不设运行时双引擎开关（项目无外部旧客户端，对外 `/v1` 契约不变）。开发期新旧包可并存于工作树，但每一次真实请求只走一个引擎，不双跑收费调用或工具；文本流、工具、预算、取消、持久化、结算对齐保留行为覆盖后，同一阶段删除旧 runLoop 与旧事件发射。
4. **R3 Work 持久执行**：接通任务 API 与最小任务列表/进度/产物界面、队列、跨实例租约、追加输入、离页继续、查询重连、显式取消、重启核对；验证多用户/任务隔离。首版已有这些真实入口才可称为 Agent 运行时，不能以新包或接口空壳宣称完成。任务界面落在新 Studio 用户端（`/`），Work 入口占位随 P1 拆分预留。
5. **R4 配置复用与平台工作台**：接入不可变 AgentVersion、内置与我的广场，完善工作区/素材与图像/翻译工具；广场与工作区界面落在新 Studio 用户端；专用翻译/图像界面可按 Studio 原方案独立并行，不因运行时重构无限延后。

首版不引入多 Agent 调度、自动学习长期记忆、定时任务、插件市场、可视化 Workflow 或智能体销售；这些参考项目的能力不自动成为 A1O 要求。单 Agent 的持久执行、上下文、工具、成果与恢复边界先完成。

## 7. 可观察验收与未决条件

- 保留现有 chat runtime/context/leg/MCP/integration 的行为覆盖（测试适配新边界，不删除保留行为的断言）；对新 Run 事件接口验证事件顺序、部分回复、工具配对、附件与摘要。模板符号按原文传递。
- 假上游集成覆盖 Responses 推理与函数/服务端工具 Item、Claude thinking/signature/tool_result、Gemini thoughtSignature/多模态、OpenResponses 独立协议身份；持久化后下一轮回送相关原生块。明确列出未支持类型，不用未知字段透传代替执行支持。
- 断流、超时、畸形/重复事件、取消、余额不足、DB 写入失败均不伪造成功；一次摘要/模型调用计费一次，费用归属到 owner/run/invocation，既有账目不改算。
- 两个 worker 竞争、租约过期、旧 worker 回写、重启及追加输入重复提交不产生双写。模型受理、工具外部生效、结算提交与事件提交各边界故障注入；未知副作用转待核对。
- Work 离页继续，断线重连能还原进度与最终产物；Chat 保持原断开策略。普通 Chat、非代码 Work、直接图像/翻译均不预创建沙箱；代码步骤按需申请，取消清理资源但保留产物。
- owner 越权、撤销工具/文件授权、伪造客户端凭据/预算/AgentVersion 均被服务端拒绝；旧会话与文件升级后仍可读。

R0 必须解决：现有预扣/结算持久事务与重试边界，模型原生状态绑定到何种渠道/账号；R1 前固定首批 Item/参数能力矩阵及事件保留/快照策略；R3 前固定数据库队列实现、租约期限/心跳与并发额度，以及代码执行隔离后端。不能仅凭 SDK/协议文档作无损兼容或崩溃自动恢复声明。真实渠道实测补充假上游验证，不能替代缺失实现。

本轮交付是静态调查及可执行重构计划；未新增依赖、执行数据库迁移或改变业务运行行为。文档已与 Studio 方案交叉核对，后续代码阶段仍须补齐上述验证。
