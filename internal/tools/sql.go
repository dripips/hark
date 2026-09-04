package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/dripips/hark/internal/store"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

// sqlRunner пускает модель в подключённую базу — и только на чтение.
//
// Ограничители стоят слоями, потому что один всегда можно обойти:
//
//  1. разрешён только SELECT и WITH … SELECT;
//  2. запрещено несколько запросов в одной строке;
//  3. запрещены слова, меняющие данные или лезущие в файлы;
//  4. таблицы сверяются с белым списком;
//  5. число строк ограничено обёрткой, а не доверием к модели;
//  6. время выполнения ограничено контекстом.
//
// Это защита в глубину, а не замена правам доступа: подключать базу нужно
// пользователем, у которого физически нет права на запись. Так и написано
// в подсказке к полю в админке.
type sqlRunner struct {
	tool    *store.Tool
	allowed map[string]bool
}

func newSQLRunner(t *store.Tool) (Runner, error) {
	if strings.TrimSpace(t.DSN) == "" {
		return nil, fmt.Errorf("инструмент %q: не задано подключение", t.Name)
	}
	allowed := map[string]bool{}
	for _, name := range strings.FieldsFunc(t.AllowedTables, func(r rune) bool {
		return r == ',' || r == '\n' || r == ' '
	}) {
		if name = strings.ToLower(strings.TrimSpace(name)); name != "" {
			allowed[name] = true
		}
	}
	if len(allowed) == 0 {
		return nil, fmt.Errorf("инструмент %q: пустой список таблиц, а без него нельзя", t.Name)
	}
	return &sqlRunner{tool: t, allowed: allowed}, nil
}

var (
	// Запрос обязан начинаться с чтения.
	reSelect = regexp.MustCompile(`(?is)^\s*(select|with)\b`)
	// Таблицы после FROM и JOIN, включая подзапросы.
	reTables = regexp.MustCompile(`(?is)\b(?:from|join)\s+([a-z_][a-z0-9_]*)`)
	// Слова, которых в читающем запросе быть не может.
	reForbidden = regexp.MustCompile(`(?is)\b(insert|update|delete|drop|alter|create|replace|truncate|grant|revoke|attach|detach|pragma|vacuum|copy|load_file|outfile|dumpfile|into\s+outfile)\b`)
	// Точка с запятой, за которой ещё что-то есть, — это второй запрос.
	reMultiple = regexp.MustCompile(`;\s*\S`)
	reHasLimit = regexp.MustCompile(`(?is)\blimit\s+\d+\s*$`)
	// Имена из WITH … AS ( … ): это временные запросы, а не таблицы, и в
	// белом списке им делать нечего.
	reCTE = regexp.MustCompile(`(?is)\b([a-z_][a-z0-9_]*)\s+as\s*\(`)
)

// Guard проверяет запрос до подключения к базе. Вынесен отдельно, чтобы его
// можно было проверить тестами без живой базы.
func (s *sqlRunner) Guard(query string) error {
	trimmed := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(query), ";"))
	if trimmed == "" {
		return fmt.Errorf("пустой запрос")
	}
	if reMultiple.MatchString(trimmed) {
		return fmt.Errorf("несколько запросов сразу запрещены")
	}
	if !reSelect.MatchString(trimmed) {
		return fmt.Errorf("разрешён только SELECT")
	}
	if match := reForbidden.FindString(trimmed); match != "" {
		return fmt.Errorf("запрещённое слово %q: инструмент работает только на чтение", match)
	}
	// Сначала собираем имена временных запросов, иначе WITH recent AS (…)
	// упрётся в белый список на собственном же имени.
	cte := map[string]bool{}
	for _, found := range reCTE.FindAllStringSubmatch(trimmed, -1) {
		cte[strings.ToLower(found[1])] = true
	}
	for _, found := range reTables.FindAllStringSubmatch(trimmed, -1) {
		table := strings.ToLower(found[1])
		if cte[table] {
			continue
		}
		if !s.allowed[table] {
			return fmt.Errorf("таблица %q не в списке разрешённых", table)
		}
	}
	return nil
}

func (s *sqlRunner) Run(ctx context.Context, args map[string]any) Result {
	started := time.Now()

	raw, _ := args["query"].(string)
	query := strings.TrimSpace(raw)

	if err := s.Guard(query); err != nil {
		// Отказ уходит и модели, и в чек: пусть переформулирует, а владелец
		// видит, что бот пытался прочитать лишнее.
		return Result{
			Text:     "Запрос отклонён: " + err.Error(),
			Request:  query,
			Status:   "отклонён",
			Response: err.Error(),
			Took:     time.Since(started),
		}
	}

	limit := s.tool.RowLimit
	if limit <= 0 {
		limit = 50
	}
	bounded := strings.TrimSuffix(strings.TrimSpace(query), ";")
	if !reHasLimit.MatchString(bounded) {
		// Обёртка надёжнее приписки: она ограничивает и запрос с UNION.
		bounded = fmt.Sprintf("SELECT * FROM (%s) AS hark_limited LIMIT %d", bounded, limit)
	}

	timeout := time.Duration(s.tool.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	driver := s.tool.Driver
	if driver == "postgres" {
		driver = "pgx"
	}
	if driver == "" {
		driver = "sqlite"
	}

	db, err := sql.Open(driver, s.tool.DSN)
	if err != nil {
		return Result{Err: err, Request: bounded, Took: time.Since(started)}
	}
	defer db.Close()
	db.SetMaxOpenConns(2)

	rows, err := db.QueryContext(runCtx, bounded)
	if err != nil {
		return Result{
			Text:    "База ответила ошибкой: " + err.Error(),
			Request: bounded, Status: "ошибка", Response: err.Error(),
			Took: time.Since(started),
		}
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return Result{Err: err, Request: bounded, Took: time.Since(started)}
	}

	var out []map[string]any
	for rows.Next() {
		cells := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range cells {
			pointers[i] = &cells[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return Result{Err: err, Request: bounded, Took: time.Since(started)}
		}
		row := map[string]any{}
		for i, name := range columns {
			if raw, ok := cells[i].([]byte); ok {
				row[name] = string(raw)
			} else {
				row[name] = cells[i]
			}
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return Result{Err: err, Request: bounded, Took: time.Since(started)}
	}

	encoded, _ := json.Marshal(out)
	return Result{
		Text:     truncate(string(encoded), 8000),
		Request:  bounded,
		Response: truncate(string(encoded), 4000),
		Status:   fmt.Sprintf("%d строк", len(out)),
		Took:     time.Since(started),
	}
}

// QuerySchema — схема параметров для SQL-инструмента. Модель пишет запрос
// сама, поэтому в описании инструмента надо перечислить таблицы и колонки.
func QuerySchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "SQL-запрос, только SELECT",
			},
		},
		"required": []string{"query"},
	}
}
