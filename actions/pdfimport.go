package actions

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"strings"

	"github.com/dtylman/saatool/ai"
	"github.com/dtylman/saatool/config"
	"github.com/dtylman/saatool/translation"
	"github.com/ledongthuc/pdf"
	"github.com/urfave/cli/v3"
)

// PDFImportAction represents the action to import a PDF file
type PDFImportAction struct {
	project *translation.Project
}

// Name returns the name of the action
func (a *PDFImportAction) Name() string {
	return "pdf"
}

// Usage returns the usage string for the action
func (a *PDFImportAction) Usage() string {
	return "Import a PDF file"
}

// Flags returns the flags for the action
func (a *PDFImportAction) Flags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:     "input",
			Aliases:  []string{"i"},
			Usage:    "Input PDF file",
			Required: true,
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
			Name:    "ocr",
			Aliases: []string{"c"},
			Usage:   "Force OCR even if text is detected",
			Value:   false,
		},
		&cli.StringFlag{
			Name:    "ocr-langs",
			Aliases: []string{"l"},
			Usage:   "Languages for OCR, comma separated (e.g. eng,pol)",
			Value:   "eng",
		},
		&cli.Float64Flag{
			Name:    "font-delta",
			Aliases: []string{"fd"},
			Usage:   "Font size delta to consider a new text block",
			Value:   15.00,
		},
		&cli.Float64Flag{
			Name:    "y-delta",
			Aliases: []string{"yd"},
			Usage:   "Y position delta to consider a new text block, if -1, use 2x average font size",
			Value:   -1.0,
		},
		&cli.StringFlag{
			Name:     "author",
			Aliases:  []string{"a"},
			Usage:    "Author of the document",
			Value:    "",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "title",
			Aliases:  []string{"t"},
			Usage:    "Title of the document",
			Value:    "",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "synopsis",
			Aliases:  []string{"s"},
			Usage:    "Synopsis of the document",
			Value:    "",
			Required: false,
		},
		&cli.BoolFlag{
			Name:    "details",
			Aliases: []string{"d"},
			Usage:   "Get book details using the configured LLM provider",
			Value:   true,
		},
		&cli.IntFlag{
			Name:    "skip-pages",
			Aliases: []string{"p"},
			Usage:   "Number of pages to skip from the start",
			Value:   0,
		},
	}
}

func (a *PDFImportAction) Action(ctx context.Context, cmd *cli.Command) error {
	input := cmd.String("input")

	log.Printf("Importing PDF file: %s\n", input)

	var err error

	// check if file exists
	if _, err := os.Stat(input); os.IsNotExist(err) {
		return fmt.Errorf("input file does not exist: %s", input)
	}

	config.Options.OpenRouterAPIKey = cmd.String("key")

	needOCR := cmd.Bool("ocr")
	if !needOCR {

		needOCR, err = a.needsOCR(input)
		if err != nil {
			return fmt.Errorf("failed to determine if OCR is needed: %w", err)
		}
	}
	if needOCR {
		input, err = a.runOCR(input, ctx, cmd)
		if err != nil {
			return fmt.Errorf("failed to run OCR: %w", err)
		}
		fmt.Printf("OCR completed, new file: %s\n", input)
	}

	err = a.createProject(ctx, cmd)
	if err != nil {
		return fmt.Errorf("failed to create project: %w", err)
	}

	if a.project == nil {
		return fmt.Errorf("project is nil")
	}

	err = a.processPDFFile(ctx, input, cmd)
	if err != nil {
		return fmt.Errorf("failed to process PDF file: %w", err)
	}

	a.project.Normalize()

	fileName, err := a.project.Save()
	if err != nil {
		return fmt.Errorf("failed to save project: %w", err)
	}

	log.Printf("Project saved to %s", fileName)

	return nil
}

func (a *PDFImportAction) processPDFFile(ctx context.Context, input string, cmd *cli.Command) error {
	log.Printf("Processing PDF file: %s\n", input)
	file, reader, err := pdf.Open(input)
	if err != nil {
		return fmt.Errorf("failed to open PDF file: %w", err)
	}
	defer file.Close()

	skipPages := cmd.Int("skip-pages")
	pages := reader.NumPage()
	for i := skipPages; i <= pages; i++ {
		p := reader.Page(i)
		if p.V.IsNull() {
			continue
		}
		err := a.processPage(ctx, cmd, &p, i)
		if err != nil {
			return fmt.Errorf("failed to process page %d: %w", i, err)
		}
	}

	return nil
}

func (a *PDFImportAction) processPage(ctx context.Context, cmd *cli.Command, p *pdf.Page, pageNum int) error {
	log.Printf("Processing page %d", pageNum)

	fontDelta := cmd.Float64("font-delta")
	yDelta := cmd.Float64("y-delta")

	rt := newRawText(fontDelta, yDelta)

	if p.V.IsNull() {
		return nil
	}

	for _, text := range p.Content().Text {
		rt.addText(text)
	}

	req := ai.OCRRequest{
		Page:     pageNum,
		OCRTexts: make([]ai.OCRInputText, 0),
		Title:    a.project.Title,
		Author:   a.project.Author,
		Synopsis: a.project.Synopsis,
		Genre:    a.project.Genre,
	}

	// Process the raw text blocks
	for _, block := range rt.Blocks {
		req.OCRTexts = append(req.OCRTexts, ai.OCRInputText{
			Text:     block.Text,
			FontSize: block.FontSize,
		})
	}

	ocrCleaner, err := ai.NewOCRCleaner()
	if err != nil {
		return fmt.Errorf("failed to create OCR cleaner: %w", err)
	}
	result, err := ocrCleaner.CleanOCR(ctx, &req)
	if err != nil {
		return fmt.Errorf("failed to clean OCR text: %w", err)
	}

	log.Printf("got %d paragraphs from OCR cleaner", len(result.Body))

	for _, para := range result.Body {
		sourceParagraph := translation.Paragraph{
			Text: para.Text,
		}
		a.project.Source.Paragraphs = append(a.project.Source.Paragraphs, sourceParagraph)
	}

	if result.FootNotes != "" {
		sourceParagraph := translation.Paragraph{
			Text: "--------\n" + result.FootNotes,
		}
		a.project.Source.Paragraphs = append(a.project.Source.Paragraphs, sourceParagraph)
	}

	return nil
}

func (a *PDFImportAction) createProject(ctx context.Context, cmd *cli.Command) error {
	title := cmd.String("title")
	if strings.TrimSpace(title) == "" {
		return fmt.Errorf("title is required")
	}
	a.project = translation.NewProject(title)
	a.project.Title = title
	a.project.Author = cmd.String("author")
	a.project.Synopsis = cmd.String("synopsis")

	// set source and target languages
	from := cmd.String("from")
	to := cmd.String("to")

	a.project.Source.Language = from
	a.project.Target.Language = to

	if !cmd.Bool("details") {
		return nil
	}

	translator, err := ai.NewTranslator(a.project)
	if err != nil {
		return fmt.Errorf("failed to create translator: %w", err)
	}
	bookDetails, err := translator.GetBookDetails(ctx)
	if err != nil {
		return fmt.Errorf("failed to get book details: %w", err)
	}
	a.project.Title = bookDetails.Title
	a.project.Author = bookDetails.Author
	a.project.Synopsis = bookDetails.Synopsis
	a.project.Genre = bookDetails.Genre

	return nil
}

// needsOCR checks if the PDF file likely needs OCR by analyzing its content.
func (a *PDFImportAction) needsOCR(fileName string) (bool, error) {
	log.Printf("Checking if PDF %s needs OCR", fileName)
	file, reader, err := pdf.Open(fileName)
	if err != nil {
		return false, fmt.Errorf("failed to open PDF file: %w", err)
	}
	defer file.Close()

	// heuristic: if the PDF has very little extractable text compared to the number of pages, OCR is likely needed.
	texts, err := reader.GetStyledTexts()
	if err != nil {
		return true, nil // If text extraction fails, assume OCR is needed.
	}
	if len(texts) == 0 {
		return true, nil
	}
	// Count total words extracted
	wordCount := 0
	for _, t := range texts {
		wordCount += len(strings.Fields(t.S))
	}
	// If average words per page is very low, assume mostly images
	numPages := reader.NumPage()
	if numPages == 0 {
		return true, nil
	}
	avgWordsPerPage := float64(wordCount) / float64(numPages)
	log.Printf("PDF %s has %d pages and %d words (avg %.2f words/page)", fileName, numPages, wordCount, avgWordsPerPage)
	return avgWordsPerPage < 10, nil // threshold: less than 10 words per page
}

// runOCR runs an external OCR tool on the PDF file and returns the path to the new OCRed PDF.
func (a *PDFImportAction) runOCR(fileName string, ctx context.Context, cmd *cli.Command) (string, error) {
	log.Printf("Running OCR on PDF %s", fileName)

	// check if ocrmypdf is installed
	_, err := exec.LookPath("ocrmypdf")
	if err != nil {
		return "", fmt.Errorf("ocrmypdf not found in PATH, please install it to use OCR functionality")
	}

	langs := cmd.String("ocr-langs")
	if langs == "" {
		langs = "eng"
	}

	outputFile := strings.TrimSuffix(fileName, ".pdf") + "_ocr.pdf"
	args := []string{"-l", langs, "--rotate-pages", "--deskew", fileName, outputFile}

	if cmd.Bool("strip-to-ascii") {
		args = append(args, "--remove-background")
	}

	if cmd.Bool("ocr") {
		args = append(args, "--force-ocr")
	}

	log.Printf("Executing command: ocrmypdf %s", strings.Join(args, " "))
	ocrCmd := exec.CommandContext(ctx, "ocrmypdf", args...)
	ocrCmd.Stdout = os.Stdout
	ocrCmd.Stderr = os.Stderr
	ocrCmd.Stdin = os.Stdin
	err = ocrCmd.Run()
	if err != nil {
		return "", fmt.Errorf("failed to run ocrmypdf: %w", err)
	}
	return outputFile, nil

}

type RawTextBlock struct {
	Text     string
	FontSize float64
}

type RawText struct {
	Blocks        []RawTextBlock
	lastY         float64
	totalFonts    int
	totalFontSize float64
	fontDelta     float64
	yDelta        float64
}

func newRawText(fontDelta float64, yDelta float64) *RawText {
	rt := RawText{
		Blocks:    make([]RawTextBlock, 0),
		fontDelta: fontDelta,
		yDelta:    yDelta,
	}
	rt.pushBlock()
	return &rt

}

func (rt *RawText) getYDelta() float64 {
	if rt.yDelta == -1 {
		return rt.averageFontSize(nil) * 2
	}
	return rt.yDelta
}

func (rt *RawText) averageFontSize(t *pdf.Text) float64 {
	if rt.totalFonts == 0 {
		if t != nil {
			return math.Abs(t.FontSize)
		}
		return 0.0
	}
	return rt.totalFontSize / float64(rt.totalFonts)
}

func (rt *RawText) addText(t pdf.Text) {
	deltaY := math.Abs(t.Y - rt.lastY)
	fontSize := math.Abs(t.FontSize)
	fontDelta := math.Abs(fontSize - rt.averageFontSize(&t))
	if deltaY > rt.getYDelta() {
		// new block
		rt.pushBlock()
	} else if fontDelta > rt.fontDelta {
		// new block
		rt.pushBlock()
	}
	// append to last block
	lastBlock := &rt.Blocks[len(rt.Blocks)-1]
	lastBlock.Text += t.S
	rt.lastY = t.Y
	rt.totalFonts++
	rt.totalFontSize += fontSize
}

func (rt *RawText) pushBlock() {
	if len(rt.Blocks) > 0 {
		lastBlock := &rt.Blocks[len(rt.Blocks)-1]
		lastBlock.FontSize = rt.averageFontSize(nil)
	}
	rt.Blocks = append(rt.Blocks, RawTextBlock{})
	rt.totalFonts = 0
	rt.totalFontSize = 0

}
