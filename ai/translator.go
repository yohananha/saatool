package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"slices"
	"sync"
	"time"

	deepseek "github.com/cohesion-org/deepseek-go"
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

// Translator is responsible for translating text using DeepSeek API
type Translator struct {
	client        *deepseek.Client
	project       *translation.Project
	inTranslation map[string]time.Time
	mutex         sync.Mutex
	stats         *TranslationStatistics
	// OnTranslationComplete happens after a paragraph is translated and saved to the project
	OnTranslationComplete func(paragraphIndex int, translation string)

	// ── Change 1: pre-computed, constant-per-project values ──────────────────
	// These are computed once in NewTranslator and reused on every API call,
	// eliminating repeated JSON marshalling and template rendering.
	cachedBookDetailsJSON    string // JSON-marshalled BookDetails
	cachedTranslateSysPrompt string // rendered system prompt for TranslateParagraph
	cachedFixSysPrompt       string // rendered system prompt for FixTranslation
	cachedProofreadSysPrompt string // rendered system prompt for SimpleProofRead
}

// maxRetries is the total number of attempts (1 original + 2 retries) for each API call.
const maxRetries = 3

// NewTranslator creates a new Translator for the given project.
// It pre-computes constant per-project values (book-details JSON, all system prompts)
// so that every subsequent API call can reuse them without recomputation.
func NewTranslator(project *translation.Project) (*Translator, error) {
	log.Printf("creating new translator for project: '%s'", project.GetTitle())

	client := deepseek.NewClient(config.Options.DeepSeekAPIKey)
	if client == nil {
		return nil, fmt.Errorf("failed to create DeepSeek client")
	}

	t := &Translator{
		client:        client,
		project:       project,
		inTranslation: make(map[string]time.Time),
		stats:         NewTranslationStatistics(),
	}

	// ── Cache book details JSON ───────────────────────────────────────────────
	bookDetailsBytes, err := json.Marshal(NewBookDetails(project))
	if err != nil {
		log.Printf("warning: could not marshal book details for cache: %v", err)
	} else {
		t.cachedBookDetailsJSON = string(bookDetailsBytes)
	}

	sourceLang := project.GetSourceLanguage()
	targetLang := project.GetTargetLanguage()

	// ── Cache translate system prompt ─────────────────────────────────────────
	translateSys, err := GetPrompt(
		`You are a professional translator from '{{.source_lang}}' to '{{.target_lang}}' and a native speaker of both '{{.source_lang}}' and '{{.target_lang}}'. Your task is to translate '{{.book_title}}', which is a {{.book_type}}. The translation is done paragraph by paragraph.{{if .writing_style}} The author's writing style is: {{.writing_style}}.{{end}} Make sure to translate the text accurately and preserve its meaning and the writer style. The translation should be: accurate; preserve the meaning and style of the original text; be free of grammatical errors; use natural and fluent {{.target_lang}} language; be culturally precise for contemporary {{.target_lang}} readers.{{if .glossary}} Use this glossary for consistent term translation:
{{.glossary}}{{end}} Here are some details about the book: {{.book_details}}`,
		map[string]string{
			"source_lang":   sourceLang,
			"target_lang":   targetLang,
			"book_title":    project.GetTitle(),
			"book_type":     project.GetType(),
			"writing_style": project.WritingStyle,
			"book_details":  t.cachedBookDetailsJSON,
			"glossary":      project.GetGlossaryFormatted(),
		},
	)
	if err != nil {
		log.Printf("warning: could not pre-render translate system prompt: %v", err)
	} else {
		t.cachedTranslateSysPrompt = translateSys
	}

	// ── Cache fix-translation system prompt ───────────────────────────────────
	fixSys, err := GetPrompt(
		`You are a translation proofreader. The translation '{{.source_lang}}' to '{{.target_lang}}' was reported bad from the readers. Since you are also a native speaker of both '{{.source_lang}}' and '{{.target_lang}}', your task is to re-translate the text in the provided json. Fix any issues with translation, make an extra care to make sure all words and terms in the target language makes sense and are grammatically correct. Also make sure the target text avoids using terms from other languages.{{if .writing_style}} Preserve the author's writing style: {{.writing_style}}.{{end}}{{if .glossary}} Use this glossary for consistent term translation:
{{.glossary}}{{end}} Here is some background information about the book being translated: {{.book_details}}`,
		map[string]string{
			"source_lang":   sourceLang,
			"target_lang":   targetLang,
			"writing_style": project.WritingStyle,
			"book_details":  t.cachedBookDetailsJSON,
			"glossary":      project.GetGlossaryFormatted(),
		},
	)
	if err != nil {
		log.Printf("warning: could not pre-render fix system prompt: %v", err)
	} else {
		t.cachedFixSysPrompt = fixSys
	}

	// ── Cache proofread system prompt ─────────────────────────────────────────
	proofSys, err := GetPrompt(
		`You are a professional proofreader and a native speaker of '{{.target_lang}}'. Your task is to proofread the provided text for grammar, spelling, punctuation, and overall readability. Ensure that the text flows well and is easy to understand.`,
		map[string]string{"target_lang": targetLang},
	)
	if err != nil {
		log.Printf("warning: could not pre-render proofread system prompt: %v", err)
	} else {
		t.cachedProofreadSysPrompt = proofSys
	}

	return t, nil
}

// ── Change 4: callAPI — exponential-backoff retry wrapper ────────────────────

// callAPI calls the DeepSeek API with up to maxRetries attempts.
// Delays between retries double on each failure: 1 s → 2 s → 4 s …
// Context cancellation / deadline is never retried.
func (t *Translator) callAPI(ctx context.Context, req *deepseek.ChatCompletionRequest) (*deepseek.ChatCompletionResponse, error) {
	var (
		resp  *deepseek.ChatCompletionResponse
		err   error
		delay = time.Second
	)
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("api retry %d/%d after %v", attempt, maxRetries-1, delay)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			delay *= 2
		}

		resp, err = t.client.CreateChatCompletion(ctx, req)
		if err == nil && resp != nil && len(resp.Choices) > 0 {
			return resp, nil
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return resp, err
		}
		if attempt < maxRetries-1 {
			log.Printf("api call failed (attempt %d/%d): %v", attempt+1, maxRetries, err)
		}
	}
	return resp, err
}

// ── In-flight deduplication ───────────────────────────────────────────────────

// SetTranslationInProgress marks a paragraph as currently being translated.
// Returns an error if that paragraph ID is already in-flight.
func (t *Translator) SetTranslationInProgress(paragraphID string) error {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	if _, exists := t.inTranslation[paragraphID]; exists {
		return fmt.Errorf("translation for paragraph ID %s is already in progress", paragraphID)
	}
	t.inTranslation[paragraphID] = time.Now()
	return nil
}

// ClearTranslationInProgress removes the in-progress marker for a paragraph.
func (t *Translator) ClearTranslationInProgress(paragraphID string) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	delete(t.inTranslation, paragraphID)
}

// ── Change 2: smart context window ───────────────────────────────────────────

// newTranslationDocument builds a translation document for the given paragraph.
// Context paragraphs (docSize-1 entries before the current one) are chosen by
// walking backwards and selecting only paragraphs that already have a non-empty
// translation — giving the AI real examples for consistency instead of empty strings.
// The current paragraph is always appended last, regardless of translation state.
func (t *Translator) newTranslationDocument(paragraphIndex int, sourceLang, targetLang string, docSize int) *TranslationDocument {
	doc := &TranslationDocument{
		Source: translation.Unit{Language: sourceLang, Paragraphs: make([]translation.Paragraph, 0)},
		Target: translation.Unit{Language: targetLang, Paragraphs: make([]translation.Paragraph, 0)},
	}

	if docSize <= 0 {
		t.appendParagraphToDoc(doc, paragraphIndex)
		return doc
	}

	// Collect up to (docSize-1) already-translated paragraphs before the current one.
	// Walk backwards so we naturally get the most recent ones first, then reverse.
	maxContext := docSize - 1
	contextIndices := make([]int, 0, maxContext)
	for i := paragraphIndex - 1; i >= 0 && len(contextIndices) < maxContext; i-- {
		tgt, err := t.project.GetTargetParagraph(i)
		if err == nil && tgt.Text != "" {
			contextIndices = append(contextIndices, i)
		}
	}
	slices.Reverse(contextIndices) // oldest first → newest second-to-last → current last

	for _, i := range contextIndices {
		t.appendParagraphToDoc(doc, i)
	}
	t.appendParagraphToDoc(doc, paragraphIndex)
	return doc
}

// appendParagraphToDoc adds a source+target paragraph pair to the document.
func (t *Translator) appendParagraphToDoc(doc *TranslationDocument, index int) {
	src, err := t.project.GetSourceParagraph(index)
	if err != nil {
		log.Printf("failed to get source paragraph %d: %v", index, err)
		return
	}
	doc.Source.Paragraphs = append(doc.Source.Paragraphs, src)

	tgt, err := t.project.GetTargetParagraph(index)
	if err != nil {
		tgt = translation.Paragraph{ID: src.ID, Text: ""}
	}
	doc.Target.Paragraphs = append(doc.Target.Paragraphs, tgt)
}

// ── translationRequestContext ─────────────────────────────────────────────────

type translationRequestContext struct {
	sourceParagraph translation.Paragraph
	sourceLang      string
	targetLang      string
	paragraphIndex  int
}

func (t *Translator) newTranslationRequestContext(paragraphIndex int) (*translationRequestContext, error) {
	if t.client == nil {
		return nil, errors.New("DeepSeek client is not initialized")
	}
	src, err := t.project.GetSourceParagraph(paragraphIndex)
	if err != nil {
		return nil, fmt.Errorf("failed to get source paragraph: %v", err)
	}

	sourceLang := t.project.GetSourceLanguage()
	targetLang := t.project.GetTargetLanguage()
	if sourceLang == "" || targetLang == "" {
		return nil, errors.New("source or target language not set")
	}

	if err := t.SetTranslationInProgress(src.ID); err != nil {
		return nil, err
	}

	return &translationRequestContext{
		sourceParagraph: src,
		sourceLang:      sourceLang,
		targetLang:      targetLang,
		paragraphIndex:  paragraphIndex,
	}, nil
}

// ── GetBookDetails ────────────────────────────────────────────────────────────

// GetBookDetails asks the AI to enrich the project's book metadata.
// This is separate from the translation pipeline and makes its own API call.
func (t *Translator) GetBookDetails(ctx context.Context) (*BookDetails, error) {
	book := NewBookDetails(t.project)
	bookRequest, err := json.Marshal(book)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal book to JSON: %v", err)
	}

	log.Printf("requesting book details for: %s", book.Title)
	resp, err := t.callAPI(ctx, &deepseek.ChatCompletionRequest{
		Model: deepseek.DeepSeekChat,
		Messages: []deepseek.ChatCompletionMessage{
			{Role: deepseek.ChatMessageRoleSystem, Content: "You are a librarian."},
			{
				Role: deepseek.ChatMessageRoleUser,
				Content: "Provide required information about the book. I need to fill in the provided JSON template. " +
					"Use the title and author fields to search for the book. Correct the existing fields and fill in missing fields. " +
					"Provide details about the main characters, genre, synopsis, and any other relevant information. " +
					"For 'writing_style', describe the author's narrative voice, tone, mood, and pacing in one or two sentences " +
					"(e.g. 'first-person, humorous and fast-paced with witty dialogue' or " +
					"'third-person omniscient, lyrical and introspective, slow-burning tension'). " +
					"Make an effort to fill in all fields. I am most interested in the gender of the main characters " +
					"and the writing style, as they are critical for the translation effort. " +
					"Return the information in the following JSON format: " + string(bookRequest),
			},
		},
		JSONMode: true,
	})
	if resp == nil && err == nil {
		return nil, errors.New("received nil response from DeepSeek API")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create chat completion: %v", err)
	}
	if len(resp.Choices) == 0 {
		return nil, errors.New("no choices returned from chat completion")
	}

	log.Printf("DeepSeek response: %s", resp.Choices[0].Message.Content)
	var bookResponse BookDetails
	extractor := deepseek.NewJSONExtractor(nil)
	if err := extractor.ExtractJSON(resp, &bookResponse); err != nil {
		return nil, fmt.Errorf("failed to extract JSON from response: %v", err)
	}
	return &bookResponse, nil
}

// ── SimpleProofRead ───────────────────────────────────────────────────────────

// SimpleProofRead proofreads the translated text of the specified paragraph.
func (t *Translator) SimpleProofRead(ctx context.Context, paragraphIndex int) error {
	log.Printf("proofreading paragraph %d", paragraphIndex)
	rc, err := t.newTranslationRequestContext(paragraphIndex)
	if err != nil {
		return err
	}
	defer t.ClearTranslationInProgress(rc.sourceParagraph.ID)
	t.stats.Started(rc.sourceParagraph.ID, len(rc.sourceParagraph.Text))
	defer t.stats.Completed(rc.sourceParagraph.ID)

	// Use cached system prompt; fall back to on-demand rendering if empty.
	systemPrompt := t.cachedProofreadSysPrompt
	if systemPrompt == "" {
		systemPrompt, err = GetPrompt(
			`You are a professional proofreader and a native speaker of '{{.target_lang}}'. Your task is to proofread the provided text for grammar, spelling, punctuation, and overall readability. Ensure that the text flows well and is easy to understand.`,
			map[string]string{"target_lang": rc.targetLang},
		)
		if err != nil {
			return fmt.Errorf("failed to create system prompt: %v", err)
		}
	}

	doc := t.newTranslationDocument(paragraphIndex, rc.sourceLang, rc.targetLang, 0)
	jsonData, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("failed to marshal translation document: %v", err)
	}

	userPrompt, err := GetPrompt(
		`The provided JSON object contains a 'target' paragraph that needs proofreading. It is a text that had been translated from {{.source_lang}} to '{{.target_lang}}'. The {{.source_lang}} is provided for reference. Please proofread the text in the 'target' paragraph and provide the corrected text in the same JSON format. Here is the JSON object: {{.data}}`,
		map[string]string{
			"source_lang": rc.sourceLang,
			"target_lang": rc.targetLang,
			"data":        string(jsonData),
		},
	)
	if err != nil {
		return fmt.Errorf("failed to create user prompt: %v", err)
	}

	log.Printf("requesting proofreading for paragraph %d from %s to %s", paragraphIndex, rc.sourceLang, rc.targetLang)
	resp, err := t.callAPI(ctx, &deepseek.ChatCompletionRequest{
		Model: deepseek.DeepSeekChat,
		Messages: []deepseek.ChatCompletionMessage{
			{Role: deepseek.ChatMessageRoleSystem, Content: systemPrompt},
			{Role: deepseek.ChatMessageRoleUser, Content: userPrompt},
		},
		JSONMode: true,
	})
	if resp == nil {
		return errors.New("received nil response from DeepSeek API")
	}
	if err != nil {
		return fmt.Errorf("failed to create chat completion: %v", err)
	}
	if len(resp.Choices) == 0 {
		return errors.New("no choices returned from chat completion")
	}

	log.Printf("DeepSeek proofreading response: %s", resp.Choices[0].Message.Content)
	var translationResponse TranslationDocument
	extractor := deepseek.NewJSONExtractor(nil)
	if err := extractor.ExtractJSON(resp, &translationResponse); err != nil {
		return fmt.Errorf("failed to extract JSON from response: %v", err)
	}

	proofed := translationResponse.Target.Paragraphs[0].Text
	if proofed == "" {
		return errors.New("received empty translation from DeepSeek API")
	}
	log.Printf("proofread paragraph %d: %s", paragraphIndex, proofed)
	if err := t.project.SetTranslation(paragraphIndex, proofed); err != nil {
		return fmt.Errorf("failed to set translation for paragraph %d: %v", paragraphIndex, err)
	}
	t.onTranslated(paragraphIndex, proofed)
	return nil
}

// ── FixTranslation ────────────────────────────────────────────────────────────

// FixTranslation re-translates the specified paragraph to correct a bad translation.
func (t *Translator) FixTranslation(ctx context.Context, paragraphIndex int) error {
	log.Printf("fixing translation for paragraph %d", paragraphIndex)
	rc, err := t.newTranslationRequestContext(paragraphIndex)
	if err != nil {
		return err
	}
	defer t.ClearTranslationInProgress(rc.sourceParagraph.ID)
	t.stats.Started(rc.sourceParagraph.ID, len(rc.sourceParagraph.Text))
	defer t.stats.Completed(rc.sourceParagraph.ID)

	// Use cached fix system prompt; fall back to on-demand rendering if empty.
	systemPrompt := t.cachedFixSysPrompt
	if systemPrompt == "" {
		systemPrompt, err = GetPrompt(
			`You are a translation proofreader. The translation '{{.source_lang}}' to '{{.target_lang}}' was reported bad from the readers. Since you are also a native speaker of both '{{.source_lang}}' and '{{.target_lang}}', your task is to re-translate the text in the provided json. Fix any issues with translation, make an extra care to make sure all words and terms in the target language makes sense and are grammatically correct. Also make sure the target text avoids using terms from other languages.{{if .writing_style}} Preserve the author's writing style: {{.writing_style}}.{{end}} Here is some background information about the book being translated: {{.book_details}}`,
			map[string]string{
				"source_lang":   rc.sourceLang,
				"target_lang":   rc.targetLang,
				"writing_style": t.project.WritingStyle,
				"book_details":  t.cachedBookDetailsJSON,
			},
		)
		if err != nil {
			return fmt.Errorf("failed to create system prompt: %v", err)
		}
	}

	// Use the normal context window so the fix has style examples to match
	translationDocument := t.newTranslationDocument(paragraphIndex, rc.sourceLang, rc.targetLang, config.Options.TranslationDocSize)
	jsonData, err := json.Marshal(translationDocument)
	if err != nil {
		return fmt.Errorf("failed to marshal translation document: %v", err)
	}

	userPrompt, err := GetPrompt(
		`The provided JSON object contains a bad 'target' translation. Please re-translate to from 'source' paragraph to '{{.target_lang}}' it and provide the corrected translation in the same JSON format. Here is the JSON object: {{.data}}`,
		map[string]string{
			"target_lang": rc.targetLang,
			"data":        string(jsonData),
		},
	)
	if err != nil {
		return fmt.Errorf("failed to create user prompt: %v", err)
	}

	fixModel := config.Options.FixModel
	if fixModel == "" {
		fixModel = deepseek.DeepSeekChat
	}
	log.Printf("requesting fix-translation for paragraph %d from %s to %s (model: %s)", paragraphIndex, rc.sourceLang, rc.targetLang, fixModel)
	resp, err := t.callAPI(ctx, &deepseek.ChatCompletionRequest{
		Model: fixModel,
		Messages: []deepseek.ChatCompletionMessage{
			{Role: deepseek.ChatMessageRoleSystem, Content: systemPrompt},
			{Role: deepseek.ChatMessageRoleUser, Content: userPrompt},
		},
		JSONMode: true,
	})
	if resp == nil {
		return errors.New("received nil response from DeepSeek API")
	}
	if err != nil {
		return fmt.Errorf("failed to create chat completion: %v", err)
	}
	if len(resp.Choices) == 0 {
		return errors.New("no choices returned from chat completion")
	}

	log.Printf("DeepSeek fix-translation response: %s", resp.Choices[0].Message.Content)
	var translationResponse TranslationDocument
	extractor := deepseek.NewJSONExtractor(nil)
	if err := extractor.ExtractJSON(resp, &translationResponse); err != nil {
		return fmt.Errorf("failed to extract JSON from response: %v", err)
	}

	fixed := translationResponse.Target.Paragraphs[0].Text
	if fixed == "" {
		return errors.New("received empty translation from DeepSeek API")
	}
	log.Printf("fixed translated paragraph %d: %s", paragraphIndex, fixed)
	if err := t.project.SetTranslation(paragraphIndex, fixed); err != nil {
		return fmt.Errorf("failed to set translation for paragraph %d: %v", paragraphIndex, err)
	}
	t.onTranslated(paragraphIndex, fixed)
	return nil
}

// ── Batch translation ─────────────────────────────────────────────────────────

// TranslateBatch translates up to len(indices) paragraphs in a single API call.
// Already-translated paragraphs are skipped (onTranslated is still fired for them).
// Falls back to single-paragraph TranslateParagraph when only one index remains.
func (t *Translator) TranslateBatch(ctx context.Context, indices []int) error {
	if len(indices) == 0 {
		return nil
	}

	sourceLang := t.project.GetSourceLanguage()
	targetLang := t.project.GetTargetLanguage()
	if sourceLang == "" || targetLang == "" {
		return errors.New("source or target language not set")
	}

	// Filter out already-translated paragraphs.
	toTranslate := make([]int, 0, len(indices))
	for _, idx := range indices {
		if existing, err := t.project.GetTargetParagraph(idx); err == nil && existing.Text != "" {
			log.Printf("paragraph %d already translated — skipping", idx)
			t.onTranslated(idx, existing.Text)
		} else {
			toTranslate = append(toTranslate, idx)
		}
	}
	if len(toTranslate) == 0 {
		return nil
	}
	if len(toTranslate) == 1 {
		return t.TranslateParagraph(ctx, toTranslate[0])
	}

	// Mark all as in-progress; collect IDs for deferred cleanup.
	var inProgressIDs []string
	for _, idx := range toTranslate {
		src, err := t.project.GetSourceParagraph(idx)
		if err != nil {
			continue
		}
		if err := t.SetTranslationInProgress(src.ID); err != nil {
			log.Printf("paragraph %d already in progress, skipping", idx)
			continue
		}
		inProgressIDs = append(inProgressIDs, src.ID)
	}
	defer func() {
		for _, id := range inProgressIDs {
			t.ClearTranslationInProgress(id)
		}
	}()

	// Build document: context before first index + all batch source paragraphs.
	doc := &TranslationDocument{
		Source: translation.Unit{Language: sourceLang, Paragraphs: make([]translation.Paragraph, 0)},
		Target: translation.Unit{Language: targetLang, Paragraphs: make([]translation.Paragraph, 0)},
	}
	firstIdx := toTranslate[0]
	maxContext := config.Options.TranslationDocSize - 1
	contextIndices := make([]int, 0, maxContext)
	for i := firstIdx - 1; i >= 0 && len(contextIndices) < maxContext; i-- {
		tgt, err := t.project.GetTargetParagraph(i)
		if err == nil && tgt.Text != "" {
			contextIndices = append(contextIndices, i)
		}
	}
	slices.Reverse(contextIndices)
	for _, i := range contextIndices {
		t.appendParagraphToDoc(doc, i)
	}
	for _, idx := range toTranslate {
		t.appendParagraphToDoc(doc, idx)
	}

	data, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("failed to marshal batch translation document: %v", err)
	}

	systemPrompt := t.cachedTranslateSysPrompt
	if systemPrompt == "" {
		systemPrompt, err = GetPrompt(
			`You are a professional translator from '{{.source_lang}}' to '{{.target_lang}}' and a native speaker of both. Translate accurately and preserve the author's style.`,
			map[string]string{"source_lang": sourceLang, "target_lang": targetLang},
		)
		if err != nil {
			return fmt.Errorf("failed to build system prompt: %v", err)
		}
	}

	userPrompt := `I need to provide a JSON object with translated text. The 'source' field contains a list of paragraphs in the source language, and the 'target' field should contain the translated text in the target language. Some of them are already translated — keep those translations consistent. Provide all translations in a JSON object. Here is the JSON object: ` + string(data)

	log.Printf("requesting batch translation for %d paragraphs (%v)", len(toTranslate), toTranslate)
	resp, err := t.callAPI(ctx, &deepseek.ChatCompletionRequest{
		Model: deepseek.DeepSeekChat,
		Messages: []deepseek.ChatCompletionMessage{
			{Role: deepseek.ChatMessageRoleSystem, Content: systemPrompt},
			{Role: deepseek.ChatMessageRoleUser, Content: userPrompt},
		},
		JSONMode: true,
	})
	if resp == nil {
		return errors.New("received nil response from DeepSeek API")
	}
	if err != nil {
		return fmt.Errorf("failed to create chat completion: %v", err)
	}
	if len(resp.Choices) == 0 {
		return errors.New("no choices returned from chat completion")
	}

	var translationResponse TranslationDocument
	extractor := deepseek.NewJSONExtractor(nil)
	if err := extractor.ExtractJSON(resp, &translationResponse); err != nil {
		return fmt.Errorf("failed to extract JSON from response: %v", err)
	}

	responseParas := translationResponse.Target.Paragraphs
	offset := len(responseParas) - len(toTranslate)
	if offset < 0 {
		return fmt.Errorf("unexpected batch response: got %d target paragraphs, expected at least %d", len(responseParas), len(toTranslate))
	}
	for i, idx := range toTranslate {
		text := responseParas[offset+i].Text
		if text == "" {
			log.Printf("empty translation for paragraph %d in batch — skipping", idx)
			continue
		}
		log.Printf("batch translated paragraph %d: %s", idx, text)
		if err := t.project.SetTranslation(idx, text); err != nil {
			log.Printf("failed to set translation for paragraph %d: %v", idx, err)
			continue
		}
		t.onTranslated(idx, text)
	}
	return nil
}

// ProofReadBatch proofreads up to len(indices) already-translated paragraphs in a single API call.
// Falls back to single-paragraph SimpleProofRead when only one index remains.
func (t *Translator) ProofReadBatch(ctx context.Context, indices []int) error {
	if len(indices) == 0 {
		return nil
	}

	sourceLang := t.project.GetSourceLanguage()
	targetLang := t.project.GetTargetLanguage()

	// Only proofread paragraphs that already have a translation.
	toProofread := make([]int, 0, len(indices))
	for _, idx := range indices {
		if existing, err := t.project.GetTargetParagraph(idx); err == nil && existing.Text != "" {
			toProofread = append(toProofread, idx)
		}
	}
	if len(toProofread) == 0 {
		return nil
	}
	if len(toProofread) == 1 {
		return t.SimpleProofRead(ctx, toProofread[0])
	}

	// Mark all as in-progress.
	var inProgressIDs []string
	for _, idx := range toProofread {
		src, err := t.project.GetSourceParagraph(idx)
		if err != nil {
			continue
		}
		if err := t.SetTranslationInProgress(src.ID); err != nil {
			log.Printf("paragraph %d already in progress for proofread, skipping", idx)
			continue
		}
		inProgressIDs = append(inProgressIDs, src.ID)
	}
	defer func() {
		for _, id := range inProgressIDs {
			t.ClearTranslationInProgress(id)
		}
	}()

	doc := &TranslationDocument{
		Source: translation.Unit{Language: sourceLang, Paragraphs: make([]translation.Paragraph, 0)},
		Target: translation.Unit{Language: targetLang, Paragraphs: make([]translation.Paragraph, 0)},
	}
	for _, idx := range toProofread {
		t.appendParagraphToDoc(doc, idx)
	}

	jsonData, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("failed to marshal batch proofread document: %v", err)
	}

	systemPrompt := t.cachedProofreadSysPrompt
	if systemPrompt == "" {
		var err2 error
		systemPrompt, err2 = GetPrompt(
			`You are a professional proofreader and a native speaker of '{{.target_lang}}'. Proofread the provided text for grammar, spelling, punctuation, and readability.`,
			map[string]string{"target_lang": targetLang},
		)
		if err2 != nil {
			return fmt.Errorf("failed to build proofread system prompt: %v", err2)
		}
	}

	userPrompt, err := GetPrompt(
		`The provided JSON object contains 'target' paragraphs that need proofreading. They were translated from {{.source_lang}} to '{{.target_lang}}'. The {{.source_lang}} source is provided for reference. Please proofread all 'target' paragraphs and return the corrected text in the same JSON format. Here is the JSON object: {{.data}}`,
		map[string]string{
			"source_lang": sourceLang,
			"target_lang": targetLang,
			"data":        string(jsonData),
		},
	)
	if err != nil {
		return fmt.Errorf("failed to create batch proofread user prompt: %v", err)
	}

	log.Printf("requesting batch proofread for %d paragraphs (%v)", len(toProofread), toProofread)
	resp, err := t.callAPI(ctx, &deepseek.ChatCompletionRequest{
		Model: deepseek.DeepSeekChat,
		Messages: []deepseek.ChatCompletionMessage{
			{Role: deepseek.ChatMessageRoleSystem, Content: systemPrompt},
			{Role: deepseek.ChatMessageRoleUser, Content: userPrompt},
		},
		JSONMode: true,
	})
	if resp == nil {
		return errors.New("received nil response from DeepSeek API")
	}
	if err != nil {
		return fmt.Errorf("failed to create chat completion: %v", err)
	}
	if len(resp.Choices) == 0 {
		return errors.New("no choices returned from chat completion")
	}

	var proofResponse TranslationDocument
	extractor := deepseek.NewJSONExtractor(nil)
	if err := extractor.ExtractJSON(resp, &proofResponse); err != nil {
		return fmt.Errorf("failed to extract JSON from batch proofread response: %v", err)
	}

	if len(proofResponse.Target.Paragraphs) < len(toProofread) {
		return fmt.Errorf("unexpected batch proofread response: got %d paragraphs, expected %d",
			len(proofResponse.Target.Paragraphs), len(toProofread))
	}
	for i, idx := range toProofread {
		text := proofResponse.Target.Paragraphs[i].Text
		if text == "" {
			log.Printf("empty proofread result for paragraph %d — skipping", idx)
			continue
		}
		log.Printf("batch proofread paragraph %d: %s", idx, text)
		if err := t.project.SetTranslation(idx, text); err != nil {
			log.Printf("failed to set proofread translation for paragraph %d: %v", idx, err)
			continue
		}
		t.onTranslated(idx, text)
	}
	return nil
}

// TranslateAndProofReadBatch translates a batch and, if AutoProofread is enabled,
// proofreads the results — all in two API calls instead of 2×N.
func (t *Translator) TranslateAndProofReadBatch(ctx context.Context, indices []int) error {
	if err := t.TranslateBatch(ctx, indices); err != nil {
		return err
	}
	if config.Options.AutoProofread {
		return t.ProofReadBatch(ctx, indices)
	}
	return nil
}

// ── Translate / TranslateParagraph ────────────────────────────────────────────

// Translate translates the paragraph and optionally auto-proofreads it.
func (t *Translator) Translate(ctx context.Context, paragraphIndex int) error {
	log.Printf("translating paragraph %d", paragraphIndex)
	if err := t.TranslateParagraph(ctx, paragraphIndex); err != nil {
		return err
	}
	if config.Options.AutoProofread {
		log.Printf("auto-proofreading paragraph %d", paragraphIndex)
		if err := t.SimpleProofRead(ctx, paragraphIndex); err != nil {
			return err
		}
	}
	return nil
}

// TranslateParagraph sends a paragraph to the DeepSeek API for translation.
func (t *Translator) TranslateParagraph(ctx context.Context, paragraphIndex int) error {
	log.Printf("translating paragraph %d", paragraphIndex)

	// ── Change 3: skip if already translated ─────────────────────────────────
	if existing, err := t.project.GetTargetParagraph(paragraphIndex); err == nil && existing.Text != "" {
		log.Printf("paragraph %d already translated — skipping API call", paragraphIndex)
		t.onTranslated(paragraphIndex, existing.Text)
		return nil
	}

	rc, err := t.newTranslationRequestContext(paragraphIndex)
	if err != nil {
		return err
	}
	defer t.ClearTranslationInProgress(rc.sourceParagraph.ID)
	t.stats.Started(rc.sourceParagraph.ID, len(rc.sourceParagraph.Text))
	defer t.stats.Completed(rc.sourceParagraph.ID)

	// Build the translation document with smart context window (Change 2)
	translationDocument := t.newTranslationDocument(paragraphIndex, rc.sourceLang, rc.targetLang, config.Options.TranslationDocSize)
	data, err := json.Marshal(translationDocument)
	if err != nil {
		return fmt.Errorf("failed to marshal translation document: %v", err)
	}

	// Use cached system prompt; fall back to on-demand rendering if empty (Change 1)
	systemPrompt := t.cachedTranslateSysPrompt
	if systemPrompt == "" {
		systemPrompt, err = GetPrompt(
			`You are a professional translator from '{{.source_lang}}' to '{{.target_lang}}' and a native speaker of both '{{.source_lang}}' and '{{.target_lang}}'. Your task is to translate '{{.book_title}}', which is a {{.book_type}}. The translation is done paragraph by paragraph.{{if .writing_style}} The author's writing style is: {{.writing_style}}.{{end}} Make sure to translate the text accurately and preserve its meaning and the writer style. The translation should be: accurate; preserve the meaning and style of the original text; be free of grammatical errors; use natural and fluent {{.target_lang}} language; be culturally precise for contemporary {{.target_lang}} readers. Here are some details about the book: {{.book_details}}`,
			map[string]string{
				"source_lang":   rc.sourceLang,
				"target_lang":   rc.targetLang,
				"book_title":    t.project.GetTitle(),
				"book_type":     t.project.GetType(),
				"writing_style": t.project.WritingStyle,
				"book_details":  t.cachedBookDetailsJSON,
			},
		)
		if err != nil {
			return fmt.Errorf("failed to create system prompt: %v", err)
		}
	}

	userPrompt := `I need to provide a JSON object with translated text. The 'source' field contains a list of paragraphs in the source language, and the 'target' field should contain the translated text in the target language. Some of them are already translated, make sure the translation is accurate, if so, keep the same ideas in the new paragraph. Keep translated names and terms consistent. provide the translation in a JSON object. Here is the JSON object: ` + string(data)

	log.Printf("requesting translation for paragraph %d from %s to %s", paragraphIndex, rc.sourceLang, rc.targetLang)
	// Change 4: use callAPI with automatic retry on failure
	resp, err := t.callAPI(ctx, &deepseek.ChatCompletionRequest{
		Model: deepseek.DeepSeekChat,
		Messages: []deepseek.ChatCompletionMessage{
			{Role: deepseek.ChatMessageRoleSystem, Content: systemPrompt},
			{Role: deepseek.ChatMessageRoleUser, Content: userPrompt},
		},
		JSONMode: true,
	})
	if resp == nil {
		return errors.New("received nil response from DeepSeek API")
	}
	if err != nil {
		return fmt.Errorf("failed to create chat completion: %v", err)
	}
	if len(resp.Choices) == 0 {
		return errors.New("no choices returned from chat completion")
	}

	log.Printf("DeepSeek translation response: %s", resp.Choices[0].Message.Content)

	var translationResponse TranslationDocument
	extractor := deepseek.NewJSONExtractor(nil)
	if err := extractor.ExtractJSON(resp, &translationResponse); err != nil {
		return fmt.Errorf("failed to extract JSON from response: %v", err)
	}
	log.Printf("response: %+v", translationResponse)

	translated := translationResponse.Target.Paragraphs[len(translationResponse.Target.Paragraphs)-1].Text
	if translated == "" {
		return errors.New("received empty translation from DeepSeek API")
	}
	log.Printf("translated paragraph %d: %s", paragraphIndex, translated)

	if err := t.project.SetTranslation(paragraphIndex, translated); err != nil {
		return fmt.Errorf("failed to set translation for paragraph %d: %v", paragraphIndex, err)
	}
	t.onTranslated(paragraphIndex, translated)
	return nil
}

func (t *Translator) onTranslated(paragraphIndex int, translation string) {
	if t.OnTranslationComplete != nil {
		t.OnTranslationComplete(paragraphIndex, translation)
	}
}

// Stats returns the estimated time until the next paragraph completes and
// the number of paragraphs currently being translated.
func (t *Translator) Stats() (time.Duration, int) {
	return t.stats.NextETA(), t.stats.InProgressCount()
}
