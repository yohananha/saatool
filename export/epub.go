package export

import (
	"fmt"
	"html"
	"io"
	"log"
	"strings"

	"github.com/dtylman/saatool/translation"
	"github.com/go-shiori/go-epub"
	iso6391 "github.com/emvi/iso-639-1"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// langToCode returns an ISO 639-1 code for the given language name (e.g. "Hebrew" -> "he").
func langToCode(langName string) string {
	langName = cases.Title(language.English).String(strings.TrimSpace(langName))
	if langName == "" {
		return "en"
	}
	code := iso6391.CodeForName(langName)
	if code == "" {
		code = iso6391.CodeForNativeName(langName)
	}
	if code == "" {
		return "en"
	}
	return code
}

// ProjectToEPUB writes the project's translation and metadata to an EPUB file.
// It uses only Target paragraphs (translation-only content) and project metadata.
// Chapter breaks follow IsChapterStart on the target paragraphs (aligned with source).
func ProjectToEPUB(p *translation.Project, destPath string) error {
	p.Lock()
	title := p.Title
	if title == "" {
		title = p.Name
	}
	author := p.Author
	synopsis := p.Synopsis
	targetLang := p.Target.Language
	paragraphs := make([]translation.Paragraph, len(p.Target.Paragraphs))
	copy(paragraphs, p.Target.Paragraphs)
	p.Unlock()

	e, err := epub.NewEpub(title)
	if err != nil {
		return fmt.Errorf("create epub: %w", err)
	}

	e.SetAuthor(author)
	e.SetLang(langToCode(targetLang))
	if synopsis != "" {
		e.SetDescription(synopsis)
	}

	dir := translation.GetTextDirection(targetLang)
	if dir == translation.RightToLeft {
		e.SetPpd("rtl")
	} else {
		e.SetPpd("ltr")
	}

	// Build sections: group consecutive paragraphs until next IsChapterStart.
	var sectionParagraphs []translation.Paragraph
	var sectionsAdded int
	flushSection := func(sectionTitle string, sectionNum int) error {
		if len(sectionParagraphs) == 0 {
			return nil
		}
		var body strings.Builder
		for _, para := range sectionParagraphs {
			text := strings.TrimSpace(para.Text)
			if text == "" {
				continue
			}
			body.WriteString("<p>")
			body.WriteString(html.EscapeString(text))
			body.WriteString("</p>\n")
		}
		if body.Len() == 0 {
			sectionParagraphs = nil
			return nil
		}
		internalFilename := fmt.Sprintf("section_%d.xhtml", sectionNum)
		_, err := e.AddSection(body.String(), sectionTitle, internalFilename, "")
		sectionParagraphs = nil
		if err == nil {
			sectionsAdded++
		}
		return err
	}

	sectionNum := 0
	for _, para := range paragraphs {
		if para.IsChapterStart && len(sectionParagraphs) > 0 {
			sectionTitle := fmt.Sprintf("Chapter %d", sectionNum)
			if err := flushSection(sectionTitle, sectionNum); err != nil {
				return fmt.Errorf("add section: %w", err)
			}
			sectionNum++
		}
		sectionParagraphs = append(sectionParagraphs, para)
	}
	if len(sectionParagraphs) > 0 {
		sectionTitle := "Chapter " + fmt.Sprintf("%d", sectionNum)
		if sectionNum == 0 {
			sectionTitle = "Content"
		}
		if err := flushSection(sectionTitle, sectionNum); err != nil {
			return fmt.Errorf("add section: %w", err)
		}
	}
	// Ensure at least one section so the EPUB is valid and non-empty (e.g. all target paragraphs empty).
	if sectionsAdded == 0 {
		_, err := e.AddSection("<p></p>", "Content", "section_0.xhtml", "")
		if err != nil {
			return fmt.Errorf("add section: %w", err)
		}
	}

	if err := e.Write(destPath); err != nil {
		return fmt.Errorf("write epub: %w", err)
	}
	log.Printf("exported epub to %s", destPath)
	return nil
}

// ProjectToEPUBWriter writes the project's translation and metadata as EPUB to w.
// The caller is responsible for closing w. Use this when the destination is an io.Writer (e.g. Android download stream).
func ProjectToEPUBWriter(p *translation.Project, w io.Writer) error {
	p.Lock()
	title := p.Title
	if title == "" {
		title = p.Name
	}
	author := p.Author
	synopsis := p.Synopsis
	targetLang := p.Target.Language
	paragraphs := make([]translation.Paragraph, len(p.Target.Paragraphs))
	copy(paragraphs, p.Target.Paragraphs)
	p.Unlock()

	e, err := epub.NewEpub(title)
	if err != nil {
		return fmt.Errorf("create epub: %w", err)
	}

	e.SetAuthor(author)
	e.SetLang(langToCode(targetLang))
	if synopsis != "" {
		e.SetDescription(synopsis)
	}

	dir := translation.GetTextDirection(targetLang)
	if dir == translation.RightToLeft {
		e.SetPpd("rtl")
	} else {
		e.SetPpd("ltr")
	}

	var sectionParagraphs []translation.Paragraph
	var sectionsAdded int
	flushSection := func(sectionTitle string, sectionNum int) error {
		if len(sectionParagraphs) == 0 {
			return nil
		}
		var body strings.Builder
		for _, para := range sectionParagraphs {
			text := strings.TrimSpace(para.Text)
			if text == "" {
				continue
			}
			body.WriteString("<p>")
			body.WriteString(html.EscapeString(text))
			body.WriteString("</p>\n")
		}
		if body.Len() == 0 {
			sectionParagraphs = nil
			return nil
		}
		internalFilename := fmt.Sprintf("section_%d.xhtml", sectionNum)
		_, err := e.AddSection(body.String(), sectionTitle, internalFilename, "")
		sectionParagraphs = nil
		if err == nil {
			sectionsAdded++
		}
		return err
	}

	sectionNum := 0
	for _, para := range paragraphs {
		if para.IsChapterStart && len(sectionParagraphs) > 0 {
			sectionTitle := fmt.Sprintf("Chapter %d", sectionNum)
			if err := flushSection(sectionTitle, sectionNum); err != nil {
				return fmt.Errorf("add section: %w", err)
			}
			sectionNum++
		}
		sectionParagraphs = append(sectionParagraphs, para)
	}
	if len(sectionParagraphs) > 0 {
		sectionTitle := "Chapter " + fmt.Sprintf("%d", sectionNum)
		if sectionNum == 0 {
			sectionTitle = "Content"
		}
		if err := flushSection(sectionTitle, sectionNum); err != nil {
			return fmt.Errorf("add section: %w", err)
		}
	}
	// Ensure at least one section so the EPUB is valid and non-empty (e.g. all target paragraphs empty).
	if sectionsAdded == 0 {
		_, err := e.AddSection("<p></p>", "Content", "section_0.xhtml", "")
		if err != nil {
			return fmt.Errorf("add section: %w", err)
		}
	}

	_, err = e.WriteTo(w)
	return err
}
