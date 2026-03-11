package actions

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"strings"

	"github.com/dtylman/saatool/ai"
	"github.com/dtylman/saatool/config"
	"github.com/dtylman/saatool/translation"
	"github.com/taylorskalyo/goreader/epub"
	"github.com/urfave/cli/v3"
	"jaytaylor.com/html2text"
)

// EPubImportAction handles converting EPUB files to text and preparing them for translation
type EPubImportAction struct {
	rc      *epub.ReadCloser
	project *translation.Project
}

func (ec *EPubImportAction) Name() string {
	return "epub"
}

func (ec *EPubImportAction) Usage() string {
	return "Import an EPUB file and prepare it for translation"
}

func (ec *EPubImportAction) Flags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:     "input",
			Aliases:  []string{"i"},
			Usage:    "Input EPUB file",
			Required: true,
		},
		&cli.IntFlag{
			Name:    "max-words",
			Aliases: []string{"m"},
			Usage:   "Maximum words per paragraph before considering a split",
			Value:   200,
		},
		&cli.IntFlag{
			Name:    "max-words-tolerance",
			Aliases: []string{"t"},
			Usage:   "Maximum words per paragraph before forcing a split",
			Value:   300,
		},
		&cli.BoolFlag{
			Name:    "strip-to-ascii",
			Aliases: []string{"s"},
			Usage:   "Strip non-ASCII characters from the text",
			Value:   false,
		},
		&cli.StringFlag{
			Name:     "from",
			Aliases:  []string{"f"},
			Usage:    "Source language (e.g. english, french, german)",
			Value:    "",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "to",
			Aliases:  []string{"o"},
			Usage:    "Target language code (e.g. polish, spanish, italian)",
			Value:    "",
			Required: true,
		},
		&cli.BoolFlag{
			Name:    "details",
			Aliases: []string{"d"},
			Usage:   "Use AI to get detailed information about the book",
			Value:   true,
		},
	}
}

func (ec *EPubImportAction) Action(ctx context.Context, cmd *cli.Command) error {
	err := ec.import1(cmd)
	if err != nil {
		return fmt.Errorf("failed to import EPUB: %w", err)
	}

	if cmd.Bool("details") {
		config.Options.DeepSeekAPIKey = cmd.String("deepseek-api-key")
		if config.Options.DeepSeekAPIKey == "" {
			return fmt.Errorf("deepseek-api-key is required to get book details")
		}
		err = ec.getBookDetails(ctx, cmd)
		if err != nil {
			return fmt.Errorf("failed to get book details: %w", err)
		}
	}

	os.Setenv("FILESDIR", ".")
	fileName, err := ec.project.Save()
	if err != nil {
		return fmt.Errorf("failed to save project: %w", err)
	}

	log.Printf("Project saved to %s", fileName)
	return nil
}

func (ec *EPubImportAction) getBookDetails(ctx context.Context, cmd *cli.Command) error {
	log.Printf("Getting book details for project %s", ec.project.Name)
	translator, err := ai.NewTranslator(ec.project)
	if err != nil {
		return fmt.Errorf("failed to create translator: %w", err)
	}
	bookDetails, err := translator.GetBookDetails(ctx)
	if err != nil {
		return fmt.Errorf("failed to get book details: %w", err)
	}
	ec.project.Author = bookDetails.Author
	ec.project.Genre = bookDetails.Genre
	ec.project.Synopsis = bookDetails.Synopsis
	ec.project.Characters = bookDetails.MainCharacters

	log.Printf("Book details retrieved: %s by %s", bookDetails.Title, bookDetails.Author)
	return nil
}

func (ec *EPubImportAction) import1(cmd *cli.Command) error {
	fileName := cmd.String("input")
	if fileName == "" {
		return fmt.Errorf("input file is required")
	}
	log.Printf("Converting EPUB file: %v", fileName)

	from := cmd.String("from")
	to := cmd.String("to")
	if from == "" || to == "" {
		return fmt.Errorf("both source and target languages must be specified")
	}

	var err error
	ec.rc, err = epub.OpenReader(fileName)
	if err != nil {
		return fmt.Errorf("failed to open EPUB file %s: %w", fileName, err)
	}
	defer ec.rc.Close()

	if len(ec.rc.Rootfiles) == 0 {
		return fmt.Errorf("no root files found in EPUB")
	}

	_, name := path.Split(fileName)
	ec.project = translation.NewProject(name)
	book := ec.rc.Rootfiles[0]

	ec.project.Title = book.Title
	ec.project.Source.Language = from
	ec.project.Source.Paragraphs = make([]translation.Paragraph, 0)
	ec.project.Target.Language = to
	ec.project.Target.Paragraphs = make([]translation.Paragraph, 0)

	maxWords := cmd.Int("max-words")
	maxWordsTolerance := cmd.Int("max-words-tolerance")
	stripToAscii := cmd.Bool("strip-to-ascii")

	log.Printf("Importing book: %s, language: %s -> %s", ec.project.Title, from, to)
	log.Printf("Using max words: %d, max words tolerance: %d, strip to ascii: %v", maxWords, maxWordsTolerance, stripToAscii)
	for _, item := range book.Spine.Itemrefs {
		err = ec.processItem(item, maxWords, maxWordsTolerance, stripToAscii)
		if err != nil {
			return fmt.Errorf("error processing item %s: %w", item.ID, err)
		}
	}

	ec.project.Normalize()

	return nil
}

func (ec *EPubImportAction) processItem(item epub.Itemref, maxWords, maxWordsTolerance int, stripToAscii bool) error {
	reader, err := item.Open()
	if err != nil {
		return fmt.Errorf("failed to open item %s: %w", item.ID, err)
	}
	defer reader.Close()

	content, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("failed to read content of item %s: %w", item.ID, err)
	}

	text, err := html2text.FromString(string(content), html2text.Options{PrettyTables: true, OmitLinks: true, TextOnly: false})
	if err != nil {
		return fmt.Errorf("failed to convert HTML to text for item %s: %w", item.ID, err)
	}

	if strings.TrimSpace(text) == "" {
		return nil
	}

	text = removeEmptyLines(text)

	splitter := translation.NewParagraphSplitter(maxWords, maxWordsTolerance, stripToAscii)

	paragraphs := splitter.Split(text)
	isFirst := true // first non-empty paragraph in this spine item = chapter start
	for _, paragraph := range paragraphs {
		if strings.TrimSpace(paragraph) == "" {
			continue
		}
		p := translation.Paragraph{
			Text:           paragraph,
			IsChapterStart: isFirst,
		}
		isFirst = false
		ec.project.Source.Paragraphs = append(ec.project.Source.Paragraphs, p)
	}
	return nil
}

// ImportEPUBFile imports an EPUB file and returns the resulting Project.
// It uses default paragraph splitting settings (maxWords=200, maxWordsTolerance=300, stripToAscii=false).
// If directRead is true the book is imported for reading as-is: source paragraphs are copied
// to the target unit (same language) so the project appears fully "translated" in the library
// and no AI translation is needed.
func ImportEPUBFile(fileName, from, to string, directRead bool) (*translation.Project, error) {
	log.Printf("Converting EPUB file: %v", fileName)

	rc, err := epub.OpenReader(fileName)
	if err != nil {
		return nil, fmt.Errorf("failed to open EPUB file %s: %w", fileName, err)
	}
	defer rc.Close()

	if len(rc.Rootfiles) == 0 {
		return nil, fmt.Errorf("no root files found in EPUB")
	}

	_, name := path.Split(fileName)
	project := translation.NewProject(name)
	book := rc.Rootfiles[0]

	project.Title = book.Title
	project.Source.Language = from
	project.Source.Paragraphs = make([]translation.Paragraph, 0)
	project.Target.Language = to
	project.Target.Paragraphs = make([]translation.Paragraph, 0)

	log.Printf("Importing book: %s, language: %s -> %s", project.Title, from, to)

	action := &EPubImportAction{rc: rc, project: project}
	for _, item := range book.Spine.Itemrefs {
		err = action.processItem(item, 200, 300, false)
		if err != nil {
			return nil, fmt.Errorf("error processing item %s: %w", item.ID, err)
		}
	}

	project.Normalize()

	// Direct-read mode: mirror source into target so the book is immediately readable
	// and shows as completed in the library without any translation step.
	if directRead {
		project.Target.Language = from
		copied := make([]translation.Paragraph, len(project.Source.Paragraphs))
		copy(copied, project.Source.Paragraphs)
		project.Target.Paragraphs = copied
	}

	return project, nil
}

func removeEmptyLines(text string) string {
	lines := strings.Split(text, "\n")
	nonEmptyLines := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			nonEmptyLines = append(nonEmptyLines, line)
		}
	}
	return strings.Join(nonEmptyLines, "\n")
}
