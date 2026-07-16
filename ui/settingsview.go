package ui

import (
	"fmt"
	"log"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
	"github.com/dtylman/saatool/config"
	"github.com/dtylman/saatool/ui/widgets"
)

type SettingsView struct {
	entryOpenRouterAPIKey *widget.Entry
	selectProvider        *widget.Select
	selectModelType       *widget.Select
	entryAppSize          *widget.Entry
	entryTransDocSize     *widget.Entry
	entryTranslateAhead   *widget.Entry
	entryAutoProofread    *widget.Check

	view fyne.CanvasObject
}

func NewSettingsView() *SettingsView {
	sv := &SettingsView{
		entryOpenRouterAPIKey: widget.NewEntry(),
		selectProvider: widget.NewSelect([]string{
			config.ProviderDeepSeek,
			config.ProviderAnthropic,
			config.ProviderOpenAI,
		}, nil),
		selectModelType: widget.NewSelect([]string{
			config.ModelTypeRegular,
			config.ModelTypeReasoning,
		}, nil),
		entryAppSize:        widget.NewEntry(),
		entryTranslateAhead: widget.NewEntry(),
		entryTransDocSize:   widget.NewEntry(),
		entryAutoProofread:  widget.NewCheck("", nil),
	}

	sv.view = widget.NewForm(
		widget.NewFormItem("OpenRouter API Key", sv.entryOpenRouterAPIKey),
		widget.NewFormItem("Provider", sv.selectProvider),
		widget.NewFormItem("Model Type", sv.selectModelType),
		widget.NewFormItem("App Sizes Factor", sv.entryAppSize),
		widget.NewFormItem("Translate Ahead", sv.entryTranslateAhead),
		widget.NewFormItem("Auto Proofread", sv.entryAutoProofread),
		widget.NewFormItem("Translation Doc Size", sv.entryTransDocSize),
	)

	Main.ClearActions()
	Main.AddAction("Save", widgets.IconSave, sv.onSaveTapped)

	sv.entryOpenRouterAPIKey.SetText(config.Options.OpenRouterAPIKey)
	sv.entryOpenRouterAPIKey.Password = true
	sv.selectProvider.SetSelected(config.Options.Provider)
	sv.selectModelType.SetSelected(config.Options.ModelType)
	sv.entryTranslateAhead.SetText(fmt.Sprintf("%v", config.Options.TranslateAhead))
	sv.entryAutoProofread.SetChecked(config.Options.AutoProofread)

	sv.entryAppSize.SetText(fmt.Sprintf("%v", config.Options.AppSize))
	sv.entryTransDocSize.SetText(fmt.Sprintf("%v", config.Options.TranslationDocSize))

	return sv

}

func (sv *SettingsView) View() fyne.CanvasObject {
	return sv.view
}

func (sv *SettingsView) Close() {
	// nothing to do
}

func (sv *SettingsView) Load() {
	// nothing to do
}

func (sv *SettingsView) onSaveTapped() {
	config.Options.OpenRouterAPIKey = sv.entryOpenRouterAPIKey.Text

	config.Options.Provider = sv.selectProvider.Selected
	if config.Options.Provider == "" {
		config.Options.Provider = config.ProviderDeepSeek
	}
	config.Options.ModelType = sv.selectModelType.Selected
	if config.Options.ModelType == "" {
		config.Options.ModelType = config.ModelTypeRegular
	}

	newSize, err := strconv.Atoi(sv.entryAppSize.Text)
	if err != nil {
		log.Printf("invalid app size: %v", err)
		sv.entryAppSize.SetText(fmt.Sprintf("%v", config.Options.AppSize))
	} else {
		config.Options.AppSize = newSize
	}

	newTranslateAhead, err := strconv.Atoi(sv.entryTranslateAhead.Text)
	if err != nil {
		log.Printf("invalid translate ahead: %v", err)
		sv.entryTranslateAhead.SetText(fmt.Sprintf("%v", config.Options.TranslateAhead))
	} else {
		config.Options.TranslateAhead = newTranslateAhead
	}

	config.Options.AutoProofread = sv.entryAutoProofread.Checked

	newDocSize, err := strconv.Atoi(sv.entryTransDocSize.Text)
	if err != nil {
		log.Printf("invalid translation document size: %v", err)
		sv.entryTransDocSize.SetText(fmt.Sprintf("%v", config.Options.TranslationDocSize))
	} else {
		config.Options.TranslationDocSize = newDocSize
	}
	err = config.SaveOptions()
	if err != nil {
		log.Printf("failed to save settings: %v", err)
		Main.ShowError(fmt.Sprintf("failed to save settings: %v", err))
	} else {
		Main.ShowMessage("Settings saved")
	}
}
