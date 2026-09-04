package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Anthropic разговаривает с /v1/messages. Формат отличается от OpenAI сильнее,
// чем кажется: системная инструкция вынесена из списка реплик, результат
// инструмента приходит репликой пользователя, а расход токенов называется
// иначе. Все различия заканчиваются здесь.
type Anthropic struct {
	BaseURL string
	APIKey  string
	Version string
	HTTP    *http.Client
}

func NewAnthropic(baseURL, apiKey string) *Anthropic {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com/v1"
	}
	return &Anthropic{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		Version: "2023-06-01",
		HTTP:    &http.Client{Timeout: 5 * time.Minute},
	}
}

func (a *Anthropic) Name() string { return "anthropic" }

type anBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
}

type anMessage struct {
	Role    string    `json:"role"`
	Content []anBlock `json:"content"`
}

func (a *Anthropic) body(req Request, stream bool) map[string]any {
	var system string
	messages := make([]anMessage, 0, len(req.Messages))

	for _, m := range req.Messages {
		switch m.Role {
		case RoleSystem:
			// Системная инструкция здесь не реплика, а отдельное поле.
			if system != "" {
				system += "\n\n"
			}
			system += m.Text

		case RoleTool:
			// Ответ инструмента приходит от имени пользователя, а не отдельной
			// ролью: в этом формате роли только две.
			messages = append(messages, anMessage{Role: "user", Content: []anBlock{{
				Type: "tool_result", ToolUseID: m.ToolCallID, Content: m.Text,
			}}})

		case RoleAssistant:
			blocks := []anBlock{}
			if m.Text != "" {
				blocks = append(blocks, anBlock{Type: "text", Text: m.Text})
			}
			for _, tc := range m.ToolCalls {
				args := json.RawMessage(tc.Arguments)
				if len(args) == 0 {
					args = json.RawMessage("{}")
				}
				blocks = append(blocks, anBlock{
					Type: "tool_use", ID: tc.ID, Name: tc.Name, Input: args,
				})
			}
			if len(blocks) > 0 {
				messages = append(messages, anMessage{Role: "assistant", Content: blocks})
			}

		default:
			messages = append(messages, anMessage{Role: "user",
				Content: []anBlock{{Type: "text", Text: m.Text}}})
		}
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		// Поле обязательное, поэтому нужен разумный запас по умолчанию.
		maxTokens = 4096
	}

	body := map[string]any{
		"model":      req.Model,
		"messages":   messages,
		"max_tokens": maxTokens,
	}
	if system != "" {
		body["system"] = system
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{
				"name":         t.Name,
				"description":  t.Description,
				"input_schema": t.Parameters,
			})
		}
		body["tools"] = tools
	}
	if stream {
		body["stream"] = true
	}
	return body
}

func (a *Anthropic) post(ctx context.Context, body map[string]any) (*http.Response, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.BaseURL+"/messages", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", a.APIKey)
	req.Header.Set("anthropic-version", a.Version)

	resp, err := a.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, &APIError{Status: resp.StatusCode, Body: string(detail)}
	}
	return resp, nil
}

func (a *Anthropic) Complete(ctx context.Context, req Request) (*Response, error) {
	started := time.Now()
	resp, err := a.post(ctx, a.body(req, false))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var parsed struct {
		Model      string    `json:"model"`
		Content    []anBlock `json:"content"`
		StopReason string    `json:"stop_reason"`
		Usage      struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}

	out := &Response{
		Model:      parsed.Model,
		FinishedAs: parsed.StopReason,
		Took:       time.Since(started),
		Usage: Usage{
			PromptTokens:     parsed.Usage.InputTokens,
			CompletionTokens: parsed.Usage.OutputTokens,
			CachedTokens:     parsed.Usage.CacheReadInputTokens,
		},
	}
	var text strings.Builder
	for _, block := range parsed.Content {
		switch block.Type {
		case "text":
			text.WriteString(block.Text)
		case "tool_use":
			out.ToolCalls = append(out.ToolCalls, ToolCall{
				ID: block.ID, Name: block.Name, Arguments: string(block.Input),
			})
		}
	}
	out.Text = text.String()
	return out, nil
}

func (a *Anthropic) Stream(ctx context.Context, req Request) (<-chan Chunk, error) {
	started := time.Now()
	resp, err := a.post(ctx, a.body(req, true))
	if err != nil {
		return nil, err
	}

	out := make(chan Chunk)
	go func() {
		defer close(out)
		defer resp.Body.Close()

		var text strings.Builder
		final := &Response{Model: req.Model}
		// Аргументы инструмента приходят кусками JSON по индексу блока.
		pending := map[int]*ToolCall{}

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))

			var event struct {
				Type    string `json:"type"`
				Index   int    `json:"index"`
				Message struct {
					Model string `json:"model"`
					Usage struct {
						InputTokens int `json:"input_tokens"`
					} `json:"usage"`
				} `json:"message"`
				ContentBlock anBlock `json:"content_block"`
				Delta        struct {
					Type        string `json:"type"`
					Text        string `json:"text"`
					PartialJSON string `json:"partial_json"`
					StopReason  string `json:"stop_reason"`
				} `json:"delta"`
				Usage struct {
					OutputTokens int `json:"output_tokens"`
					InputTokens  int `json:"input_tokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal([]byte(payload), &event); err != nil {
				continue
			}

			switch event.Type {
			case "message_start":
				if event.Message.Model != "" {
					final.Model = event.Message.Model
				}
				final.Usage.PromptTokens = event.Message.Usage.InputTokens
			case "content_block_start":
				if event.ContentBlock.Type == "tool_use" {
					pending[event.Index] = &ToolCall{
						ID: event.ContentBlock.ID, Name: event.ContentBlock.Name,
					}
				}
			case "content_block_delta":
				if event.Delta.Text != "" {
					text.WriteString(event.Delta.Text)
					out <- Chunk{Text: event.Delta.Text}
				}
				if event.Delta.PartialJSON != "" {
					if call, ok := pending[event.Index]; ok {
						call.Arguments += event.Delta.PartialJSON
					}
				}
			case "message_delta":
				if event.Delta.StopReason != "" {
					final.FinishedAs = event.Delta.StopReason
				}
				if event.Usage.OutputTokens > 0 {
					final.Usage.CompletionTokens = event.Usage.OutputTokens
				}
			case "message_stop":
				// поток окончен
			}
		}
		if err := scanner.Err(); err != nil {
			out <- Chunk{Err: err, Done: true}
			return
		}

		final.Text = text.String()
		final.Took = time.Since(started)
		for i := 0; i < len(pending)+len(final.ToolCalls); i++ {
			if call, ok := pending[i]; ok {
				if call.Arguments == "" {
					call.Arguments = "{}"
				}
				final.ToolCalls = append(final.ToolCalls, *call)
			}
		}
		out <- Chunk{Done: true, Response: final}
	}()

	return out, nil
}

// Models: у Anthropic список моделей закрыт, поэтому имя задаётся руками.
func (a *Anthropic) Models(ctx context.Context) ([]string, error) {
	return nil, fmt.Errorf("%w: список моделей задаётся вручную", ErrNotSupported)
}
