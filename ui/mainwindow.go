package ui

import (
	"errors"
	"fmt"
	"log"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"
	"github.com/dtylman/saatool/ai"
	"github.com/dtylman/saatool/config"
	"github.com/dtylman/saatool/translation"
	"github.com/dtylman/saatool/ui/widgets"
)

// Main is the global instance of the main application window.
var Main *MainWindow

// WindowContent is an interface for views that can be shown in the main area
type WindowContent interface {
	View() fyne.CanvasObject
	Close()
	Load()
}

// MainWindow represents the main application window.
type MainWindow struct {
	fyneApp    fyne.App
	window     fyne.Window
	content    WindowContent
	toolBar    *fyne.Container
	header     *widget.Label
	translator *ai.Translator
}

func (mw *MainWindow) OpenProjectSaveDialog(callback func(fyne.URIWriteCloser, error), project *translation.Project) {
	fg := dialog.NewFileSave(callback, mw.window)
	if project != nil {
		fileName := project.Name
		if fileName == "" {
			fileName = "untitled"
		}
		if !strings.HasSuffix(fileName, config.ProjectFileExt) {
			fileName += config.ProjectFileExt
		}

		fg.SetFileName(fileName)
	}
	fg.SetFilter(storage.NewExtensionFileFilter([]string{config.ProjectFileExt}))
	fg.Show()
}

// OpenFileDialog opens a file dialog to select a file and calls the callback with the selected file.
func (mw *MainWindow) OpenProjectLoadDialog(callback func(reader fyne.URIReadCloser, err error)) {
	fd := dialog.NewFileOpen(callback, mw.window)
	fd.SetFilter(storage.NewExtensionFileFilter([]string{config.ProjectFileExt}))
	fd.Show()
}

// NewMainWindow creates a new instance of the main application window.
func NewMainWindow() error {
	if Main != nil {
		return errors.New("main window already exists")
	}

	Main = &MainWindow{
		window:  nil,
		toolBar: container.NewGridWrap(fyne.NewSize(100, 50)),
		header:  widget.NewLabel(fmt.Sprintf("SaaTool %v", config.Version)),
	}

	return nil
}

// ShowAndRun creates the main application window and starts the Fyne event loop.
func (mw *MainWindow) ShowAndRun() {
	defer log.Println("exiting application")
	mw.fyneApp = app.New()

	err := config.LoadOptions()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}
	mw.fyneApp.Settings().SetTheme(widgets.NewTheme(config.Options.AppSize))

	mw.window = mw.fyneApp.NewWindow("SaaTool")

	mw.window.Resize(fyne.NewSize(800, 600))
	mw.window.SetMaster()

	mw.onProjectsTapped()

	mw.window.ShowAndRun()

}

// SetContent sets the content of the main window.
func (mw *MainWindow) SetContent(content WindowContent) {
	if mw.content != nil {
		mw.content.Close()
	}
	mw.content = content
	mw.Refresh()
}

func (mw *MainWindow) Refresh() {
	if mw.content == nil {
		return
	}
	fyne.Do(func() {
		panelTop := container.NewHBox(
			widget.NewIcon(widgets.IconLogo),
			mw.header,
		)

		mainToolBar := container.NewGridWrap(fyne.NewSize(100, 50))
		mainToolBar.Add(widget.NewButtonWithIcon("Projects", widgets.IconProject, mw.onProjectsTapped))
		mainToolBar.Add(widget.NewButtonWithIcon("Settings", widgets.IconSettings, mw.onSettingsTapped))
		mainToolBar.Add(widget.NewButtonWithIcon("Log", widgets.IconLog, mw.onLogTapped))

		panelBottom := container.NewVBox(
			mw.toolBar,
			mainToolBar,
		)

		mw.window.SetContent(
			container.NewBorder(
				panelTop,
				panelBottom,
				nil,
				nil,
				container.NewVScroll(mw.content.View()),
			),
		)

		mw.content.Load()
	})
}

func (mw *MainWindow) ClearActions() {
	mw.toolBar.RemoveAll()
	mw.toolBar.Refresh()
}

func (mw *MainWindow) AddActionWidget(widget fyne.CanvasObject) {
	mw.toolBar.Add(widget)
	mw.toolBar.Refresh()
}

func (mw *MainWindow) AddAction(label string, icon fyne.Resource, action func()) *widget.Button {
	btn := widget.NewButtonWithIcon(label, icon, action)
	mw.toolBar.Add(btn)
	return btn
}

func (mw *MainWindow) onSettingsTapped() {
	mw.SetContent(NewSettingsView())
}

func (mw *MainWindow) onProjectsTapped() {
	mw.SetContent(NewProjectsView())
}

func (mw *MainWindow) onLogTapped() {
	mw.SetContent(NewLogView())
}

func (mw *MainWindow) ShowMessage(message string) {
	fyne.Do(dialog.NewInformation("Message", message, mw.window).Show)
}

func (mw *MainWindow) ShowError(message string) {
	fyne.Do(dialog.NewError(errors.New(message), mw.window).Show)
}
