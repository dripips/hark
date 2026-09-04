// Package llm прячет различия между поставщиками моделей.
//
// Hark не привязан к одной модели: один и тот же бот работает на OpenAI,
// на Anthropic и на любом эндпоинте, совместимом с OpenAI, — Ollama, vLLM,
// LM Studio, OpenRouter. Поэтому весь остальной код разговаривает с этим
// интерфейсом, а не с чужим форматом JSON.
package llm

import (
	"context"
	"errors"
	"time"
)

// Role — кто сказал реплику. Значения совпадают с форматом OpenAI, потому что
// он стал общим языком; переводом в формат Anthropic занимается его провайдер.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message — одна реплика в разговоре.
type Message struct {
	Role Role
	Text string

	// Заполняется, когда модель просит вызвать инструмент.
	ToolCalls []ToolCall
	// Заполняется в ответной реплике с результатом инструмента.
	ToolCallID string
}

// ToolCall — просьба модели вызвать инструмент с этими аргументами.
type ToolCall struct {
	ID   string
	Name string
	// Аргументы приходят строкой JSON: разные поставщики шлют разную структуру,
	// а строку одинаково умеют все.
	Arguments string
}

// Tool — описание инструмента для модели.
type Tool struct {
	Name        string
	Description string
	// Схема параметров в формате JSON Schema.
	Parameters map[string]any
}

// Request — то, что уходит модели.
type Request struct {
	Model    string
	Messages []Message
	Tools    []Tool

	// MaxTokens ограничивает ответ. Ноль означает «на усмотрение поставщика».
	MaxTokens int
	// Temperature применяется, только если модель её принимает: думающие
	// модели OpenAI отвергают любое значение кроме единицы, и запрос падает.
	Temperature *float64
	// ReasoningEffort — low, medium, high. Ручка думающих моделей вместо
	// температуры. Пустая строка означает «не передавать».
	ReasoningEffort string
}

// Usage — расход токенов. Reasoning считается отдельно: у думающих моделей он
// не виден в ответе, но оплачивается и объясняет задержку.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	ReasoningTokens  int
	CachedTokens     int
}

func (u Usage) Total() int { return u.PromptTokens + u.CompletionTokens }

// Response — то, что вернула модель.
type Response struct {
	Text       string
	ToolCalls  []ToolCall
	Usage      Usage
	Model      string
	FinishedAs string
	Took       time.Duration
}

// Chunk — кусок потокового ответа.
type Chunk struct {
	Text string
	// Done приходит последним и несёт итоговый расход.
	Done     bool
	Response *Response
	Err      error
}

// Capabilities — что модель на самом деле принимает. Заполняется пробой
// (см. Probe), а не берётся из документации: у самостоятельно поднятых
// моделей поведение расходится с любым списком.
type Capabilities struct {
	Streaming       bool
	Tools           bool
	Temperature     bool
	ReasoningEffort bool
	JSONMode        bool
	// ReportsReasoning означает, что поставщик отдаёт токены рассуждения
	// отдельным числом, и учёт стоимости может им пользоваться.
	ReportsReasoning bool
	CheckedAt        time.Time
	Notes            []string
}

// Provider — поставщик моделей.
type Provider interface {
	// Name возвращает короткое имя вида "openai" или "anthropic".
	Name() string
	// Complete выполняет запрос целиком.
	Complete(ctx context.Context, req Request) (*Response, error)
	// Stream отдаёт ответ кусками. Канал закрывается после Chunk с Done.
	Stream(ctx context.Context, req Request) (<-chan Chunk, error)
	// Models перечисляет доступные модели, если поставщик умеет.
	Models(ctx context.Context) ([]string, error)
}

// ErrNotSupported возвращается, когда поставщик не умеет запрошенного.
var ErrNotSupported = errors.New("поставщик не поддерживает эту возможность")
