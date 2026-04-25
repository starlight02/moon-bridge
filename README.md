# Moon Bridge

Moon Bridge 是一个 OpenAI Responses 兼容转发层，对外提供 `/v1/responses`，对内调用 Anthropic Messages 兼容 Provider API。

## 配置

复制示例配置，并填入真实 Provider 信息：

```bash
cp config.example.yml config.yml
```

`config.yml` 包含 Provider API Key，已被 `.gitignore` 忽略，不要提交。
`provider.models` 里建议保留 `moonbridge` 这个模型别名，Codex 验证脚本和 E2E 会优先使用它。

## 运行

```bash
go run ./cmd/moonbridge
```

指定配置文件或临时覆盖监听地址：

```bash
go run ./cmd/moonbridge --config ./config.yml --addr 127.0.0.1:8080
```

## 调用

```bash
curl -sS http://localhost:8080/v1/responses \
  -H 'content-type: application/json' \
  -d '{"model":"gpt-test","input":"Hello"}'
```

## 接入 Codex CLI

Moon Bridge 兼容 Codex CLI 使用的 OpenAI Responses 请求形态，包括 `/responses` 路径、`local_shell` 工具、函数工具、工具结果回传和常见 Codex 元数据字段。

示例 `~/.codex/config.toml`：

```toml
model = "moonbridge"
model_provider = "moonbridge"

[model_providers.moonbridge]
name = "Moon Bridge"
base_url = "http://localhost:8080/v1"
wire_api = "responses"
env_key = "MOONBRIDGE_CLIENT_API_KEY"

[model_providers.moonbridge.models.moonbridge]
name = "Moon Bridge"
```

本地转发层当前不校验客户端 API key，可随便给一个占位值：

```bash
export MOONBRIDGE_CLIENT_API_KEY="local-dev"
```

再启动 Moon Bridge：

```bash
go run ./cmd/moonbridge
```

## 测试

```bash
CGO_ENABLED=0 GOCACHE="$(pwd)/.cache/go-build" go test ./...
```

## E2E 测试

真实 Provider 配置读取 `config.yml`，该文件已被 `.gitignore` 忽略。测试会优先使用 `provider.models.e2e-model`，没有时使用 `provider.models.moonbridge`。

运行真实 Provider E2E：

```bash
CGO_ENABLED=0 GOCACHE="$(pwd)/.cache/go-build" go test -tags=e2e ./internal/e2e
```

缓存 E2E 会产生额外 token 成本，默认跳过；需要时设置：

```bash
MOONBRIDGE_E2E_CACHE=1 CGO_ENABLED=0 GOCACHE="$(pwd)/.cache/go-build" go test -tags=e2e ./internal/e2e
```

## Codex 端到端验证

脚本会读取 `config.yml`，自动启动 Moon Bridge，并用临时 `CODEX_HOME=./verify-codex-home` 运行 Codex，不修改全局 `~/.codex` 配置。`./verify-codex-home` 已被 `.gitignore` 忽略。

启动交互式 Codex TUI，在里面实际让 Codex 跑测试：

```bash
./scripts/verify-codex-tui.sh
```

也可以带一个初始任务进入 TUI：

```bash
./scripts/verify-codex-tui.sh '请运行 CGO_ENABLED=0 GOCACHE="$(pwd)/.cache/go-build" go test ./... 并汇报结果'
```

非交互 smoke test：

```bash
./scripts/verify-codex.sh
```

自定义提示词：

```bash
./scripts/verify-codex.sh "Reply exactly: moonbridge codex ok"
```

可选环境变量：

```bash
MOONBRIDGE_VERIFY_PORT=18081 ./scripts/verify-codex.sh
MOONBRIDGE_VERIFY_MODEL_ALIAS=moonbridge ./scripts/verify-codex.sh
MOONBRIDGE_CONFIG="$(pwd)/config.yml" ./scripts/verify-codex.sh
CODEX_HOME="$(pwd)/verify-codex-home" ./scripts/verify-codex.sh
```
