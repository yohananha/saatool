package actions

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"github.com/dtylman/saatool/export"
	"github.com/dtylman/saatool/translation"
	"github.com/urfave/cli/v3"
)

// EPUBExportAction exports a translation project to EPUB format (translation + metadata).
type EPUBExportAction struct{}

func (e *EPUBExportAction) Name() string {
	return "epub"
}

func (e *EPUBExportAction) Usage() string {
	return "Export a translation project to EPUB format (translation text and metadata)"
}

func (e *EPUBExportAction) Flags() []cli.Flag {
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
			Usage:   "Output EPUB file path (optional, defaults to input filename with .epub extension)",
		},
	}
}

func (e *EPUBExportAction) Action(ctx context.Context, cmd *cli.Command) error {
	inputPath := cmd.String("input")
	outputPath := cmd.String("output")

	project, err := translation.LoadProject(inputPath)
	if err != nil {
		return fmt.Errorf("failed to load project: %w", err)
	}

	if outputPath == "" {
		ext := filepath.Ext(inputPath)
		outputPath = strings.TrimSuffix(inputPath, ext) + ".epub"
	}

	log.Printf("Exporting project '%s' to EPUB: %s", project.Name, outputPath)
	if err := export.ProjectToEPUB(project, outputPath); err != nil {
		return fmt.Errorf("export epub: %w", err)
	}
	log.Printf("Successfully exported to %s", outputPath)
	return nil
}
