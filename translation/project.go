package translation

import (
	"bytes"
	"compress/gzip"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/dtylman/saatool/config"
)

// Bookmark represents a user-defined reading bookmark.
type Bookmark struct {
	// Index is the paragraph index of the bookmark.
	Index int `json:"index"`
	// Note is an optional note attached to the bookmark.
	Note string `json:"note,omitempty"`
}

// Character represents a character in a translation project.
type Character struct {
	// Name is the name of the character.
	Name string `json:"name"`
	// Gender is the gender of the character
	Gender string `json:"gender,omitempty"`
	// Age is the age of the character.
	Age int `json:"age,omitempty"`
	// Role is the role of the character in the story.
	Role string `json:"role,omitempty"`
	// Description is a brief description of the character.
	Description string `json:"description,omitempty"`
}

// Paragraph represents a single paragraph in a translation project.
type Paragraph struct {
	// ID is a unique identifier for the paragraph.
	ID string `json:"id"`
	// Text is the content of the paragraph.
	Text string `json:"text"`
	// IsChapterStart marks the first paragraph of a new EPUB spine item
	// (chapter, section, preface, etc.). Used by the reader to break pages
	// at chapter boundaries rather than mid-chapter.
	IsChapterStart bool `json:"chapter_start,omitempty"`
}

// CalcID calculates a unique ID for the paragraph based on its text.
func (p *Paragraph) CalcID() string {
	sum := md5.Sum([]byte(p.Text))
	return hex.EncodeToString(sum[:])
}

// Unit represents a collection of paragraphs in a specific language.
type Unit struct {
	// Language is the language of the paragraphs.
	Language string `json:"language"`
	// Paragraphs is a list of paragraphs in this unit.
	Paragraphs []Paragraph `json:"paragraphs"`
}

// Project represents a translation project with source and target languages and paragraphs.
type Project struct {
	// Name is the name of the project.
	Name string `json:"name"`
	// Title is the title of the text being translated.
	Title string `json:"title"`
	// Author is the author of the text being translated.
	Author string `json:"author"`
	// Synopsis is a brief summary of the text.
	Synopsis string `json:"synopsis"`
	// Genre is the genre of the text.
	Genre string `json:"genre"`
	// Characters is a list of characters involved in the text.
	Characters []Character `json:"characters"`
	// WritingStyle describes the author's narrative voice, tone, and pacing.
	// E.g. "third-person omniscient, dark and introspective, fast-paced".
	WritingStyle string `json:"writing_style,omitempty"`
	// Glossary maps source-language terms to their preferred target-language
	// translations. Used to keep made-up words, proper nouns, and specialised
	// terms consistent across the whole book.
	Glossary map[string]string `json:"glossary,omitempty"`
	// Source is the source language unit containing paragraphs to be translated.
	Source Unit `json:"source"`
	// Target is the target language unit where the translated paragraphs will be stored.
	Target Unit `json:"target"`
	// Prompt is the translation prompt or instructions for the translator.
	Prompt string `json:"prompt"`
	// Bookmarks is a list of user-defined reading bookmarks.
	Bookmarks []Bookmark `json:"bookmarks,omitempty"`
	// LastSourceView indicates whether the last view was source or target.
	LastSourceView bool `json:"last_source_view"`
	// LastParagraphIndex is the index of the last viewed paragraph.
	LastParagraphIndex int `json:"last_paragraph_index"`
	// mutex to protect concurrent access
	mutex sync.Mutex
}

// GetTargetLanguage returns the target language of the project.
func (p *Project) GetTargetLanguage() string {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	return p.Target.Language
}

// GetSourceLanguage returns the source language of the project.
func (p *Project) GetSourceLanguage() string {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	return p.Source.Language
}

// GetSourceParagraph returns the source paragraph at the given index.
func (p *Project) GetSourceParagraph(paragraphIndex int) (Paragraph, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if paragraphIndex < 0 || paragraphIndex >= len(p.Source.Paragraphs) {
		return Paragraph{}, fmt.Errorf("paragraph index %d out of range", paragraphIndex)
	}
	return p.Source.Paragraphs[paragraphIndex], nil
}

// GetTargetParagraph returns the target paragraph at the given index.
func (p *Project) GetTargetParagraph(paragraphIndex int) (Paragraph, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if paragraphIndex < 0 || paragraphIndex >= len(p.Target.Paragraphs) {
		return Paragraph{}, fmt.Errorf("paragraph index %d out of range", paragraphIndex)
	}
	return p.Target.Paragraphs[paragraphIndex], nil
}

// NewProject creates a new translation project with empty source and target units.
func NewProject(name string) *Project {
	return &Project{
		Name:               name,
		Source:             Unit{Paragraphs: make([]Paragraph, 0)},
		Target:             Unit{Paragraphs: make([]Paragraph, 0)},
		Characters:         make([]Character, 0),
		LastSourceView:     true,
		LastParagraphIndex: 0,
	}
}

// LoadProject loads a project from the specified file path.
func LoadProject(path string) (*Project, error) {
	log.Printf("loading project from %s", path)

	inFile, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open project file: %v", err)
	}
	defer inFile.Close()

	gzipReader, err := gzip.NewReader(inFile)
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %v", err)
	}
	defer gzipReader.Close()

	data, err := io.ReadAll(gzipReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read project file: %v", err)
	}
	var proj Project
	err = json.Unmarshal(data, &proj)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal project file: %v", err)
	}
	return &proj, nil
}

// Save saves the project to its file.
func (p *Project) Save() (string, error) {

	log.Printf("saving project '%v'", p.Name)
	if p.Name == "" {
		return "", fmt.Errorf("project name is empty")
	}

	fileName := filepath.Join(config.ProjectsDir(), p.Name)
	log.Printf("writing project to %s", fileName)

	if filepath.Ext(fileName) != config.ProjectFileExt {
		fileName += config.ProjectFileExt
	}

	outFile, err := os.Create(fileName)
	if err != nil {
		return "", fmt.Errorf("failed to create project file: %v", err)
	}
	defer outFile.Close()
	n, err := p.SaveToWriter(outFile)
	if err != nil {
		return "", fmt.Errorf("failed to save project: %v", err)
	}

	log.Printf("wrote %d bytes to %s", n, fileName)
	return fileName, nil

}

// SaveToWriter saves the project to the provided writer.
func (p *Project) SaveToWriter(writer io.Writer) (int64, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	gzipWriter := gzip.NewWriter(writer)
	defer gzipWriter.Close()

	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return 0, fmt.Errorf("failed to marshal project: %v", err)
	}

	return io.Copy(gzipWriter, bytes.NewReader(data))
}

// Normalize ensures that the project has valid data.
func (p *Project) Normalize() {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.normalizeUnsafe()
}

func (p *Project) normalizeUnsafe() {
	if len(p.Source.Paragraphs) > len(p.Target.Paragraphs) {
		diff := len(p.Source.Paragraphs) - len(p.Target.Paragraphs)
		log.Printf("normalizing project: adding %d paragraphs to target", diff)
		for i := 0; i < diff; i++ {
			p.Target.Paragraphs = append(p.Target.Paragraphs, Paragraph{})
		}
	}

	for i := range p.Source.Paragraphs {
		if p.Source.Paragraphs[i].ID == "" {
			p.Source.Paragraphs[i].ID = p.Source.Paragraphs[i].CalcID()
			p.Target.Paragraphs[i].ID = p.Source.Paragraphs[i].ID
		}
	}
}

// SetName sets the name of the project.
func (p *Project) SetName(name string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.Name = name
}

// SetTranslation sets the translation for a specific paragraph in the project.
func (p *Project) SetTranslation(paragraph int, translated string) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	log.Printf("setting translation for paragraph %d", paragraph)
	if paragraph < 0 || paragraph >= len(p.Source.Paragraphs) {
		return fmt.Errorf("paragraph index %d out of range", paragraph)
	}
	p.normalizeUnsafe()
	p.Target.Paragraphs[paragraph].Text = translated
	p.Target.Paragraphs[paragraph].ID = p.Source.Paragraphs[paragraph].ID
	return nil
}

// IsEmpty checks if the project has no source or target paragraphs.
func (p *Project) IsEmpty() bool {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	return len(p.Source.Paragraphs) == 0 && len(p.Target.Paragraphs) == 0
}

// Lock locks the project for safe concurrent access.
func (p *Project) Lock() {
	p.mutex.Lock()
}

// Unlock unlocks the project.
func (p *Project) Unlock() {
	p.mutex.Unlock()
}

// GetTitle returns the title of the project.
func (p *Project) GetTitle() string {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	return p.Title
}

// GetType returns the type of the project based on its genre.
func (p *Project) GetType() string {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if p.Genre == "Article" {
		return "article"
	}
	return "book"
}

// SetPosition sets the last viewed position in the project.
func (p *Project) SetPosition(view bool, index int) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.LastSourceView = view
	p.LastParagraphIndex = index
}

// ── Glossary ──────────────────────────────────────────────────────────────────

// GetGlossary returns a shallow copy of the project's glossary (thread-safe).
func (p *Project) GetGlossary() map[string]string {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	result := make(map[string]string, len(p.Glossary))
	for k, v := range p.Glossary {
		result[k] = v
	}
	return result
}

// SetGlossaryEntry adds or updates a single glossary entry (thread-safe).
func (p *Project) SetGlossaryEntry(term, translation string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if p.Glossary == nil {
		p.Glossary = make(map[string]string)
	}
	p.Glossary[term] = translation
}

// DeleteGlossaryEntry removes a glossary entry (thread-safe).
func (p *Project) DeleteGlossaryEntry(term string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	delete(p.Glossary, term)
}

// GetBookmarks returns a copy of all bookmarks (thread-safe).
func (p *Project) GetBookmarks() []Bookmark {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	result := make([]Bookmark, len(p.Bookmarks))
	copy(result, p.Bookmarks)
	return result
}

// AddBookmark adds or updates a bookmark at the given paragraph index (thread-safe).
func (p *Project) AddBookmark(index int, note string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	for i, b := range p.Bookmarks {
		if b.Index == index {
			p.Bookmarks[i] = Bookmark{Index: index, Note: note}
			return
		}
	}
	p.Bookmarks = append(p.Bookmarks, Bookmark{Index: index, Note: note})
}

// DeleteBookmark removes the bookmark at the given paragraph index (thread-safe).
func (p *Project) DeleteBookmark(index int) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	for i, b := range p.Bookmarks {
		if b.Index == index {
			p.Bookmarks = append(p.Bookmarks[:i], p.Bookmarks[i+1:]...)
			return
		}
	}
}

// GetGlossaryFormatted returns the glossary as a ready-to-embed prompt string,
// e.g. '  "butter-beer" → "בירת חמאה"\n'.  Returns "" when the glossary is empty.
func (p *Project) GetGlossaryFormatted() string {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if len(p.Glossary) == 0 {
		return ""
	}
	var sb strings.Builder
	for term, trans := range p.Glossary {
		fmt.Fprintf(&sb, "  \"%s\" → \"%s\"\n", term, trans)
	}
	return sb.String()
}

// DeleteProject deletes the project file from disk.
func DeleteProject(proj *Project) error {
	if proj == nil {
		return fmt.Errorf("project is nil")
	}
	if proj.Name == "" {
		return fmt.Errorf("project name is empty")
	}
	fileName := filepath.Join(config.ProjectsDir(), proj.Name)
	if filepath.Ext(fileName) != config.ProjectFileExt {
		fileName += config.ProjectFileExt
	}
	log.Printf("deleting project file %s", fileName)
	err := os.Remove(fileName)
	if err != nil {
		return fmt.Errorf("failed to delete project file: %v", err)
	}
	return nil
}
