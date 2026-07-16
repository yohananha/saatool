// Package llm provides a single, provider-agnostic chat client that talks
// to OpenRouter (https://openrouter.ai), which fronts DeepSeek, Anthropic,
// OpenAI and other LLM providers behind one OpenAI-compatible REST API.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/dtylman/saatool/config"
)

const openRouterURL = "https://openrouter.ai/api/v1/chat/completions"

// ChatRequest is a single system+user turn. When JSON is true, the model is
// asked to respond with a single JSON object.
type ChatRequest struct {
	SystemPrompt string
	UserPrompt   string
	JSON         bool
}

// ChatResponse is the assistant's reply.
type ChatResponse struct {
	Content string
}

// Client talks to OpenRouter using the provider and model type configured
// in config.Options.
type Client struct {
	apiKey    string
	model     string
	reasoning bool
	http      *http.Client
}

// NewClient builds a Client using config.Options.OpenRouterAPIKey,
// config.Options.Provider and config.Options.ModelType.
func NewClient() (*Client, error) {
	if config.Options.OpenRouterAPIKey == "" {
		return nil, errors.New("OpenRouter API key is not set")
	}
	model, reasoning := resolveModel(config.Options.Provider, config.Options.ModelType)
	return &Client{
		apiKey:    config.Options.OpenRouterAPIKey,
		model:     model,
		reasoning: reasoning,
		http:      &http.Client{},
	}, nil
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type reasoningConfig struct {
	Effort string `json:"effort"`
}

type chatCompletionRequest struct {
	Model          string           `json:"model"`
	Messages       []chatMessage    `json:"messages"`
	ResponseFormat *responseFormat  `json:"response_format,omitempty"`
	Reasoning      *reasoningConfig `json:"reasoning,omitempty"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Chat sends a system+user prompt to the configured model and returns the
// assistant's reply.
func (c *Client) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	body := chatCompletionRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: req.SystemPrompt},
			{Role: "user", Content: req.UserPrompt},
		},
	}
	if req.JSON {
		body.ResponseFormat = &responseFormat{Type: "json_object"}
	}
	if c.reasoning {
		body.Reasoning = &reasoningConfig{Effort: "medium"}
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal chat request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, openRouterURL, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request to OpenRouter: %w", err)
	}
	defer httpResp.Body.Close()

	respData, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read OpenRouter response: %w", err)
	}

	var resp chatCompletionResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse OpenRouter response: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("OpenRouter error: %s", resp.Error.Message)
	}
	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OpenRouter request failed with status %d: %s", httpResp.StatusCode, string(respData))
	}
	if len(resp.Choices) == 0 {
		return nil, errors.New("no choices returned from OpenRouter")
	}

	return &ChatResponse{Content: resp.Choices[0].Message.Content}, nil
}
