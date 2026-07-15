package config

import (
	"log"
	"os"
	"path"

	"fyne.io/fyne/v2"
)

// cachedAppDir holds the resolved application storage directory so that it
// only needs to be resolved from the Fyne app once. This avoids repeatedly
// calling fyne.CurrentApp() from background goroutines (e.g. long-running
// translations) after the app has been paused or backgrounded by the OS,
// which would otherwise spam "Attempt to access current Fyne app when none
// is started" errors even though the directory is already known.
var cachedAppDir string

// AppDir returns the application's storage directory.
func AppDir() string {
	if cachedAppDir != "" {
		return cachedAppDir
	}

	if fyne.CurrentApp() != nil {
		dir := fyne.CurrentApp().Storage().RootURI().Path()
		if dir != "" {
			log.Printf("using fyne app storage dir: %v", dir)
			cachedAppDir = dir
			return dir
		}
	}
	dir := os.Getenv("FILESDIR")
	if dir != "" {
		log.Printf("using FILESDIR env var: %v", dir)
		cachedAppDir = dir
		return dir
	}

	dir, err := os.UserConfigDir()
	if err == nil && dir != "" {
		log.Printf("using user config dir: %v", dir)
		cachedAppDir = dir
		return dir
	}

	dir, err = os.UserHomeDir()
	if err == nil && dir != "" {
		log.Printf("using user home dir: %v", dir)
		cachedAppDir = dir
		return dir
	}

	log.Fatal("failed to determine app dir")
	return ""
}

// ConfigDir returns the configuration directory path, creating it if necessary.
func ConfigDir() string {
	appDir := AppDir()
	configDir := path.Join(appDir, "config")
	err := os.MkdirAll(configDir, 0755)
	if err != nil {
		log.Fatalf("failed to create config dir %s: %v", configDir, err)
	}
	return configDir
}

// ProjectsDir returns the projects directory path, creating it if necessary.
func ProjectsDir() string {
	appDir := AppDir()
	projectsDir := path.Join(appDir, "projects")
	err := os.MkdirAll(projectsDir, 0755)
	if err != nil {
		log.Fatalf("failed to create projects dir %s: %v", projectsDir, err)
	}
	return projectsDir
}
