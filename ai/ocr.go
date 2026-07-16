package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/dtylman/saatool/ai/llm"
	"github.com/dtylman/saatool/translation"
)

// OCRInputText represents a piece of text extracted via OCR along with its font size.
type OCRInputText struct {
	Text     string  `json:"text"`
	FontSize float64 `json:"font_size,omitempty"`
}

// OCRRequest represents a request to clean OCR text using the configured LLM provider.
type OCRRequest struct {
	Title     string                  `json:"title"`
	Author    string                  `json:"author"`
	Synopsis  string                  `json:"synopsis"`
	Genre     string                  `json:"genre"`
	Page      int                     `json:"page"`
	OCRTexts  []OCRInputText          `json:"ocr_texts"`
	Header    string                  `json:"header"`
	Body      []translation.Paragraph `json:"body"`
	Footer    string                  `json:"footer"`
	FootNotes string                  `json:"footnotes"`
	Comments  string                  `json:"comments"`
}

// OCRCleaner is responsible for cleaning OCR text using the configured LLM provider.
type OCRCleaner struct {
	client *llm.Client
}

// NewOCRCleaner creates a new instance of OCRCleaner using the configured LLM provider.
func NewOCRCleaner() (*OCRCleaner, error) {
	client, err := llm.NewClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM client: %w", err)
	}
	return &OCRCleaner{client: client}, nil
}

// CleanOCR sends the OCR text to the configured LLM provider for cleaning and formatting.
func (oc *OCRCleaner) CleanOCR(ctx context.Context, request *OCRRequest) (*OCRRequest, error) {
	log.Printf("Sending OCR cleaning request for page %d", request.Page)

	data, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal OCR request: %w", err)
	}

	resp, err := oc.client.Chat(ctx, llm.ChatRequest{
		SystemPrompt: "You are an expert text cleaner. Your task is to clean up OCR text extracted from scanned documents. Remove any artifacts, correct common OCR errors, and format the text into coherent paragraphs. Ensure that the cleaned text maintains the original meaning and context. The OCR text may include headers, footers, and body text. Separate these sections clearly in your output. Any comments or notes should be included in a separate 'comments' field.",
		UserPrompt: `Clean the following OCR text from a scanned document. Please fill in the 'header', 'body', 'footer', and 'footnotes' fields appropriately.
				The 'body' should be an array of paragraphs with each paragraph as an object containing 'id' and 'text'. The 'id' should be a unique identifier for the paragraph, but you can just enter any string there (I'll fix that later). The 'header', 'footer' and 'footnotes' should be plain text string. They be empty if not applicable. If you have any comments about the text, include them in the 'comments' field, which is a plain text string.
				The text is provided in JSON format:\n\n` + string(data),
		JSON: true,
	})
	if err != nil {
		return nil, err
	}

	fmt.Println("LLM response:", resp.Content)

	var ocrResult OCRRequest
	err = llm.ExtractJSON(resp.Content, &ocrResult)
	if err != nil {
		return nil, fmt.Errorf("failed to extract JSON from LLM response: %w", err)
	}
	return &ocrResult, nil
}
