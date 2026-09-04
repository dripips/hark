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

// OpenAI разговаривает с любым эндпоинтом формата /v1/chat/completions.
// Этим форматом говорят и сам OpenAI, и Ollama, и vLLM, и LM Studio, и
// OpenRouter, поэтому одна реализация покрывает почти весь рынок.
type OpenAI struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client

	// MaxTokensField различается по поколениям: думающие модели OpenAI
	// принимают только max_completion_tokens и отвергают max_tokens, а
	// большинство совместимых серверов понимает лишь старое имя.
	// Пустое значение означает «выяснить пробой».
	MaxTokensField string
}

func NewOpenAI(baseURL, apiKey string) *OpenAI {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	return &OpenAI{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		HTTP:    &http.Client{Timeout: 5 * time.Minute},
	}
}

func (o *OpenAI) Name() string { return "openai" }

type oaMessage struct {
	Role       string       `json:"role"`
	Content    string       `json:"content,omitempty"`
	ToolCalls  []oaToolCall `json:"tool_calls,omitempty"`
	ToolCallID string       `json:"tool_call_id,omitempty"`
}

type oaToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Index    int    `json:"index,omitempty"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type oaUsage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	TotalTokens         int `json:"total_tokens"`
	PromptTokensDetails struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

func (o *OpenAI) body(req Request, stream bool) map[string]any {
	messages := make([]oaMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		om := oaMessage{Role: string(m.Role), Content: m.Text, ToolCallID: m.ToolCallID}
		for _, tc := range m.ToolCalls {
			var call oaToolCall
			call.ID = tc.ID
			call.Type = "function"
			call.Function.Name = tc.Name
			call.Function.Arguments = tc.Arguments
			om.ToolCalls = append(om.ToolCalls, call)
		}
		messages = append(messages, om)
	}

	body := map[string]any{"model": req.Model, "messages": messages}

	if req.MaxTokens > 0 {
		field := o.MaxTokensField
		if field == "" {
			field = "max_completion_tokens"
		}
		body[field] = req.MaxTokens
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if req.ReasoningEffort != "" {
		body["reasoning_effort"] = req.ReasoningEffort
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        t.Name,
					"description": t.Description,
					"parameters":  t.Parameters,
				},
			})
		}
		body["tools"] = tools
	}
	if stream {
		body["stream"] = true
		// Без этого расход токенов в потоке не приходит вовсе, и чек остаётся
		// без цены.
		body["stream_options"] = map[string]any{"include_usage": true}
	}
	return body
}

func (o *OpenAI) post(ctx context.Context, body map[string]any) (*http.Response, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		o.BaseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if o.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.APIKey)
	}
	resp, err := o.HTTP.Do(req)
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

// APIError несёт ответ поставщика целиком: в чеке важно показать не «ошибка
// 400», а что именно поставщик отказался принимать.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	message := e.Body
	var parsed struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(e.Body), &parsed) == nil && parsed.Error.Message != "" {
		message = parsed.Error.Message
	}
	return fmt.Sprintf("%d: %s", e.Status, message)
}

func (o *OpenAI) Complete(ctx context.Context, req Request) (*Response, error) {
	started := time.Now()
	resp, err := o.post(ctx, o.body(req, false))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var parsed struct {
		Model   string `json:"model"`
		Choices []struct {
			Message      oaMessage `json:"message"`
			FinishReason string    `json:"finish_reason"`
		} `json:"choices"`
		Usage oaUsage `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("поставщик вернул ответ без вариантов")
	}

	choice := parsed.Choices[0]
	out := &Response{
		Text:       choice.Message.Content,
		Model:      parsed.Model,
		FinishedAs: choice.FinishReason,
		Took:       time.Since(started),
		Usage: Usage{
			PromptTokens:     parsed.Usage.PromptTokens,
			CompletionTokens: parsed.Usage.CompletionTokens,
			ReasoningTokens:  parsed.Usage.CompletionTokensDetails.ReasoningTokens,
			CachedTokens:     parsed.Usage.PromptTokensDetails.CachedTokens,
		},
	}
	for _, tc := range choice.Message.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, ToolCall{
			ID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments,
		})
	}
	return out, nil
}

func (o *OpenAI) Stream(ctx context.Context, req Request) (<-chan Chunk, error) {
	started := time.Now()
	resp, err := o.post(ctx, o.body(req, true))
	if err != nil {
		return nil, err
	}

	out := make(chan Chunk)
	go func() {
		defer close(out)
		defer resp.Body.Close()

		var text strings.Builder
		// Куски вызова инструмента приходят вразбивку и склеиваются по индексу.
		calls := map[int]*ToolCall{}
		final := &Response{Model: req.Model}

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "[DONE]" {
				break
			}

			var event struct {
				Model   string `json:"model"`
				Choices []struct {
					Delta struct {
						Content   string       `json:"content"`
						ToolCalls []oaToolCall `json:"tool_calls"`
					} `json:"delta"`
					FinishReason string `json:"finish_reason"`
				} `json:"choices"`
				Usage *oaUsage `json:"usage"`
			}
			if err := json.Unmarshal([]byte(payload), &event); err != nil {
				continue
			}
			if event.Model != "" {
				final.Model = event.Model
			}
			if event.Usage != nil {
				final.Usage = Usage{
					PromptTokens:     event.Usage.PromptTokens,
					CompletionTokens: event.Usage.CompletionTokens,
					ReasoningTokens:  event.Usage.CompletionTokensDetails.ReasoningTokens,
					CachedTokens:     event.Usage.PromptTokensDetails.CachedTokens,
				}
			}
			for _, choice := range event.Choices {
				if choice.FinishReason != "" {
					final.FinishedAs = choice.FinishReason
				}
				if choice.Delta.Content != "" {
					text.WriteString(choice.Delta.Content)
					out <- Chunk{Text: choice.Delta.Content}
				}
				for _, tc := range choice.Delta.ToolCalls {
					call, ok := calls[tc.Index]
					if !ok {
						call = &ToolCall{}
						calls[tc.Index] = call
					}
					if tc.ID != "" {
						call.ID = tc.ID
					}
					if tc.Function.Name != "" {
						call.Name = tc.Function.Name
					}
					call.Arguments += tc.Function.Arguments
				}
			}
		}
		if err := scanner.Err(); err != nil {
			out <- Chunk{Err: err, Done: true}
			return
		}

		final.Text = text.String()
		final.Took = time.Since(started)
		for i := 0; i < len(calls); i++ {
			if call, ok := calls[i]; ok {
				final.ToolCalls = append(final.ToolCalls, *call)
			}
		}
		out <- Chunk{Done: true, Response: final}
	}()

	return out, nil
}

func (o *OpenAI) Models(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.BaseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	if o.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.APIKey)
	}
	resp, err := o.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, ErrNotSupported
	}
	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(parsed.Data))
	for _, m := range parsed.Data {
		names = append(names, m.ID)
	}
	return names, nil
}
