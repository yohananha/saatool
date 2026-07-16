package main

import (
	"context"
	"fmt"

	"os"

	"github.com/dtylman/saatool/actions"
	"github.com/dtylman/saatool/config"
	"github.com/urfave/cli/v3"
)

func main() {
	cmd := &cli.Command{
		Name:     "saatool",
		Version:  config.Version,
		Usage:    "saatool - a tool for working with translation projects",
		Commands: []*cli.Command{},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "openrouter-api-key",
				Aliases: []string{"key"},
				Usage:   "API key for OpenRouter.ai",
			},
		},
		EnableShellCompletion: true,
	}
	actions.AddAction(cmd, "import", &actions.EPubImportAction{})
	actions.AddAction(cmd, "import", &actions.PDFImportAction{})
	actions.AddAction(cmd, "export", &actions.RTFExportAction{})
	err := cmd.Run(context.Background(), os.Args)
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}
