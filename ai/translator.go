package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/dtylman/saatool/ai/llm"
	"github.com/dtylman/saatool/config"
	"github.com/dtylman/saatool/translation"
)

// TranslationDocument represents a specific document structure for translation requests and responses.
type TranslationDocument struct {
	// Source is the source language unit containing paragraphs to be translated.
	Source translation.Unit `json:"source"`
	// Target is the target language unit where the translated paragraphs will be stored.
	Target translation.Unit `json:"target"`
}

// Translator is responsible for translating text using an LLM provider via OpenRouter
type Translator struct {
	client        *llm.Client
	project       *translation.Project
	inTranslation map[string]time.Time
	mutex         sync.Mutex
	stats         *TranslationStatistics
	//OnTranslationComplete happens after a paragraph is translated and saved to the project
	OnTranslationComplete func(paragraphIndex int, translation string)
}

// NewTranslator creates a new translator using the configured LLM provider
func NewTranslator(project *translation.Project) (*Translator, error) {
	log.Printf("creating new translator for project: '%s'", project.GetTitle())

	client, err := llm.NewClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM client: %w", err)
	}
	return &Translator{client: client,
		project:       project,
		inTranslation: make(map[string]time.Time),
		mutex:         sync.Mutex{},
		stats:         NewTranslationStatistics(),
	}, nil
}

// GetBookDetails retrieves details about a book using the configured LLM provider.
func (t *Translator) GetBookDetails(ctx context.Context) (*BookDetails, error) {
	book := NewBookDetails(t.project)

	bookRequest, err := json.Marshal(book)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal book to JSON: %v", err)
	}

	log.Printf("requesting book details for: %s", book.Title)
	resp, err := t.client.Chat(ctx, llm.ChatRequest{
		SystemPrompt: "You are a librarian.",
		UserPrompt: "Provide required in formation about the book. I need to fill in the provided JSON template. " +
			"Use the title and author fields to search for the book. Correct the existing fields and fill in missing fields. " +
			"Provide details about the main characters, genre, synopsis, and any other relevant information. " +
			"Make an effort to fill in all fields. I am most interested in the gender of the main characters, " +
			"as they are important for the translation effort." +
			"Return the information in the following JSON format: " + string(bookRequest),
		JSON: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create chat completion: %v", err)
	}

	log.Printf("LLM response: %s", resp.Content)

	var bookResponse BookDetails
	err = llm.ExtractJSON(resp.Content, &bookResponse)
	if err != nil {
		return nil, fmt.Errorf("failed to extract JSON from response: %v", err)
	}

	return &bookResponse, nil
}

// IsTranslationInProgress checks if a translation is in progress for a given paragraph ID.
func (t *Translator) SetTranslationInProgress(paragraphID string) error {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	if _, exists := t.inTranslation[paragraphID]; exists {
		return fmt.Errorf("translation for paragraph ID %s is already in progress", paragraphID)
	}
	t.inTranslation[paragraphID] = time.Now()
	return nil
}

// ClearTranslationInProgress clears the translation in progress for a given paragraph ID.
func (t *Translator) ClearTranslationInProgress(paragraphID string) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	delete(t.inTranslation, paragraphID)
}

// newTranslationDocument creates a new translation document for the specified paragraph index.
func (t *Translator) newTranslationDocument(paragraphIndex int, sourceLang string, targetLang string, docSize int) *TranslationDocument {

	doc := &TranslationDocument{
		Source: translation.Unit{
			Language:   sourceLang,
			Paragraphs: make([]translation.Paragraph, 0),
		},
		Target: translation.Unit{
			Language:   targetLang,
			Paragraphs: make([]translation.Paragraph, 0),
		},
	}

	previousParagraphsCount := docSize - 1
	if previousParagraphsCount < 0 {
		previousParagraphsCount = 0
	}
	fromParagraphIndex := paragraphIndex - previousParagraphsCount
	if fromParagraphIndex < 0 {
		fromParagraphIndex = 0
	}
	for i := fromParagraphIndex; i <= paragraphIndex; i++ {
		sourceParagraph, err := t.project.GetSourceParagraph(i)
		if err != nil {
			log.Printf("failed to get source paragraph %d: %v", i, err)
			continue
		}
		doc.Source.Paragraphs = append(doc.Source.Paragraphs, sourceParagraph)
		targetParagraph, err := t.project.GetTargetParagraph(i)
		if err != nil {
			log.Printf("failed to get target paragraph %d: %v", i, err)
			targetParagraph = translation.Paragraph{
				ID:   sourceParagraph.ID,
				Text: "",
			}
		}
		doc.Target.Paragraphs = append(doc.Target.Paragraphs, targetParagraph)
	}
	return doc
}

type translationRequestContext struct {
	sourceParagraph translation.Paragraph
	sourceLang      string
	targetLang      string
	paragraphIndex  int
}

func (t *Translator) newTranslationRequestContext(paragraphIndex int) (*translationRequestContext, error) {
	if t.client == nil {
		return nil, errors.New("LLM client is not initialized")
	}
	sourceParagraph, err := t.project.GetSourceParagraph(paragraphIndex)
	if err != nil {
		return nil, fmt.Errorf("failed to get source paragraph: %v", err)
	}

	sourceLang := t.project.GetSourceLanguage()
	targetLang := t.project.GetTargetLanguage()
	if sourceLang == "" || targetLang == "" {
		return nil, errors.New("source or target language not set")
	}

	err = t.SetTranslationInProgress(sourceParagraph.ID)
	if err != nil {
		return nil, err
	}

	return &translationRequestContext{
		sourceParagraph: sourceParagraph,
		sourceLang:      sourceLang,
		targetLang:      targetLang,
		paragraphIndex:  paragraphIndex,
	}, nil
}

// SimpleProofRead performs a simple proofread of the specified paragraph.
func (t *Translator) SimpleProofRead(ctx context.Context, paragraphIndex int) error {
	log.Printf("proofreading paragraph %d", paragraphIndex)
	rc, err := t.newTranslationRequestContext(paragraphIndex)
	if err != nil {
		return err
	}
	defer t.ClearTranslationInProgress(rc.sourceParagraph.ID)
	t.stats.Started(rc.sourceParagraph.ID, len(rc.sourceParagraph.Text))
	defer t.stats.Completed(rc.sourceParagraph.ID)

	systemPrompt, err := GetPrompt(`You are a professional proofreader and a native speaker of '{{.target_lang}}'. Your task is to proofread the provided text for grammar, spelling, punctuation, and overall readability. Ensure that the text flows well and is easy to understand.`,
		map[string]string{
			"target_lang": rc.targetLang,
		})
	if err != nil {
		return fmt.Errorf("failed to create system prompt: %v", err)
	}

	doc := t.newTranslationDocument(paragraphIndex, rc.sourceLang, rc.targetLang, 0)
	jsonData, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("failed to marshal translation document: %v", err)
	}

	userPrompt, err := GetPrompt(`The provided JSON object contains a 'target' paragraph that needs proofreading. It is a text that had been translated from {{.source_lang}} to '{{.target_lang}}'. The {{.source_lang}} is provided for reference. Please proofread the text in the 'target' paragraph and provide the corrected text in the same JSON format. Here is the JSON object: {{.data}}`,
		map[string]string{
			"source_lang": rc.sourceLang,
			"target_lang": rc.targetLang,
			"data":        string(jsonData),
		})
	if err != nil {
		return fmt.Errorf("failed to create user prompt: %v", err)
	}
	log.Printf("requesting proofreading for paragraph %d from %s to %s", paragraphIndex, rc.sourceLang, rc.targetLang)
	resp, err := t.client.Chat(ctx, llm.ChatRequest{
		SystemPrompt: systemPrompt,
		UserPrompt:   userPrompt,
		JSON:         true,
	})
	if err != nil {
		return fmt.Errorf("failed to create chat completion: %v", err)
	}
	log.Printf("LLM proofreading response: %s", resp.Content)
	var translationResponse TranslationDocument
	err = llm.ExtractJSON(resp.Content, &translationResponse)
	if err != nil {
		return fmt.Errorf("failed to extract JSON from response: %v", err)
	}
	log.Printf("response: %+v", translationResponse)
	translation := translationResponse.Target.Paragraphs[0].Text
	if translation == "" {
		return errors.New("received empty translation from LLM")
	}
	log.Printf("proofread paragraph %d: %s", paragraphIndex, translation)
	err = t.project.SetTranslation(paragraphIndex, translation)
	if err != nil {
		return fmt.Errorf("failed to set translation for paragraph %d: %v", paragraphIndex, err)
	}
	t.onTranslated(paragraphIndex, translation)
	return nil
}

// FixTranslation re-translates the specified paragraph to fix its translation.
func (t *Translator) FixTranslation(ctx context.Context, paragraphIndex int) error {
	log.Printf("fixing translation for paragraph %d", paragraphIndex)
	rc, err := t.newTranslationRequestContext(paragraphIndex)
	if err != nil {
		return err
	}
	defer t.ClearTranslationInProgress(rc.sourceParagraph.ID)
	t.stats.Started(rc.sourceParagraph.ID, len(rc.sourceParagraph.Text))
	defer t.stats.Completed(rc.sourceParagraph.ID)

	bookDetails, err := json.Marshal(NewBookDetails(t.project))
	if err != nil {
		return fmt.Errorf("failed to marshal book details: %v", err)
	}

	systemPrompt, err := GetPrompt(`You are a translation proofreader. The translation '{{.source_lang}}' to '{{.target_lang}}' was reported bad from the readers. Since you are also a native speaker of both '{{.source_lang}}' and '{{.target_lang}}', your task is to re-translate the text in the provided json. Fix any issues with translation, make an extra care to make sure all words and terms in the target language makes sense and are grammatically correct. Also make sure the target text avoids using terms from other languages. Here is some background information about the book being translated: {{.book_details}}`,
		map[string]string{
			"source_lang":  rc.sourceLang,
			"target_lang":  rc.targetLang,
			"book_details": string(bookDetails),
		})

	if err != nil {
		return fmt.Errorf("failed to create system prompt: %v", err)
	}

	translationDocument := t.newTranslationDocument(paragraphIndex, rc.sourceLang, rc.targetLang, 0)
	jsonData, err := json.Marshal(translationDocument)
	if err != nil {
		return fmt.Errorf("failed to marshal translation document: %v", err)
	}
	userPrompt, err := GetPrompt(`The provided JSON object contains a bad 'target' translation. Please re-translate to from 'source' paragraph to '{{.target_lang}}' it and provide the corrected translation in the same JSON format. Here is the JSON object: {{.data}}`,
		map[string]string{
			"target_lang": rc.targetLang,
			"data":        string(jsonData),
		})

	if err != nil {
		return fmt.Errorf("failed to create user prompt: %v", err)
	}

	log.Printf("requesting fix- translation for paragraph %d from %s to %s", paragraphIndex, rc.sourceLang, rc.targetLang)
	resp, err := t.client.Chat(ctx, llm.ChatRequest{
		SystemPrompt: systemPrompt,
		UserPrompt:   userPrompt,
		JSON:         true,
	})
	if err != nil {
		return fmt.Errorf("failed to create chat completion: %v", err)
	}

	log.Printf("LLM fix-translation response: %s", resp.Content)
	var translationResponse TranslationDocument
	err = llm.ExtractJSON(resp.Content, &translationResponse)
	if err != nil {
		return fmt.Errorf("failed to extract JSON from response: %v", err)
	}
	log.Printf("response: %+v", translationResponse)
	translation := translationResponse.Target.Paragraphs[0].Text
	if translation == "" {
		return errors.New("received empty translation from LLM")
	}
	log.Printf("fixed translated paragraph %d: %s", paragraphIndex, translation)
	err = t.project.SetTranslation(paragraphIndex, translation)
	if err != nil {
		return fmt.Errorf("failed to set translation for paragraph %d: %v", paragraphIndex, err)
	}
	t.onTranslated(paragraphIndex, translation)
	return nil
}

// Translate translates the specified paragraph and updates the project with the translation.
func (t *Translator) Translate(ctx context.Context, paragraphIndex int) error {
	log.Printf("translating paragraph %d", paragraphIndex)
	err := t.TranslateParagraph(ctx, paragraphIndex)
	if err != nil {
		return err
	}
	if config.Options.AutoProofread {
		log.Printf("auto-proofreading paragraph %d", paragraphIndex)
		err = t.SimpleProofRead(ctx, paragraphIndex)
		if err != nil {
			return err
		}
	}
	return nil
}

// TranslateParagraph translates a paragraph from the source language to the target language using the configured LLM provider and returns the translated text.
func (t *Translator) TranslateParagraph(ctx context.Context, paragraphIndex int) error {
	log.Printf("translating paragraph %d", paragraphIndex)

	rc, err := t.newTranslationRequestContext(paragraphIndex)
	if err != nil {
		return err
	}

	defer t.ClearTranslationInProgress(rc.sourceParagraph.ID)
	t.stats.Started(rc.sourceParagraph.ID, len(rc.sourceParagraph.Text))
	defer t.stats.Completed(rc.sourceParagraph.ID)

	translationDocument := t.newTranslationDocument(paragraphIndex, rc.sourceLang, rc.targetLang, config.Options.TranslationDocSize)

	data, err := json.Marshal(translationDocument)
	if err != nil {
		return fmt.Errorf("failed to marshal translation document: %v", err)
	}

	bookDetails, err := json.Marshal(NewBookDetails(t.project))
	if err != nil {
		return fmt.Errorf("failed to marshal book details: %v", err)
	}

	systemPrompt, err := GetPrompt(`You are a professional translator from '{{.source_lang}}' to '{{.target_lang}}' and a native speaker of both '{{.source_lang}}' and '{{.target_lang}}'. Your task is to translate '{{.book_title}}', which is a {{.book_type}}. The translation is done paragraph by paragraph. Make sure to translate the text accurately and preserve its meaning and the writer style. The translation should be: accurate; preserve the meaning and style of the original text; be free of grammatical errors; use natural and fluent {{.target_lang}} language; be culturally precise for contemporary {{.target_lang}} readers. Here are some details about the book: {{.book_details}}`,
		map[string]string{
			"source_lang":  rc.sourceLang,
			"target_lang":  rc.targetLang,
			"book_title":   t.project.GetTitle(),
			"book_type":    t.project.GetType(),
			"book_details": string(bookDetails),
		})

	if err != nil {
		return fmt.Errorf("failed to create system prompt: %v", err)
	}

	userPrompt := `I need to provide a JSON object with translated text. The 'source' field contains a list of paragraphs in the source language, and the 'target' field should contain the translated text in the target language. Some of them are already translated, make sure the translation is accurate, if so, keep the same ideas in the new paragraph. Keep translated names and terms consistent. provide the translation in a JSON object. Here is the JSON object: ` + string(data)

	log.Printf("requesting translation for paragraph %d from %s to %s", paragraphIndex, rc.sourceLang, rc.targetLang)
	resp, err := t.client.Chat(ctx, llm.ChatRequest{
		SystemPrompt: systemPrompt,
		UserPrompt:   userPrompt,
		JSON:         true,
	})
	if err != nil {
		return fmt.Errorf("failed to create chat completion: %v", err)
	}

	log.Printf("LLM translation response: %s", resp.Content)

	var translationResponse TranslationDocument
	err = llm.ExtractJSON(resp.Content, &translationResponse)
	if err != nil {
		return fmt.Errorf("failed to extract JSON from response: %v", err)
	}

	log.Printf("response: %+v", translationResponse)
	translation := translationResponse.Target.Paragraphs[len(translationResponse.Target.Paragraphs)-1].Text
	if translation == "" {
		return errors.New("received empty translation from LLM")
	}
	log.Printf("translated paragraph %d: %s", paragraphIndex, translation)

	err = t.project.SetTranslation(paragraphIndex, translation)
	if err != nil {
		return fmt.Errorf("failed to set translation for paragraph %d: %v", paragraphIndex, err)
	}

	t.onTranslated(paragraphIndex, translation)
	return nil

}

func (t *Translator) onTranslated(paragraphIndex int, translation string) {
	if t.OnTranslationComplete != nil {
		t.OnTranslationComplete(paragraphIndex, translation)
	}
}

// Stats returns the estimated time remaining for the next paragraph to complete and the number of paragraphs currently being translated.
func (t *Translator) Stats() (time.Duration, int) {
	return t.stats.NextETA(), t.stats.InProgressCount()
}
