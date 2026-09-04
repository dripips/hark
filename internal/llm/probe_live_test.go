package llm

import (
	"bufio"
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// Живая проба против настоящего поставщика. Запускается только когда ключ задан
// в окружении, поэтому обычный go test её пропускает и не тратит деньги:
//
//	HARK_LIVE_KEY=... HARK_LIVE_MODEL=gpt-5-nano go test ./internal/llm -run Live -v
func TestProbeLive(t *testing.T) {
	key, model := liveEnv(t)

	provider := NewOpenAI(os.Getenv("HARK_LIVE_BASE"), key)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	caps := Probe(ctx, provider, model)

	t.Logf("поток %v, инструменты %v, температура %v, усилие %v, рассуждение отдельно %v",
		caps.Streaming, caps.Tools, caps.Temperature, caps.ReasoningEffort, caps.ReportsReasoning)
	for _, note := range caps.Notes {
		t.Logf("  замечание: %s", note)
	}

	// Без инструментов и потока продукт не работает — это не вкусовщина.
	if !caps.Tools {
		t.Errorf("модель %s не умеет инструменты, бот с ней бесполезен", model)
	}
	if !caps.Streaming {
		t.Errorf("модель %s не отдаёт поток, ответ будет появляться рывком", model)
	}
}

// Проверяем, что цикл с инструментом доходит до конца: модель просит вызов,
// получает результат и отвечает словами.
func TestToolRoundTripLive(t *testing.T) {
	key, model := liveEnv(t)
	provider := NewOpenAI(os.Getenv("HARK_LIVE_BASE"), key)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	tool := Tool{
		Name:        "order_status",
		Description: "Статус заказа по его номеру",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"id": map[string]any{"type": "string"}},
			"required":   []string{"id"},
		},
	}
	messages := []Message{
		{Role: RoleSystem, Text: "Ты помощник магазина. Про заказы спрашивай инструмент."},
		{Role: RoleUser, Text: "Где мой заказ 4127?"},
	}

	first, err := provider.Complete(ctx, Request{
		Model: model, Messages: messages, Tools: []Tool{tool}, MaxTokens: 900,
	})
	if err != nil {
		t.Fatalf("первый запрос: %v", err)
	}
	if len(first.ToolCalls) == 0 {
		t.Fatalf("модель не позвала инструмент, ответила: %q", first.Text)
	}
	call := first.ToolCalls[0]
	if !strings.Contains(call.Arguments, "4127") {
		t.Errorf("номер заказа не доехал до инструмента: %s", call.Arguments)
	}

	messages = append(messages,
		Message{Role: RoleAssistant, ToolCalls: first.ToolCalls},
		Message{Role: RoleTool, ToolCallID: call.ID,
			Text: `{"status":"в пути","eta":"12 сентября"}`})

	second, err := provider.Complete(ctx, Request{
		Model: model, Messages: messages, Tools: []Tool{tool}, MaxTokens: 900,
	})
	if err != nil {
		t.Fatalf("второй запрос: %v", err)
	}
	if second.Text == "" {
		t.Fatal("после результата инструмента модель молчит")
	}
	t.Logf("ответ: %s", second.Text)
	t.Logf("токены: ввод %d, вывод %d, рассуждение %d",
		second.Usage.PromptTokens, second.Usage.CompletionTokens, second.Usage.ReasoningTokens)
}

func liveEnv(t *testing.T) (key, model string) {
	t.Helper()
	key = os.Getenv("HARK_LIVE_KEY")
	model = os.Getenv("HARK_LIVE_MODEL")
	if key == "" {
		if loaded := loadEnvFile(os.Getenv("HARK_LIVE_ENV")); loaded != nil {
			key = loaded["OPENAI_API_KEY"]
			if model == "" {
				model = loaded["OPENAI_MODEL"]
			}
		}
	}
	if key == "" {
		t.Skip("нет HARK_LIVE_KEY — живая проба пропущена")
	}
	if model == "" {
		model = "gpt-5-nano"
	}
	return key, model
}

func loadEnvFile(path string) map[string]string {
	if path == "" {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if found {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return values
}
