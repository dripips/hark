package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dripips/hark/internal/llm"
	"github.com/dripips/hark/internal/store"
)

// fakeProvider отвечает по заранее записанному сценарию: сеть в тестах не
// нужна, а проверять надо не модель, а то, что чек собирается верно.
type fakeProvider struct {
	turns []llm.Response
	seen  int
}

func (f *fakeProvider) Name() string { return "fake" }

func (f *fakeProvider) Complete(ctx context.Context, req llm.Request) (*llm.Response, error) {
	if f.seen >= len(f.turns) {
		return &llm.Response{Text: "конец сценария"}, nil
	}
	resp := f.turns[f.seen]
	f.seen++
	return &resp, nil
}

func (f *fakeProvider) Stream(ctx context.Context, req llm.Request) (<-chan llm.Chunk, error) {
	resp, err := f.Complete(ctx, req)
	if err != nil {
		return nil, err
	}
	out := make(chan llm.Chunk, 2)
	if resp.Text != "" {
		out <- llm.Chunk{Text: resp.Text}
	}
	out <- llm.Chunk{Done: true, Response: resp}
	close(out)
	return out, nil
}

func (f *fakeProvider) Models(ctx context.Context) ([]string, error) { return nil, nil }

// Подменяем построитель поставщика на время теста.
func withFake(t *testing.T, fake llm.Provider) {
	t.Helper()
	original := buildProvider
	buildProvider = func(*store.Bot) (llm.Provider, error) { return fake, nil }
	t.Cleanup(func() { buildProvider = original })
}

func setup(t *testing.T) (*store.DB, *store.Bot, *store.Conversation) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "hark.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	bot := &store.Bot{
		Slug: "shop", Name: "Магазин", Instructions: "Помогай с заказами.",
		Provider: "openai", Model: "test-model", MaxTokens: 900,
		// 15 копеек за миллион на вводе, 60 на выводе.
		PriceIn: 15, PriceOut: 60, EscalateAfter: 2, Enabled: true,
	}
	if err := db.SaveBot(context.Background(), bot); err != nil {
		t.Fatal(err)
	}
	conv := &store.Conversation{BotID: bot.ID, Token: "tok-1"}
	if err := db.CreateConversation(context.Background(), conv); err != nil {
		t.Fatal(err)
	}
	return db, bot, conv
}

func TestReceiptRecordsToolCall(t *testing.T) {
	db, bot, conv := setup(t)
	ctx := context.Background()

	// Инструмент — настоящий HTTP, поднятый рядом: проверяем весь путь, а не
	// заглушку вместо заглушки.
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"в пути","eta":"12 сентября"}`))
	}))
	defer api.Close()

	tool := &store.Tool{
		BotID: bot.ID, Kind: "http", Name: "order_status",
		Description: "Статус заказа", Method: "GET", URL: api.URL + "/orders/{id}",
		Parameters: `{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`,
		TimeoutMS:  3000, Enabled: true,
	}
	if err := db.SaveTool(ctx, tool); err != nil {
		t.Fatal(err)
	}

	withFake(t, &fakeProvider{turns: []llm.Response{
		{
			ToolCalls: []llm.ToolCall{{ID: "c1", Name: "order_status", Arguments: `{"id":"4127"}`}},
			Usage:     llm.Usage{PromptTokens: 200, CompletionTokens: 40, ReasoningTokens: 30},
		},
		{
			Text:  "Заказ 4127 в пути, приедет 12 сентября.",
			Usage: llm.Usage{PromptTokens: 300, CompletionTokens: 60, ReasoningTokens: 45},
		},
	}})

	if err := db.AddMessage(ctx, &store.Message{
		ConversationID: conv.ID, Role: "user", Text: "Где заказ 4127?",
	}); err != nil {
		t.Fatal(err)
	}

	engine := &Engine{DB: db}
	reply, err := engine.Answer(ctx, bot, conv, nil)
	if err != nil {
		t.Fatalf("ответ не получен: %v", err)
	}
	if reply.Escalated {
		t.Fatalf("бот сдался, хотя инструмент ответил: %s", reply.Reason)
	}
	if !strings.Contains(reply.Message.Text, "4127") {
		t.Errorf("в ответе нет номера заказа: %q", reply.Message.Text)
	}

	receipt := reply.Receipt

	// Шаги: обращение к модели, вызов инструмента, ещё обращение к модели.
	var kinds []string
	for _, step := range receipt.Steps {
		kinds = append(kinds, step.Kind+":"+step.Name)
	}
	if len(receipt.Steps) != 3 {
		t.Fatalf("ожидали три шага, получили %d: %v", len(receipt.Steps), kinds)
	}
	if receipt.Steps[1].Kind != "tool" || receipt.Steps[1].Name != "order_status" {
		t.Errorf("второй шаг должен быть вызовом инструмента: %v", kinds)
	}
	if !strings.Contains(receipt.Steps[1].Request, "4127") {
		t.Errorf("в чеке не видно, что уходило в API: %q", receipt.Steps[1].Request)
	}
	if !strings.Contains(receipt.Steps[1].Response, "в пути") {
		t.Errorf("в чеке не видно, что вернул API: %q", receipt.Steps[1].Response)
	}
	if receipt.Steps[1].Status != "200" {
		t.Errorf("статус вызова: %q", receipt.Steps[1].Status)
	}

	// Токены складываются по всем обращениям, рассуждение считается отдельно.
	if receipt.PromptTokens != 500 || receipt.CompletionTokens != 100 {
		t.Errorf("токены сложились неверно: ввод %d, вывод %d",
			receipt.PromptTokens, receipt.CompletionTokens)
	}
	if receipt.ReasoningTokens != 75 {
		t.Errorf("рассуждение должно считаться отдельно, получили %d", receipt.ReasoningTokens)
	}

	// Стоимость считается в микрорублях: два миллиона токенов ввода по
	// 15 копеек за миллион — это 30 копеек, миллион вывода по 60 — ещё 60.
	// Итого 0,90 ₽, то есть 900 000 микрорублей.
	big := Cost(bot, llm.Usage{PromptTokens: 2_000_000, CompletionTokens: 1_000_000})
	if big != 900_000 {
		t.Errorf("стоимость посчитана неверно: %d микрорублей вместо 900000", big)
	}

	// Один ответ не должен округляться в ноль — ради этого и микрорубли.
	one := Cost(bot, llm.Usage{PromptTokens: 509, CompletionTokens: 518})
	if one == 0 {
		t.Error("стоимость одного ответа округлилась в ноль")
	}

	// Чек должен читаться из базы, а не только жить в памяти.
	saved, err := db.ReceiptsFor(ctx, conv.ID)
	if err != nil {
		t.Fatal(err)
	}
	stored, ok := saved[reply.Message.ID]
	if !ok {
		t.Fatal("чек не сохранён")
	}
	if len(stored.Steps) != 3 {
		t.Errorf("после чтения из базы шагов %d", len(stored.Steps))
	}
}

func TestEscalationCallsHuman(t *testing.T) {
	db, bot, conv := setup(t)
	ctx := context.Background()

	withFake(t, &fakeProvider{turns: []llm.Response{
		{ToolCalls: []llm.ToolCall{{
			ID: "c1", Name: escalateTool,
			Arguments: `{"reason":"нужен возврат, у бота нет прав"}`,
		}}},
		{Text: "Подключаю менеджера."},
	}})

	if err := db.AddMessage(ctx, &store.Message{
		ConversationID: conv.ID, Role: "user", Text: "Хочу вернуть товар",
	}); err != nil {
		t.Fatal(err)
	}

	engine := &Engine{DB: db}
	reply, err := engine.Answer(ctx, bot, conv, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reply.Escalated {
		t.Fatal("бот не позвал человека, хотя вызвал инструмент сдачи")
	}
	if !strings.Contains(reply.Reason, "возврат") {
		t.Errorf("причина не доехала до очереди: %q", reply.Reason)
	}

	// Разговор должен встать в очередь к человеку.
	fresh, err := db.ConversationByID(ctx, conv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.State != "waiting" {
		t.Errorf("состояние разговора %q, ожидали waiting", fresh.State)
	}
	if !fresh.EscalatedAt.Valid {
		t.Error("время передачи человеку не записано")
	}
}

func TestKnobsRespectProbe(t *testing.T) {
	bot := &store.Bot{Temperature: "0.4", Reasoning: "low"}

	// Проба сказала, что температуры нет: параметр не должен уйти в запрос,
	// иначе поставщик уронит весь разговор.
	caps, _ := json.Marshal(map[string]bool{"Temperature": false, "ReasoningEffort": true})
	bot.Capabilities = string(caps)

	req := llm.Request{}
	applyKnobs(bot, &req)
	if req.Temperature != nil {
		t.Error("температура ушла в запрос вопреки пробе")
	}
	if req.ReasoningEffort != "low" {
		t.Errorf("усилие рассуждения не передано: %q", req.ReasoningEffort)
	}

	// Проба сказала, что температура есть — передаём.
	caps, _ = json.Marshal(map[string]bool{"Temperature": true})
	bot.Capabilities = string(caps)
	req = llm.Request{}
	applyKnobs(bot, &req)
	if req.Temperature == nil || *req.Temperature != 0.4 {
		t.Errorf("температура не передана: %v", req.Temperature)
	}
}
