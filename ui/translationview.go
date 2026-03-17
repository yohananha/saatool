package ui

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/dtylman/saatool/ai"
	"github.com/dtylman/saatool/config"
	"github.com/dtylman/saatool/translation"
	"github.com/dtylman/saatool/ui/widgets"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// TranslationView represents the view for translating text in a project.
type TranslationView struct {
	view            fyne.CanvasObject
	project         *translation.Project
	translator      *ai.Translator
	txt             *widgets.BidiText
	panelMain       *fyne.Container
	overlay         *fyne.Container
	lblProgress     *widget.Label
	btnLanguage     *widget.Button
	projectSaver    *ProjectSaver
	sourceView      bool // true for source language, false for target language
	paragraphIndex  int  // current paragraph index
	// translationSem caps concurrent translation API calls (MaxConcurrentTranslations).
	translationSem chan struct{}
}

// NewTranslationView creates a new TranslationView for the given project.
func NewTranslationView(project *translation.Project) (*TranslationView, error) {
	translator, err := ai.NewTranslator(project)
	if err != nil {
		return nil, fmt.Errorf("failed to create translator: %v", err)
	}

	cap := config.Options.MaxConcurrentTranslations
	if cap < 1 {
		cap = 1
	}
	openIndex, openSourceView := project.EffectiveOpenPosition()
	tv := &TranslationView{
		project:        project,
		txt:            widgets.NewBidiText(),
		lblProgress:    widget.NewLabel(""),
		sourceView:     openSourceView,
		paragraphIndex: 0,
		translator:     translator,
		projectSaver:   NewProjectSaver(translator, project),
		translationSem: make(chan struct{}, cap),
	}

	tv.translator.OnTranslationComplete = tv.onTranslationCompleted

	// language toggle button (lives in overlay)
	lang := cases.Title(language.English).String(project.Source.Language)
	tv.btnLanguage = widget.NewButton(lang, tv.onLangChanged)

	// reading view: no always-visible toolbar buttons
	Main.ClearActions()

	// build the text size
	appSize := config.Options.AppSize
	tv.txt.TextSize = float32(appSize) * 2
	tv.txt.Padding = float32(appSize) / 2
	tv.txt.Spacing = float32(appSize) / 2

	tv.panelMain = container.NewStack(tv.txt)

	// build overlay (hidden by default)
	tv.overlay = tv.buildOverlay()
	tv.overlay.Hide()

	// outer layout: text fills top, overlay sits at the bottom
	outerLayout := container.NewBorder(nil, tv.overlay, nil, nil, tv.panelMain)
	panel := widgets.NewPanel(outerLayout, fyne.NewSize(0, 0))
	panel.OnTapped = tv.onMainPanelTapped
	tv.view = panel

	// Set the initial paragraph (defaults to last translated when saved position is 0)
	tv.SetParagraph(openIndex)

	tv.updateProgress()
	tv.updateText()

	return tv, nil
}

// buildOverlay creates the slim control bar that slides in on center tap.
func (tv *TranslationView) buildOverlay() *fyne.Container {
	btnFontDown := widget.NewButton("A−", func() { tv.adjustFontSize(-2) })
	btnFontUp := widget.NewButton("A+", func() { tv.adjustFontSize(+2) })
	btnFix := widget.NewButton("Fix", tv.onFixParagraph)

	tv.lblProgress.Alignment = fyne.TextAlignCenter

	row := container.NewHBox(
		btnFontDown,
		btnFontUp,
		widget.NewSeparator(),
		tv.btnLanguage,
		widget.NewSeparator(),
		btnFix,
		widget.NewSeparator(),
		tv.lblProgress,
	)

	return container.NewPadded(row)
}

// WindowContent implementation:
func (tv *TranslationView) View() fyne.CanvasObject {
	return tv.view
}

func (tv *TranslationView) Close() {
	tv.projectSaver.Stop()
}

func (tv *TranslationView) Load() {
	tv.projectSaver.Start()
	tv.invokeTranslation()
}

// onMainPanelTapped handles tap events on the main panel.
// Left 30% → previous, Right 30% → next, Center 40% → toggle overlay.
func (tv *TranslationView) onMainPanelTapped(pe *fyne.PointEvent) {
	width := tv.txt.Size().Width
	x := pe.Position.X

	switch {
	case x < width*0.30:
		tv.onPrevious()
	case x > width*0.70:
		tv.onNext()
	default:
		tv.toggleOverlay()
	}
}

// toggleOverlay shows the overlay if hidden, hides it if visible.
// When shown, it auto-hides after 4 seconds.
func (tv *TranslationView) toggleOverlay() {
	if tv.overlay.Visible() {
		tv.overlay.Hide()
	} else {
		tv.overlay.Show()
		time.AfterFunc(4*time.Second, func() {
			fyne.Do(tv.overlay.Hide)
		})
	}
}

// adjustFontSize changes the reading font size by delta points and refreshes the text layout.
func (tv *TranslationView) adjustFontSize(delta int) {
	newSize := config.Options.AppSize + delta
	if newSize < 8 {
		newSize = 8
	}
	config.Options.AppSize = newSize
	tv.txt.TextSize = float32(newSize) * 2
	tv.txt.Padding = float32(newSize) / 2
	tv.txt.Spacing = float32(newSize) / 2
	tv.txt.Refresh()
}

// onNext handles the action of moving to the next word or paragraph.
func (tv *TranslationView) onNext() {
	if tv.txt.Next() {
		tv.updateProgress()
		return
	}
	tv.SetParagraph(tv.paragraphIndex + 1)
}

// SetParagraph sets the current paragraph to display in the translation view.
func (tv *TranslationView) SetParagraph(paragraph int) {
	if (tv.paragraphIndex == paragraph) || (paragraph < 0) || (paragraph >= len(tv.project.Target.Paragraphs)) {
		return
	}

	tv.paragraphIndex = paragraph
	if !tv.sourceView {
		tv.invokeTranslation()
	}

	tv.project.SetPosition(tv.sourceView, tv.paragraphIndex)
	tv.updateText()
	tv.updateProgress()
}

// invokeTranslation initiates the translation for the current and subsequent paragraphs based on the configuration.
func (tv *TranslationView) invokeTranslation() {
	err := tv.translateParagraph(tv.paragraphIndex)
	if err != nil {
		log.Printf("translation error (paragraph %v): %v", tv.paragraphIndex, err)
	}
	// Pre-translate ahead paragraphs
	for i := 1; i <= config.Options.TranslateAhead; i++ {
		idx := tv.paragraphIndex + i
		if idx >= len(tv.project.Target.Paragraphs) {
			break
		}
		err := tv.translateParagraph(idx)
		if err != nil {
			log.Printf("pre-translation error (paragraph %v): %v", idx, err)
		}
	}
}

// translateParagraph translates the specified paragraph from source to target language.
func (tv *TranslationView) translateParagraph(paragraph int) error {

	if paragraph < 0 || paragraph >= len(tv.project.Target.Paragraphs) {
		return fmt.Errorf("paragraph %d out of range", paragraph)

	}

	sourceLang := tv.project.Source.Language
	targetLang := tv.project.Target.Language

	if sourceLang == "" || targetLang == "" {
		return errors.New("source or target language not set")
	}

	log.Printf("translating paragraph %d from %v to %v", paragraph, sourceLang, targetLang)

	target := tv.project.Target.Paragraphs[paragraph]

	if target.Text != "" {
		log.Printf("paragraph %d already translated", paragraph)
		return nil
	}

	if paragraph < 0 || paragraph >= len(tv.project.Source.Paragraphs) {
		log.Printf("source paragraph %d out of range", paragraph)
		return errors.New("source paragraph out of range")
	}

	go func() {
		tv.translationSem <- struct{}{}
		defer func() { <-tv.translationSem }()
		err := tv.translator.Translate(context.Background(), paragraph)
		if err != nil {
			log.Printf("translation error (paragraph %v): %v", paragraph, err)
		}
	}()

	return nil
}

// onTranslationCompleted is called when a translation is completed.
func (tv *TranslationView) onTranslationCompleted(paragraphIndex int, translation string) {
	tv.projectSaver.SetDirty(true)
	if tv.paragraphIndex == paragraphIndex {
		fyne.Do(func() {
			tv.updateText()
			tv.updateProgress()
		})
	}
}

// onProgressTapped handles the action of navigating to a specific paragraph.
func (tv *TranslationView) onProgressTapped() {
	selected := binding.NewString()
	selected.Set(fmt.Sprintf("%d", tv.paragraphIndex))
	dialog.NewForm("Go to Paragraph", "Go", "Cancel",
		[]*widget.FormItem{
			widget.NewFormItem("Go to:", widget.NewEntryWithData(selected)),
		},
		func(ok bool) {
			if ok {
				data, err := selected.Get()
				if err != nil {
					dialog.ShowError(fmt.Errorf("invalid paragraph number"), Main.window)
					return
				}
				selectedParagraph, err := strconv.Atoi(data)
				if err != nil {
					dialog.ShowError(fmt.Errorf("invalid paragraph number: %v", err), Main.window)
					return
				}
				if selectedParagraph < 0 || selectedParagraph >= len(tv.project.Target.Paragraphs) {
					dialog.ShowError(fmt.Errorf("paragraph number out of range"), Main.window)
					return
				}
				tv.SetParagraph(selectedParagraph)
			}
		},
		Main.window,
	).Show()
}

// onPrevious handles the action of moving to the previous word or paragraph.
func (tv *TranslationView) onPrevious() {
	if tv.txt.Previous() {
		tv.updateProgress()
		return
	}
	tv.SetParagraph(tv.paragraphIndex - 1)
}

// updateProgress updates the progress label with the current paragraph and word offset.
func (tv *TranslationView) updateProgress() {
	progressText := fmt.Sprintf("%v / %v", tv.paragraphIndex+1, len(tv.project.Target.Paragraphs))
	tv.lblProgress.SetText(progressText)
	tv.project.LastSourceView = tv.sourceView
	tv.project.LastParagraphIndex = tv.paragraphIndex
}

// updateText updates the text displayed in the translation view based on the current paragraph and language.
func (tv *TranslationView) updateText() {
	var p translation.Paragraph
	var lang string
	if tv.sourceView {
		lang = tv.project.Source.Language
		p = tv.project.Source.Paragraphs[tv.paragraphIndex]
	} else {
		lang = tv.project.Target.Language
		p = tv.project.Target.Paragraphs[tv.paragraphIndex]
	}

	if p.Text == "" {
		tv.panelMain.RemoveAll()
		text := "No text available for this paragraph."

		label := widget.NewLabel(text)
		label.Alignment = fyne.TextAlignCenter
		label.Wrapping = fyne.TextWrapBreak

		tv.panelMain.Add(label)

	} else {
		tv.panelMain.RemoveAll()
		tv.panelMain.Add(tv.txt)
		words := strings.Fields(strings.Replace(p.Text, "\n", " <NL> ", -1))
		dir := translation.GetTextDirection(lang)
		tv.txt.SetWords(words)
		if dir == translation.RightToLeft {
			tv.txt.Direction = widgets.RightToLeft
		} else {
			tv.txt.Direction = widgets.LeftToRight
		}
	}

	tv.updateProgress()
}

// onLangChanged handles the change of the source/target language toggle.
func (tv *TranslationView) onLangChanged() {
	tv.sourceView = !tv.sourceView
	if tv.sourceView {
		lang := cases.Title(language.English).String(tv.project.Source.Language)
		tv.btnLanguage.SetText(lang)
	} else {
		lang := cases.Title(language.English).String(tv.project.Target.Language)
		tv.btnLanguage.SetText(lang)
	}
	tv.updateText()
}

// onFixParagraph handles the action of fixing the current paragraph by re-translating it.
func (tv *TranslationView) onFixParagraph() {
	go tv.translator.FixTranslation(context.Background(), tv.paragraphIndex)
}
