package actions

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/dtylman/saatool/translation"
	"github.com/urfave/cli/v3"
)

// TextExportAction exports only the translation (target) paragraphs to a plain text file.
type TextExportAction struct{}

func (t *TextExportAction) Name() string {
	return "text"
}

func (t *TextExportAction) Usage() string {
	return "Export only the translation text to a plain text file"
}

func (t *TextExportAction) Flags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:     "input",
			Aliases:  []string{"i"},
			Usage:    "Input project file (.spz)",
			Required: true,
		},
		&cli.StringFlag{
			Name:    "output",
			Aliases: []string{"o"},
			Usage:   "Output text file path (optional, defaults to input filename with .txt extension)",
		},
	}
}

func (t *TextExportAction) Action(ctx context.Context, cmd *cli.Command) error {
	inputPath := cmd.String("input")
	outputPath := cmd.String("output")

	project, err := translation.LoadProject(inputPath)
	if err != nil {
		return fmt.Errorf("failed to load project: %w", err)
	}

	if outputPath == "" {
		ext := filepath.Ext(inputPath)
		outputPath = strings.TrimSuffix(inputPath, ext) + ".txt"
	}

	log.Printf("Exporting translation to text: %s", outputPath)
	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create output file: %w", err)
	}
	defer f.Close()

	for i, para := range project.Target.Paragraphs {
		if i > 0 && para.IsChapterStart {
			_, _ = io.WriteString(f, "\n---\n\n")
		}
		_, _ = io.WriteString(f, para.Text)
		_, _ = io.WriteString(f, "\n\n")
	}
	log.Printf("Successfully exported to %s", outputPath)
	return nil
}
