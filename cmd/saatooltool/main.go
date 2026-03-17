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
				Name:    "deepseek-api-key",
				Aliases: []string{"key"},
				Usage:   "API key for DeepSeek.ai",
			},
		},
		EnableShellCompletion: true,
	}
	actions.AddAction(cmd, "import", &actions.EPubImportAction{})
	actions.AddAction(cmd, "import", &actions.PDFImportAction{})
	actions.AddAction(cmd, "export", &actions.RTFExportAction{})
	actions.AddAction(cmd, "export", &actions.EPUBExportAction{})
	actions.AddAction(cmd, "export", &actions.TextExportAction{})
	err := cmd.Run(context.Background(), os.Args)
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}
