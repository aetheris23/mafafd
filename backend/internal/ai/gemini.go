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

// GeminiProvider calls the Google Generative Language API in JSON mode.
type GeminiProvider struct {
	apiKey string
	model  string
	client *http.Client
	log    zerolog.Logger
}

func NewGeminiProvider(apiKey, model string, log zerolog.Logger) *GeminiProvider {
	return &GeminiProvider{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{Timeout: 20 * time.Second},
		log:    log,
	}
}

func (p *GeminiProvider) Name() string { return "gemini" }

type geminiReq struct {
	Contents         []geminiContent `json:"contents"`
	GenerationConfig geminiGen       `json:"generationConfig"`
}

type geminiContent struct {
	Role  string       `json:"role"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiGen struct {
	ResponseMimeType string  `json:"responseMimeType"`
	Temperature      float64 `json:"temperature"`
}

type geminiResp struct {
	Candidates []struct {
		Content geminiContent `json:"content"`
	} `json:"candidates"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (p *GeminiProvider) Analyze(ctx context.Context, in Input) (*Result, error) {
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s",
		p.model, p.apiKey)
	body, _ := json.Marshal(geminiReq{
		Contents:         []geminiContent{{Role: "user", Parts: []geminiPart{{Text: Prompt(in)}}}},
		GenerationConfig: geminiGen{ResponseMimeType: "application/json", Temperature: 0.2},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Goog-Api-Key", p.apiKey)

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		resp, err := p.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("gemini request: %w", err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("gemini status %d: %s", resp.StatusCode, truncateStr(string(raw), 300))
			time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
			continue
		}
		var gr geminiResp
		if err := json.Unmarshal(raw, &gr); err != nil {
			return nil, fmt.Errorf("gemini decode: %w", err)
		}
		if gr.Error != nil {
			return nil, fmt.Errorf("gemini error: %s", gr.Error.Message)
		}
		if len(gr.Candidates) == 0 || len(gr.Candidates[0].Content.Parts) == 0 {
			return nil, fmt.Errorf("gemini empty response")
		}
		text := gr.Candidates[0].Content.Parts[0].Text
		res := &Result{}
		text = strings.TrimSpace(strings.Trim(strings.TrimSpace(text), "`"))
		if err := json.Unmarshal([]byte(text), res); err != nil {
			return nil, fmt.Errorf("gemini JSON decode: %w (raw=%s)", err, truncateStr(text, 200))
		}
		res.Raw = text
		if err := res.validate(); err != nil {
			return nil, err
		}
		return res, nil
	}
	return nil, lastErr
}
