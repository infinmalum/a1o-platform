# 内置聊天运行时（Built-in Chat）

`/chat` 页面由 iframe 内嵌外部应用替换为原生轻量聊天运行时。它把聊天变成一个可配置、可调用工具的 AI 工作空间：服务端持有会话与消息，执行工具循环，并复用 One Gateway 既有的登录、余额、模型权限、计费与调用日志链路。

不做复杂 Workflow、多 Agent、长任务调度或持久 Agent Runtime；无向量检索。

## 功能一览

- **多会话聊天**：左侧会话列表（新建/重命名/删除），消息持久化在数据库，跨设备可见。
- **模型与参数**：模型从当前用户分组的可用模型中选择；可设置 temperature、top_p、max_tokens、系统提示词。
- **Skill**：提示词片段 + 可选工具绑定。管理员可建全局 Skill，用户可建个人 Skill；会话内自由选用。
- **MCP 工具**：管理员配置全局 MCP 服务器（stdio 命令 / Streamable HTTP）；用户可在“工具与 Skill”里添加自己的 **HTTP** MCP 服务器。所有启用的工具以 OpenAI function 形式暴露给模型。
- **基础 Tool Loop**：模型请求工具 → 网关执行（MCP 调用或本地沙箱）→ 结果回传模型继续，直到给出最终回答或达到轮数上限（`ChatMaxToolIterations`，默认 8）。
- **文件/知识上下文**：用户文件库 + 按消息挂载。文本文件（≤256KB）内容内联进该条消息；图片（PNG/JPEG/WebP/GIF）以 base64 data URL 作为 `image_url` 传给视觉模型。附件内容随历史持久并参与压缩。
- **代码 Sandbox**：`sandbox` 工具在本地子进程执行 Python/Node 代码（详见下文安全边界）。
- **长对话压缩**：按 tiktoken 估算上下文，超过预算（全局 `ChatContextBudgetTokens` 或会话级覆盖）时，把较早的历史经同模型压缩为摘要存入会话，前端收到 `compact` 事件。

## 计费与权限

- 首次使用聊天时，系统为该用户自动创建一个隐藏令牌 **「内置聊天」**（无限额度、不过期）。它只用于走通计费链路，实际扣费与额度校验发生在用户余额上。若用户在令牌页删除或停用它，下次使用会自动重建/恢复。
- 每一轮模型调用都通过 `lifecycle.Forward` 走标准路由：渠道选择、协议桥接（含订阅渠道的 Chat→Responses 转换）、预扣/结算、消费日志与 `/v1` 请求完全一致；日志中令牌名为「内置聊天」。
- 模型可用性由用户分组的渠道能力决定，与 `/api/user/available_models` 一致。

## HTTP API（均需登录态，`/api/chat/*`）

| 端点 | 说明 |
| --- | --- |
| `GET /api/chat/bootstrap` | 运行时信息：预算、沙箱可用性、上限 |
| `GET/POST /api/chat/sessions`、`GET/PUT/DELETE /api/chat/sessions/:id` | 会话 CRUD |
| `GET /api/chat/sessions/:id/messages` | 历史消息 |
| `POST /api/chat/sessions/:id/run` | 发起一轮对话，SSE 事件流 |
| `GET/POST /api/chat/files`、`DELETE /api/chat/files/:id` | 附件管理（multipart 字段名 `file`） |
| `GET/POST/PUT/DELETE /api/chat/skills[...]` | 个人 Skill；`/api/chat/skills/global` 为管理员全局 Skill |
| `GET/POST/PUT/DELETE /api/chat/mcp/user[...]` | 个人 HTTP MCP 服务器；`/api/chat/mcp` 为管理员全局（含 stdio） |

`run` 的 SSE 事件类型：`message_start`、`delta`（增量文本）、`tool_call`、`tool_result`、`usage`、`compact`、`error`、`done`。客户端断开即中止本轮，已生成内容保留。

## 配置项（管理员 → 设置 → 聊天）

| 选项 | 默认 | 说明 |
| --- | --- | --- |
| `ChatSandboxEnabled` | false | 代码沙箱总开关 |
| `ChatSandboxTimeoutSeconds` | 30 | 单次代码执行墙钟超时 |
| `ChatContextBudgetTokens` | 32000 | 默认上下文预算（0-200000 可按会话覆盖） |
| `ChatMaxToolIterations` | 8 | 单轮对话工具循环轮数上限 |
| `ChatMaxFileBytes` | 20 MiB | 附件上传大小上限 |

## Sandbox 安全边界

- 执行方式：`sh -c "ulimit ...; exec python3/node main"`，独立进程组；超时（默认 30s）整组 SIGKILL。
- 限制：CPU 时间=超时值、文件写入 ≤8MB、输出截断 64KB；Python 额外限制地址空间 512MB（Node 的 V8 保留大量虚拟内存，故不加）。
- **不隔离网络访问**，文件系统除临时目录外不受限。这是进程级“轻量”隔离，只适合可信用户环境；请在了解风险后再开启总开关，且每个会话还需单独打开沙箱开关。
- 本机缺少 `python3`/`node` 时该工具自动不可用（bootstrap 会如实上报）。

## MCP 服务器

- 全局：管理员配置，支持 `stdio`（命令 + 参数）与 `http`（URL + 请求头）。
- 个人：仅 `http` 传输；运行时强制校验，用户配置的 stdio 行会被忽略。
- 工具名映射为 `mcp_<服务器>_<工具>`（清洗为 `[a-zA-Z0-9_-]`，64 字符内去重）。Skill 的绑定工具列表即使用这些虚拟名。
- 连接按配置行的更新时间懒重连；单服务器连接失败只跳过该服务器，不影响本轮对话；工具调用超时 120s，错误作为工具结果回传给模型。

## Skill 语义

- Skill = 名称 + 描述 + 提示词内容 + 可选绑定工具列表。选中的 Skill 内容追加进 system 提示词。
- 当任一选中 Skill 声明了绑定工具时，本轮可见工具收缩为这些工具的并集（且仅限实际可用的）；未声明时保持全部可用工具。

## 已验证

- `go test ./...` 全量通过；`web/default` typecheck + build 通过。
- 真机冒烟（临时 SQLite + fake 上游 + root 登录）：SSE 文本流、Python 沙箱 `print(6*7)→42` 的工具往返、8 轮上限熔断、消费日志（令牌名「内置聊天」、渠道归属、流式标记）、附件注入上游请求、小预算触发的 `compact` 压缩与摘要持久化。
