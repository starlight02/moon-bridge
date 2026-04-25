# DeepSeek V4 扩展

Moon Bridge 内置 DeepSeek V4 Provider 扩展，处理 DeepSeek 特有的推理内容（thinking/reasoning_content）行为，使得 Codex CLI 等多轮对话客户端可以正常使用 DeepSeek V4 模型。

## 为什么需要扩展

DeepSeek V4 (deepseek-v4-pro 等) 基于 Anthropic Messages 兼容接口暴露，但存在几个与标准 Anthropic 协议不同的行为：

- **reasoning_content 不能回传**：DeepSeek 在前一轮响应中返回 `reasoning_content`，但若下一轮请求的 input 中包含该字段，上游会返回 400 错误。
- **thinking 块不自动保留**：与原生 Anthropic 不同，DeepSeek 不会在后续轮次中自动保留前一轮的 `thinking` block。客户端需要自行记忆并重新注入。
- **temperature / top_p 被忽略**：DeepSeek 不支持这些参数，传了可能引发某些代理层 Warning，但无实际作用。
- **reasoning_effort → thinking 映射**：OpenAI 客户端使用 `reasoning.effort` 控制推理深度，DeepSeek 使用 `thinking` 配置 + `budget_tokens` 控制。

## 配置启用

在 `config.yml` 的 `provider` 段中设置：

```yaml
provider:
  deepseek_v4: true
```

开启后，Moon Bridge 自动在 Transform 模式下启用全部 DeepSeek 兼容逻辑。

## 功能详解

### 1. reasoning_content 剥离

每次将历史消息转为下一轮 Anthropic input 时，扩展会遍历所有消息内容，删除顶层的 `reasoning_content` 字段以及嵌套在 `content` 数组中的 `reasoning_content` 部分。

这样 DeepSeek 不会因为收到非法字段而返回 400。

### 2. Thinking 记忆与重注入

扩展维护一个进程内 `State`，在每轮模型返回后记录：

- **工具调用关联**：模型在调用工具前产生的 thinking 内容，按 `tool_call_id` 索引。下一轮如果同一 `tool_use` id 出现于历史 input，扩展会在对应的 `assistant` 消息前自动注入之前缓存的 thinking block。
- **纯文本关联**：模型在产生文本输出前产生的 thinking 内容，按文本 hash 索引。下一轮如果 assistant 消息包含同一段文本，扩展会在该消息前注入缓存的 thinking block。
- **容量控制**：记忆上限 1024 条，超出后按 FIFO 淘汰最旧记录。

### 3. reasoning_effort 映射

当 Codex 等 OpenAl 客户端在请求中传入 `reasoning`（如 `{"effort": "high"}`），扩展会：

| OpenAI effort | DeepSeek thinking level | budget_tokens |
|---------------|------------------------|---------------|
| low / medium  | high                    | max_tokens / 2 |
| high / max    | max                     | max_tokens * 3/4 |

同时移除 `temperature` 和 `top_p` 字段。

### 4. Reasoning 输出注入

DeepSeek 返回的 `thinking` 或 `reasoning_content` 块会被提取并转为 OpenAI 风格的输出消息（`role: "assistant"`），插入到真正的模型输出之前。Codex 客户端因此可以展示推理过程。

### 5. 流式处理

流式模式下，扩展通过 `StreamState` 逐事件收集 `thinking_delta` / `reasoning_content_delta` / `signature_delta`，在 thinking block 结束时将其汇入 `State` 供下一轮使用。若上游只返回 `signature_delta` 而没有 thinking 文本，扩展也会缓存并回放一个空文本的 `thinking` block，避免下一轮 thinking mode 请求因缺少 `content[].thinking` 被拒绝。

## 模块结构

```
internal/extensions/deepseek_v4/
├── deepseek_v4.go    # 核心转换函数：剥离、提取、注入、请求变异
├── deepseek_v4_test.go
├── state.go          # State / StreamState：记忆管理和流式状态跟踪
```

## 与 Bridge 的集成

扩展的触发点分布在 Bridge 层的多个位置：

| 位置 | 操作 |
|------|------|
| `bridge/request.go:convertInput()` | 剥离历史 reasoning_content |
| `bridge/request.go:convertInput()` | 为 tool_use / assistant text 注入缓存的 thinking block |
| `bridge/bridge.go:ToAnthropic()` | 调用 `ToAnthropicRequest` 变异请求 |
| `bridge/bridge.go:FromAnthropicWithPlanAndContext()` | 记录本轮 thinking 到 State |
| `bridge/stream.go:ConvertStreamEventsWithContext()` | 创建 StreamState；结束时汇入 State |
| `bridge/stream_events.go` | 流式事件中识别和收集 thinking delta |

## 注意事项

- 扩展仅在 `mode: Transform` 且 `provider.deepseek_v4: true` 时生效。
- `State` 是进程内内存存储，服务重启后丢失。首次请求或思维链变化后可能出现一轮缺少推理输出的情况，不影响功能正确性。
- thinking 的重注入依赖 tool_call_id 或文本 hash 匹配，如果模型在不同轮次对同一工具调用产生不同文本，可能匹配失败，同样不影响功能，仅影响推理显示完整性。
