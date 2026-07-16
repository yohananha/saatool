# SAATool Development Guide

This guide covers the development setup, building, and contribution guidelines for SAATool.

## Prerequisites

- Go 1.24 or later
- Fyne framework
- Android SDK (for building APK)
- Git

## Required Dependencies

### System Dependencies (Ubuntu/Debian)
```bash
sudo apt-get install -y gcc libgl1-mesa-dev xorg-dev libxkbcommon-dev
```

### Go Dependencies
```bash
go get fyne.io/fyne/v2@latest
go install fyne.io/tools/cmd/fyne@latest
```

### Android Development
```bash
# Install Android SDK
# Set environment variables:
export ANDROID_HOME=$HOME/Android/Sdk
export ANDROID_NDK_HOME=$HOME/Android/Sdk/ndk/29.0.13846066
export PATH=$PATH:$ANDROID_HOME/tools:$ANDROID_HOME/platform-tools
```

## Building the Project

### Desktop Application (for testing)
```bash
cd cmd/saatool
go build
./saatool
```

### Command Line Tool
```bash
cd cmd/saatooltool
go build
./saatooltool --help

# Import EPUB files
./saatooltool import epub -i book.epub -f english -o hebrew --openrouter-api-key "your_key"

# Import PDF files  
./saatooltool import pdf -i document.pdf -f english -o spanish -a "Author" -t "Title" --openrouter-api-key "your_key"
```

### Android APK
```bash
# Using the build script
./package.sh

# Or manually with fyne
cd cmd/saatool
fyne package --target android/arm64 --app-id org.saatool.app --icon icon.png --name "SAATool"
```

### CI/CD Build
The project includes GitHub Actions workflow that automatically builds APKs for both ARM64 and AMD64 architectures.

## Project Structure

```
saatool/
├── ai/                     # AI translation logic
│   ├── translator.go       # Main translation engine
│   ├── bookdetails.go      # Book metadata handling
│   ├── prompts.go          # AI prompts management
│   └── stats.go            # Translation statistics
├── cmd/
│   ├── saatool/            # Main Android app
│   │   ├── main.go         # App entry point
│   │   ├── icon.png        # App icon
│   │   └── FyneApp.toml    # App metadata
│   └── saatooltool/        # Command line converter
│       └── main.go         # CLI entry point
├── actions/                # CLI command implementations
│   ├── action.go           # Command pattern interface
│   ├── epubimport.go       # EPUB import functionality
│   └── pdfimport.go        # PDF import functionality
├── config/                 # Configuration management
│   ├── options.go          # App settings
│   ├── projects.go         # Project file handling
│   └── fs.go               # File system utilities
├── translation/            # Core translation logic
│   ├── project.go          # Project data structures
│   ├── direction.go        # Text direction handling
│   └── fyne.go             # UI integration
└── ui/                     # User interface
    ├── mainwindow.go       # Main application window
    ├── translationview.go  # Translation reading interface
    ├── projectsview.go     # Project management
    ├── settingsview.go     # Settings configuration
    └── widgets/            # Custom UI components
```

## Key Technologies

- **[Fyne](https://fyne.io/)**: Cross-platform UI framework for Go
- **[OpenRouter API](https://openrouter.ai)**: unified access to DeepSeek, Anthropic, OpenAI, and other AI translation providers
- **[GoReader](https://github.com/taylorskalyo/goreader)**: EPUB file processing
- **[html2text](https://github.com/jaytaylor/html2text)**: HTML to text conversion

## Development Guidelines

### Code Style
- Follow standard Go formatting (`go fmt`)
- Use meaningful variable and function names
- Add comments for exported functions and complex logic
- Maintain thread safety for concurrent operations

### Translation Logic
- All translation operations should be asynchronous
- Use proper error handling and user feedback
- Maintain translation state and progress tracking
- Implement proper cleanup for incomplete translations

### UI Guidelines
- Use Fyne's responsive design principles
- Implement proper navigation and user feedback
- Support both touch and keyboard interactions
- Maintain consistent theming and icons

### Testing
```bash
# Run tests
go test ./...

# Test specific package
go test ./translation
```

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests if applicable
5. Submit a pull request

## Build Configuration

The app metadata is managed in `cmd/saatool/FyneApp.toml`:
- Version and build numbers are automatically extracted during CI builds
- App ID and name are configured for proper Android packaging
- The build process creates APKs for both ARM64 and AMD64 architectures

## Architecture Overview

### Dual Application Structure
- **`cmd/saatool/`**: Main Android app (Fyne-based GUI) for reading and translating
- **`cmd/saatooltool/`**: CLI tool for EPUB/PDF → SPZ conversion using action pattern

### Core Data Flow
1. CLI converts EPUB/PDF → SPZ (compressed JSON project file)  
2. Android app imports SPZ → translates paragraphs → exports SPZ
3. Future: CLI converts translated SPZ → EPUB (planned)

### Key Components
- **`translation/project.go`**: Core data structures (Project, Unit, Paragraph, Character)
- **`ai/translator.go`**: DeepSeek API integration with async translation queue
- **`config/`**: Global options with JSON persistence (`Options` struct)
- **`ui/`**: Fyne widgets with custom theme and navigation patterns

For more detailed architectural information, see `.github/copilot-instructions.md`.