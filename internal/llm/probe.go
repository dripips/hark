package llm

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Probe выясняет, что модель принимает на самом деле.
//
// Документация тут помогает слабо: у одного поставщика думающая модель
// отвергает temperature, у другого её же требует, а самостоятельно поднятая
// модель ведёт себя как ей угодно. Дешевле спросить: несколько крошечных
// запросов дают точную картину, и настройки показывают только те ручки,
// которые не сломают разговор.
//
// Пробы стоят денег. У думающих моделей даже «скажи привет» тратит сотни
// токенов рассуждения, поэтому Probe вызывается по кнопке, а не при каждом
// сохранении настроек.
func Probe(ctx context.Context, p Provider, model string) Capabilities {
	caps := Capabilities{CheckedAt: time.Now().UTC()}
	hi := []Message{{Role: RoleUser, Text: "Reply with the single word: ok"}}

	base := Request{Model: model, Messages: hi, MaxTokens: 600}

	// 1. Базовый вызов. Заодно ловим спор про имя поля с ограничением длины.
	resp, err := p.Complete(ctx, base)
	if err != nil {
		if oa, ok := p.(*OpenAI); ok && mentions(err, "max_tokens") {
			// Поставщик просит старое имя поля. Переключаемся и пробуем снова.
			oa.MaxTokensField = "max_tokens"
			caps.Notes = append(caps.Notes, "ограничение длины передаётся полем max_tokens")
			resp, err = p.Complete(ctx, base)
		}
		if err != nil {
			caps.Notes = append(caps.Notes, "базовый запрос не прошёл: "+err.Error())
			return caps
		}
	}
	if resp.Usage.ReasoningTokens > 0 {
		caps.ReportsReasoning = true
		caps.Notes = append(caps.Notes,
			"модель думающая: рассуждение не видно в ответе, но оплачивается")
	}

	// 2. Температура. Думающие модели OpenAI принимают только значение по
	//    умолчанию и роняют запрос на любом другом.
	half := 0.5
	if _, err := p.Complete(ctx, Request{
		Model: model, Messages: hi, MaxTokens: 600, Temperature: &half,
	}); err == nil {
		caps.Temperature = true
	} else {
		caps.Notes = append(caps.Notes, "температура не поддерживается: "+short(err))
	}

	// 3. Ручка усилия рассуждения — замена температуре у думающих моделей.
	if _, err := p.Complete(ctx, Request{
		Model: model, Messages: hi, MaxTokens: 600, ReasoningEffort: "low",
	}); err == nil {
		caps.ReasoningEffort = true
	}

	// 4. Инструменты: без них половина продукта не работает, проверяем всерьёз.
	probeTool := Tool{
		Name:        "hark_probe",
		Description: "Возвращает состояние проверки",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"value": map[string]any{"type": "string"}},
			"required":   []string{"value"},
		},
	}
	if _, err := p.Complete(ctx, Request{
		Model:     model,
		Messages:  []Message{{Role: RoleUser, Text: "Call hark_probe with value \"x\"."}},
		Tools:     []Tool{probeTool},
		MaxTokens: 800,
	}); err == nil {
		caps.Tools = true
	} else {
		caps.Notes = append(caps.Notes, "инструменты не поддерживаются: "+short(err))
	}

	// 5. Поток. Проверяем, что приходит хотя бы один кусок текста.
	if stream, err := p.Stream(ctx, base); err == nil {
		for chunk := range stream {
			if chunk.Text != "" {
				caps.Streaming = true
			}
			if chunk.Done && chunk.Response != nil && chunk.Response.Text != "" {
				caps.Streaming = true
			}
		}
	}

	return caps
}

func mentions(err error, needle string) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return strings.Contains(strings.ToLower(apiErr.Body), strings.ToLower(needle))
	}
	return strings.Contains(strings.ToLower(err.Error()), strings.ToLower(needle))
}

func short(err error) string {
	text := err.Error()
	if len(text) > 140 {
		return text[:140] + "…"
	}
	return text
}
