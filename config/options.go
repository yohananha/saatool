package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path"
)

// Options holds the application configuration options.
var Options struct {
	//DeepSeekAPIKey is the API key for DeepSeek service.
	DeepSeekAPIKey string `json:"deepseek_api_key"`
	//TranslateAhead is the number of paragraphs to translate ahead.
	TranslateAhead int `json:"translate_ahead"`
	//AppSize is the application size factor
	AppSize int `json:"app_size"`
	//TranslationDocSize is the number of paragraphs to include in the translation context.
	TranslationDocSize int `json:"translation_doc_size"`
	//AutoProofread proofreads translated paragraphs immediately after translation
	AutoProofread bool `json:"auto_proofread"`
	//SourceLanguage is the default source language for EPUB imports (e.g. "english")
	SourceLanguage string `json:"source_language"`
	//TargetLanguage is the default target language for EPUB imports (e.g. "hebrew")
	TargetLanguage string `json:"target_language"`
	//DarkMode enables the dark color theme
	DarkMode bool `json:"dark_mode"`
	//FixModel is the DeepSeek model used by the Fix button ("deepseek-chat" or "deepseek-reasoner")
	FixModel string `json:"fix_model"`
	//TranslateModel is the DeepSeek model used for ongoing and whole-book translation ("deepseek-chat" or "deepseek-reasoner")
	TranslateModel string `json:"translate_model"`
	//MaxConcurrentTranslations limits how many batch API calls run in parallel
	MaxConcurrentTranslations int `json:"max_concurrent_translations"`
	//TranslationBatchSize is the number of paragraphs sent per translation API call (1 = best perceived speed)
	TranslationBatchSize int `json:"translation_batch_size"`
	// ProjectsDirectory is the folder where translated books (.spz) are saved. Empty = use AppDir()/projects.
	ProjectsDirectory string `json:"projects_directory"`
}

func init() {
	// Set default options
	Options.DeepSeekAPIKey = ""
	Options.TranslateAhead = 16
	Options.AppSize = 16
	Options.TranslationDocSize = 3
	Options.AutoProofread = true
	Options.SourceLanguage = ""
	Options.TargetLanguage = ""
	Options.DarkMode = true
	Options.FixModel = "deepseek-chat"
	Options.TranslateModel = "deepseek-chat"
	Options.MaxConcurrentTranslations = 4
	Options.TranslationBatchSize = 1
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
	// 0600 = owner read/write only; protects the DeepSeek API key from other
	// users on shared systems (was 0644 = world-readable).
	return os.WriteFile(configFile, data, 0600)
}
