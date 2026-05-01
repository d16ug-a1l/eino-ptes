package master

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// OpenAIConfig configures the lightweight OpenAI-compatible client.
type OpenAIConfig struct {
	APIKey    string
	BaseURL   string // e.g. "https://api.openai.com/v1" or "https://ark.cn-beijing.volces.com/api/v3"
	Model     string // e.g. "gpt-4o", "doubao-pro-32k", "llama3"
	Timeout   time.Duration
}

// OpenAIChatModel is a minimal OpenAI-compatible client implementing model.BaseChatModel.
// It works with OpenAI, Ark (Volcano Engine), Ollama, vLLM, and any other OpenAI-compatible endpoint.
type OpenAIChatModel struct {
	apiKey  string
	baseURL string
	model   string
	client  *http.Client
}

// NewOpenAIChatModel creates a new lightweight OpenAI-compatible chat model.
func NewOpenAIChatModel(cfg *OpenAIConfig) *OpenAIChatModel {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com/v1"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 60 * time.Second
	}
	return &OpenAIChatModel{
		apiKey:  cfg.APIKey,
		baseURL: cfg.BaseURL,
		model:   cfg.Model,
		client:  &http.Client{Timeout: cfg.Timeout},
	}
}

// Generate implements model.BaseChatModel.
func (m *OpenAIChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	reqBody, err := m.buildRequest(input)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", m.baseURL+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+m.apiKey)

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, string(body))
	}

	var result chatCompletionResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	choice := result.Choices[0]
	msg := &schema.Message{
		Role:    schema.Assistant,
		Content: choice.Message.Content,
	}
	return msg, nil
}

// Stream implements model.BaseChatModel. Currently returns an error; streaming can be added later if needed.
func (m *OpenAIChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, fmt.Errorf("streaming not implemented")
}

func (m *OpenAIChatModel) buildRequest(messages []*schema.Message) ([]byte, error) {
	var msgs []map[string]string
	for _, m := range messages {
		role := string(m.Role)
		if role == "" {
			role = "user"
		}
		msgs = append(msgs, map[string]string{
			"role":    role,
			"content": m.Content,
		})
	}

	req := map[string]interface{}{
		"model":    m.model,
		"messages": msgs,
	}

	return json.Marshal(req)
}

type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}
