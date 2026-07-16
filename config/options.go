package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path"
)

// Provider identifies which LLM provider (via OpenRouter) is used for translation.
const (
	ProviderDeepSeek  = "deepseek"
	ProviderAnthropic = "anthropic"
	ProviderOpenAI    = "openai"
)

// ModelType selects between a provider's regular and reasoning model.
const (
	ModelTypeRegular   = "regular"
	ModelTypeReasoning = "reasoning"
)

// Options holds the application configuration options.
var Options struct {
	//OpenRouterAPIKey is the API key for the OpenRouter service, used to reach all LLM providers.
	OpenRouterAPIKey string `json:"openrouter_api_key"`
	//Provider is the selected LLM provider (see Provider* constants).
	Provider string `json:"provider"`
	//ModelType selects between the provider's regular and reasoning model (see ModelType* constants).
	ModelType string `json:"model_type"`
	//TranslateAhead is the number of paragraphs to translate ahead.
	TranslateAhead int `json:"translate_ahead"`
	//AppSize is the application size factor
	AppSize int `json:"app_size"`
	//TranslationDocSize is the number of paragraphs to include in the translation context.
	TranslationDocSize int `json:"translation_doc_size"`
	//AutoProofread proofreads translated paragraphs immediately after translation
	AutoProofread bool `json:"auto_proofread"`
}

func init() {
	// Set default options
	Options.OpenRouterAPIKey = ""
	Options.Provider = ProviderDeepSeek
	Options.ModelType = ModelTypeRegular
	Options.TranslateAhead = 6
	Options.AppSize = 16
	Options.TranslationDocSize = 3
	Options.AutoProofread = true
}

// LoadOptions loads options from the config file, if it exists. Otherwise, defaults are used.
func LoadOptions() error {
	configFile := path.Join(ConfigDir(), "options.json")
	log.Printf("loading options file: %s", configFile)

	data, err := os.ReadFile(configFile)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("options file does not exist, using defaults")
			return nil
		}
		return fmt.Errorf("failed to read options file: %v", err)
	}

	return json.Unmarshal(data, &Options)
}

// SaveOptions saves the current options to the config file.
func SaveOptions() error {
	log.Println("saving options")

	data, err := json.MarshalIndent(Options, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal default options: %v", err)
	}

	configFile := path.Join(ConfigDir(), "options.json")
	log.Printf("writing options file: %s", configFile)
	return os.WriteFile(configFile, data, 0644)
}
