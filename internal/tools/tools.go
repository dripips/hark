// Package tools исполняет то, что попросила модель: вызов чужого API или
// запрос в подключённую базу.
//
// Всё, что здесь происходит, попадает в чек: запрос, ответ, время и статус.
// Инструмент, работу которого нельзя показать, в Hark не нужен.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dripips/hark/internal/store"
)

// Result — что вернул инструмент. Text уходит модели, остальное в чек.
type Result struct {
	Text     string
	Request  string
	Response string
	Status   string
	Took     time.Duration
	Err      error
}

// Runner исполняет один инструмент.
type Runner interface {
	Run(ctx context.Context, args map[string]any) Result
}

// Build превращает настройку инструмента в исполнителя.
func Build(t *store.Tool) (Runner, error) {
	switch t.Kind {
	case "http":
		return newHTTPRunner(t)
	case "sql":
		return newSQLRunner(t)
	default:
		return nil, fmt.Errorf("неизвестный вид инструмента: %q", t.Kind)
	}
}

// Schema отдаёт схему параметров в виде, который ждёт модель. Битая или
// пустая схема заменяется пустым объектом: лучше инструмент без параметров,
// чем упавший запрос.
func Schema(t *store.Tool) map[string]any {
	schema := map[string]any{}
	if err := json.Unmarshal([]byte(t.Parameters), &schema); err != nil || len(schema) == 0 {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	if _, ok := schema["type"]; !ok {
		schema["type"] = "object"
	}
	return schema
}

// truncate режет по символам: чек и запрос могут быть на любом языке, а
// байтовая обрезка на кириллице врёт вдвое и рубит букву пополам.
func truncate(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "…"
}
