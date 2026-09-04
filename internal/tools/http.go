package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dripips/hark/internal/store"
)

// httpRunner дёргает чужой API. Аргументы модели подставляются в адрес и в
// тело по именам в фигурных скобках: /orders/{id}.
type httpRunner struct {
	tool   *store.Tool
	client *http.Client
}

func newHTTPRunner(t *store.Tool) (Runner, error) {
	if strings.TrimSpace(t.URL) == "" {
		return nil, fmt.Errorf("инструмент %q: не задан адрес", t.Name)
	}
	timeout := time.Duration(t.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &httpRunner{tool: t, client: &http.Client{Timeout: timeout}}, nil
}

func (h *httpRunner) Run(ctx context.Context, args map[string]any) Result {
	started := time.Now()
	tool := h.tool

	target := substitute(tool.URL, args, true)
	body := substitute(tool.BodyTemplate, args, false)

	method := strings.ToUpper(strings.TrimSpace(tool.Method))
	if method == "" {
		method = http.MethodGet
	}

	// У GET оставшиеся аргументы уходят в строку запроса: так инструмент
	// описывается одним адресом без шаблона на каждый параметр.
	if method == http.MethodGet && len(args) > 0 {
		parsed, err := url.Parse(target)
		if err == nil {
			query := parsed.Query()
			for key, value := range args {
				if strings.Contains(tool.URL, "{"+key+"}") {
					continue
				}
				query.Set(key, fmt.Sprint(value))
			}
			parsed.RawQuery = query.Encode()
			target = parsed.String()
		}
	}

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return Result{Err: err, Request: target, Took: time.Since(started)}
	}

	headers := map[string]string{}
	_ = json.Unmarshal([]byte(tool.Headers), &headers)
	for key, value := range headers {
		request.Header.Set(key, substitute(value, args, false))
	}
	if body != "" && request.Header.Get("Content-Type") == "" {
		request.Header.Set("Content-Type", "application/json")
	}

	shown := method + " " + target
	if body != "" {
		shown += "\n" + truncate(body, 800)
	}

	response, err := h.client.Do(request)
	if err != nil {
		return Result{Err: err, Request: shown, Took: time.Since(started)}
	}
	defer response.Body.Close()

	// Ответ режется: модели не нужен мегабайт, а чеку тем более.
	raw, err := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	if err != nil {
		return Result{Err: err, Request: shown, Took: time.Since(started)}
	}
	text := strings.TrimSpace(string(raw))

	result := Result{
		Text:     truncate(text, 8000),
		Request:  shown,
		Response: truncate(text, 4000),
		Status:   fmt.Sprintf("%d", response.StatusCode),
		Took:     time.Since(started),
	}
	if response.StatusCode >= 400 {
		// Ошибку не прячем: модель должна знать, что API отказал, иначе она
		// придумает ответ. Это прямо тот случай, ради которого нужен чек.
		result.Text = fmt.Sprintf("Сервис ответил %d. Тело: %s",
			response.StatusCode, truncate(text, 600))
	}
	return result
}

// substitute подставляет аргументы в шаблон. Для адреса значения экранируются,
// для тела и заголовков — нет: там уже свой формат.
func substitute(template string, args map[string]any, escape bool) string {
	if template == "" {
		return ""
	}
	out := template
	for key, value := range args {
		text := fmt.Sprint(value)
		if escape {
			text = url.PathEscape(text)
		}
		out = strings.ReplaceAll(out, "{"+key+"}", text)
	}
	return out
}
