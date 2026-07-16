package llm

import "github.com/dtylman/saatool/config"

// resolveModel maps a (provider, modelType) pair to an OpenRouter model ID
// and whether OpenRouter's unified reasoning parameter should be enabled.
func resolveModel(provider, modelType string) (model string, reasoning bool) {
	reasoning = modelType == config.ModelTypeReasoning
	switch provider {
	case config.ProviderOpenAI:
		if reasoning {
			return "openai/o3-mini", false
		}
		return "openai/gpt-4o", false
	case config.ProviderAnthropic:
		return "anthropic/claude-3.5-sonnet", reasoning
	case config.ProviderDeepSeek:
		fallthrough
	default:
		if reasoning {
			return "deepseek/deepseek-r1", false
		}
		return "deepseek/deepseek-chat", false
	}
}
