# Moon Bridge

Moon Bridge 是一个 OpenAI Responses 兼容转发层，对外提供 `/v1/responses`，对内调用 Anthropic Messages 兼容 Provider API。

## 配置

```bash
export MOONBRIDGE_PROVIDER_BASE_URL="https://provider.example.com"
export MOONBRIDGE_PROVIDER_API_KEY="your-provider-key"
export MOONBRIDGE_MODEL_MAP="gpt-test=claude-test"
export MOONBRIDGE_ADDR=":8080"
```

可选缓存配置：

```bash
export MOONBRIDGE_CACHE_MODE="automatic" # off / automatic / explicit / hybrid
export MOONBRIDGE_CACHE_TTL="5m"         # 5m / 1h
```

## 运行

```bash
go run ./cmd/moonbridge
```

## 调用

```bash
curl -sS http://localhost:8080/v1/responses \
  -H 'content-type: application/json' \
  -d '{"model":"gpt-test","input":"Hello"}'
```

## 测试

```bash
CGO_ENABLED=0 GOCACHE="$(pwd)/.cache/go-build" go test ./...
```

## E2E 测试

`.env.test` 放真实 Provider 配置，已被 `.gitignore` 忽略：

```bash
ANTHROPIC_MESSAGE_BASE_URL="https://provider.example.com"
ANTHROPIC_API_KEY="your-provider-key"
ANTHROPIC_MODEL_NAME="provider-model"
```

运行真实 Provider E2E：

```bash
CGO_ENABLED=0 GOCACHE="$(pwd)/.cache/go-build" go test -tags=e2e ./internal/e2e
```

缓存 E2E 会产生额外 token 成本，默认跳过；需要时在 `.env.test` 加：

```bash
MOONBRIDGE_E2E_CACHE=1
```
