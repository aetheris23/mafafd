package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// OpenAIProvider calls any OpenAI-compatible chat completions endpoint.
type OpenAIProvider struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
	log     zerolog.Logger
}

func NewOpenAIProvider(apiKey, model, baseURL string, log zerolog.Logger) *OpenAIProvider {
	return &OpenAIProvider{
		apiKey:  apiKey,
		model:   model,
		baseURL: strings.TrimSuffix(baseURL, "/"),
		client:  &http.Client{Timeout: 20 * time.Second},
		log:     log,
	}
}

func (p *OpenAIProvider) Name() string { return "openai" }

type openaiReq struct {
	Model          string          `json:"model"`
	Messages       []openaiMessage `json:"messages"`
	ResponseFormat openaiFormat    `json:"response_format"`
	Temperature    float64         `json:"temperature"`
}

type openaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openaiFormat struct {
	Type string `json:"type"`
}

type openaiResp struct {
	Choices []struct {
		Message openaiMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (p *OpenAIProvider) Analyze(ctx context.Context, in Input) (*Result, error) {
	url := fmt.Sprintf("%s/chat/completions", p.baseURL)
	body, _ := json.Marshal(openaiReq{
		Model: p.model,
		Messages: []openaiMessage{
			{Role: "system", Content: "You are a fraud analyst. Respond only with JSON."},
			{Role: "user", Content: Prompt(in)},
		},
		ResponseFormat: openaiFormat{Type: "json_object"},
		Temperature:    0.2,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		resp, err := p.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("openai request: %w", err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("openai status %d: %s", resp.StatusCode, truncateStr(string(raw), 300))
			time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
			continue
		}
		var gr openaiResp
		if err := json.Unmarshal(raw, &gr); err != nil {
			return nil, fmt.Errorf("openai decode: %w", err)
		}
		if gr.Error != nil {
			return nil, fmt.Errorf("openai error: %s", gr.Error.Message)
		}
		if len(gr.Choices) == 0 {
			return nil, fmt.Errorf("openai empty choices")
		}
		text := gr.Choices[0].Message.Content
		res := &Result{}
		text = strings.TrimSpace(strings.Trim(strings.TrimSpace(text), "`"))
		if err := json.Unmarshal([]byte(text), res); err != nil {
			return nil, fmt.Errorf("openai JSON decode: %w (raw=%s)", err, truncateStr(text, 200))
		}
		res.Raw = text
		if err := res.validate(); err != nil {
			return nil, err
		}
		return res, nil
	}
	return nil, lastErr
}
