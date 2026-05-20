package modelgw

// NewOllamaProvider 等价于 OpenAIProvider,但 Type() 返 "ollama"。
// Ollama 0.4+ 在 /v1/* 提供 OpenAI 兼容端点;请求/响应字段一致。
//
// 与 OpenAI 的实际差异:
//   - 通常无 API key (api_key_env 为空)
//   - 模型名格式 "qwen2.5:7b" 含冒号 (Registry.Resolve 已正确处理)
//   - usage 字段语义略有差异: input_tokens/output_tokens 可能为 0 (取决于版本)
//
// 没有差异需要写新代码;直接复用 OpenAIProvider。
func NewOllamaProvider(cfg ProviderConfig) (*OpenAIProvider, error) {
	cfg.Type = "ollama"
	return NewOpenAIProvider(cfg)
}
