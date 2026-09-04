// Пакет notify зовёт человека наружу, когда бот сдался.
//
// Внутри Hark нет ни почты, ни Телеграма, ни очередей — и не будет: это не
// SaaS, и обязательных внешних служб у него быть не должно. Вместо этого
// владелец вписывает один адрес, и Телеграм получается тем, что он вставляет
// api.telegram.org/bot<токен>/sendMessage, а почта — тем, что он ставит рядом
// свой мост.
//
// Живая вкладка админки узнаёт об эскалации быстрее и без настройки. Этот
// путь нужен ровно для одного случая: админка закрыта, а посетитель ждёт.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Event — то, что случилось. Переписки здесь нет никогда: наружу уходит факт
// и ссылка, а читать разговор нужно в админке.
type Event struct {
	BotID    int64
	BotName  string
	BotSlug  string
	Reason   string
	PageURL  string
	AdminURL string
	ConvID   int64
	Waiting  int
	At       time.Time
	Test     bool
}

// Target — куда и как звонить. Пустой адрес означает «выключено»: отдельного
// флага нет, чтобы два состояния не разъезжались.
type Target struct {
	URL      string `json:"url"`
	Method   string `json:"method"`
	Headers  string `json:"headers"`
	Template string `json:"template"`
	// Quiet убирает причину из тела. Причину пишет модель, и она может
	// пересказать слова посетителя — наружу это уходить не обязано.
	Quiet bool `json:"quiet"`
}

// ParseTarget читает настройки, сохранённые одной колонкой JSON. Пустая
// строка и мусор дают выключенный зов, а не ошибку: настройка внешнего
// оповещения не должна ронять страницу бота.
func ParseTarget(raw string) Target {
	var target Target
	if strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &target)
	}
	if target.Method != http.MethodGet {
		target.Method = http.MethodPost
	}
	return target
}

// Enabled — задан ли адрес. Пустой адрес и есть «выключено».
func (t Target) Enabled() bool { return strings.TrimSpace(t.URL) != "" }

// minInterval — не чаще одного зова на бота. Когда ложится поставщик модели,
// эскалирует каждый разговор подряд, и без этого получился бы шторм.
const minInterval = 10 * time.Second

// Sender шлёт зовы из одной фоновой горутины.
//
// Синхронно звонить нельзя: посетитель ждал бы чужой сервер. Плодить горутину
// на каждый зов тоже нельзя: при недоступном адресе их станет столько,
// сколько эскалаций.
type Sender struct {
	client *http.Client
	queue  chan task
	done   chan struct{}
	once   sync.Once

	mu   sync.Mutex
	last map[int64]time.Time

	// Note записывает исход зова, чтобы владелец увидел его в настройках бота.
	Note func(botID int64, status string)
}

type task struct {
	target Target
	event  Event
}

func New() *Sender {
	return &Sender{
		client: &http.Client{
			Timeout: 10 * time.Second,
			// Переход по редиректу увёл бы заголовок Authorization и токен из
			// адреса на чужой хост. На это владелец не соглашался.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return errors.New("переходы запрещены")
			},
		},
		queue: make(chan task, 32),
		done:  make(chan struct{}),
		last:  map[int64]time.Time{},
	}
}

// Start поднимает единственную рабочую горутину.
func (s *Sender) Start() {
	go func() {
		for job := range s.queue {
			status := s.deliver(job.target, job.event)
			if s.Note != nil {
				s.Note(job.event.BotID, status)
			}
		}
		close(s.done)
	}()
}

// Close даёт последнему зову уйти и не ждёт дольше пяти секунд.
func (s *Sender) Close() {
	s.once.Do(func() { close(s.queue) })
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
	}
}

// Fire кладёт зов в очередь и сразу возвращается. Ответ посетителю не имеет
// права ждать чужой сервер.
func (s *Sender) Fire(target Target, event Event) {
	if strings.TrimSpace(target.URL) == "" {
		return
	}
	if !s.allow(event.BotID) {
		return
	}

	select {
	case s.queue <- task{target: target, event: event}:
	default:
		// Очередь забита — значит, адресат не отвечает уже давно. Разговор при
		// этом в очереди к человеку и никуда не денется.
		log.Printf("зов пропущен, очередь полна: бот %d", event.BotID)
	}
}

func (s *Sender) allow(botID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if was, ok := s.last[botID]; ok && now.Sub(was) < minInterval {
		return false
	}
	s.last[botID] = now
	return true
}

// Send звонит прямо сейчас и рассказывает, что вышло. Нужен кнопке проверки:
// человек нажал и должен увидеть исход, а не строчку в журнале.
func (s *Sender) Send(target Target, event Event) string {
	return s.deliver(target, event)
}

// deliver делает две попытки. Адрес, не ответивший дважды с промежутком в
// пять секунд, лежит, и десять повторов этого не изменят.
func (s *Sender) deliver(target Target, event Event) string {
	var last string
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			time.Sleep(5 * time.Second)
		}
		status, err := s.attempt(target, event)
		if err == nil {
			return status
		}
		last = status
	}
	return last
}

func (s *Sender) attempt(target Target, event Event) (string, error) {
	address, body, err := build(target, event)
	if err != nil {
		return "не смогли собрать запрос: " + err.Error(), err
	}

	method := strings.ToUpper(strings.TrimSpace(target.Method))
	if method == "" {
		method = http.MethodPost
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var reader io.Reader
	if method != http.MethodGet && method != http.MethodHead {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, address, reader)
	if err != nil {
		return "плохой адрес: " + err.Error(), err
	}
	if reader != nil {
		request.Header.Set("Content-Type", contentType(target.Template))
	}
	for name, value := range parseHeaders(target.Headers) {
		request.Header.Set(name, value)
	}

	response, err := s.client.Do(request)
	if err != nil {
		return "не дозвонились: " + trimError(err), err
	}
	defer response.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(response.Body, 400))

	if response.StatusCode >= 300 {
		text := fmt.Sprintf("ответил %d: %s", response.StatusCode, strings.TrimSpace(string(answer)))
		return text, errors.New(text)
	}
	return fmt.Sprintf("доставлено, ответ %d", response.StatusCode), nil
}

// build подставляет значения в адрес и в тело.
//
// Экранирование выбирается по месту: в адресе — процентное, в теле JSON —
// строковое. Иначе причина с кавычкой ломает тело, а причина с амперсандом
// дописывает лишний параметр в адрес.
func build(target Target, event Event) (string, []byte, error) {
	values := placeholders(event, target.Quiet)

	address := substitute(target.URL, values, escapeQuery)
	if _, err := url.Parse(address); err != nil {
		return "", nil, err
	}
	if !strings.HasPrefix(address, "http://") && !strings.HasPrefix(address, "https://") {
		return "", nil, errors.New("адрес должен начинаться с http:// или https://")
	}

	template := strings.TrimSpace(target.Template)
	if template == "" {
		body, err := json.Marshal(standardBody(event, target.Quiet))
		return address, body, err
	}
	if looksLikeJSON(template) {
		return address, []byte(substitute(template, values, escapeJSON)), nil
	}
	return address, []byte(substitute(template, values, func(s string) string { return s })), nil
}

func placeholders(event Event, quiet bool) map[string]string {
	reason := event.Reason
	if quiet {
		reason = ""
	}
	return map[string]string{
		"bot":     event.BotName,
		"slug":    event.BotSlug,
		"reason":  reason,
		"id":      strconv.FormatInt(event.ConvID, 10),
		"url":     event.AdminURL,
		"page":    event.PageURL,
		"waiting": strconv.Itoa(event.Waiting),
		"text":    plainText(event, quiet),
	}
}

// plainText — готовая строка по-русски. С ней Телеграм и любой мост покажут
// осмысленное сообщение вообще без шаблона.
func plainText(event Event, quiet bool) string {
	var b strings.Builder
	if event.Test {
		b.WriteString("Проверка связи. ")
	}
	fmt.Fprintf(&b, "Бот «%s» зовёт человека", event.BotName)
	if !quiet && event.Reason != "" {
		fmt.Fprintf(&b, ": %s", event.Reason)
	}
	b.WriteString(".")
	if event.Waiting > 0 {
		fmt.Fprintf(&b, " Ждут: %d.", event.Waiting)
	}
	if event.AdminURL != "" {
		fmt.Fprintf(&b, " Открыть: %s", event.AdminURL)
	}
	return b.String()
}

func standardBody(event Event, quiet bool) map[string]any {
	kind := "escalated"
	if event.Test {
		kind = "test"
	}
	body := map[string]any{
		"event":           kind,
		"bot":             event.BotName,
		"bot_slug":        event.BotSlug,
		"conversation_id": event.ConvID,
		"url":             event.AdminURL,
		"page":            event.PageURL,
		"waiting":         event.Waiting,
		"at":              event.At.UTC().Format(time.RFC3339),
		"text":            plainText(event, quiet),
	}
	if !quiet {
		body["reason"] = event.Reason
	}
	return body
}

func substitute(source string, values map[string]string, escape func(string) string) string {
	out := source
	for name, value := range values {
		out = strings.ReplaceAll(out, "{"+name+"}", escape(value))
	}
	return out
}

func escapeQuery(s string) string { return url.QueryEscape(s) }

func escapeJSON(s string) string {
	quoted, err := json.Marshal(s)
	if err != nil {
		return ""
	}
	// json.Marshal возвращает значение в кавычках, а подставляем мы внутрь
	// уже написанных кавычек шаблона.
	return string(quoted[1 : len(quoted)-1])
}

func looksLikeJSON(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[")
}

func contentType(template string) string {
	if template == "" || looksLikeJSON(template) {
		return "application/json; charset=utf-8"
	}
	return "text/plain; charset=utf-8"
}

func parseHeaders(raw string) map[string]string {
	out := map[string]string{}
	if strings.TrimSpace(raw) == "" {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

// trimError прячет длинную обёртку net/http и оставляет суть.
func trimError(err error) string {
	text := err.Error()
	if i := strings.LastIndex(text, ": "); i > 0 && len(text)-i < 80 {
		return text[i+2:]
	}
	if len(text) > 160 {
		return text[:160] + "…"
	}
	return text
}
