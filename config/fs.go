package config

import (
	"log"
	"os"
	"path"
	"path/filepath"

	"fyne.io/fyne/v2"
)

// AppDir returns the application's storage directory.
func AppDir() string {
	if fyne.CurrentApp() != nil {
		dir := fyne.CurrentApp().Storage().RootURI().Path()
		if dir != "" {
			log.Printf("using fyne app storage dir: %v", dir)
			return dir
		}
	}
	dir := os.Getenv("FILESDIR")
	if dir != "" {
		log.Printf("using FILESDIR env var: %v", dir)
		return dir
	}

	dir, err := os.UserConfigDir()
	if err == nil && dir != "" {
		log.Printf("using user config dir: %v", dir)
		return dir
	}

	dir, err = os.UserHomeDir()
	if err == nil && dir != "" {
		log.Printf("using user home dir: %v", dir)
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
// If Options.ProjectsDirectory is set, that path is used; otherwise AppDir()/projects.
func ProjectsDir() string {
	var projectsDir string
	if Options.ProjectsDirectory != "" {
		projectsDir = filepath.Clean(Options.ProjectsDirectory)
	} else {
		appDir := AppDir()
		projectsDir = path.Join(appDir, "projects")
	}
	err := os.MkdirAll(projectsDir, 0755)
	if err != nil {
		log.Fatalf("failed to create projects dir %s: %v", projectsDir, err)
	}
	return projectsDir
}
