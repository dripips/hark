package web

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dripips/hark/internal/chat"
	"github.com/dripips/hark/internal/llm"
	"github.com/dripips/hark/internal/store"
	"github.com/dripips/hark/internal/tools"
	"github.com/go-chi/chi/v5"
)

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bots, _ := s.DB.Bots(ctx)
	waiting, _ := s.DB.Conversations(ctx, 0, "waiting", 5)
	recent, _ := s.DB.Conversations(ctx, 0, "", 8)

	summary := s.summary(ctx, 7)

	s.render(w, r, "dashboard.html", map[string]any{
		"Title": "Обзор", "Bots": bots, "Waiting": waiting,
		"Recent": recent, "Summary": summary,
	})
}

func (s *Server) botList(w http.ResponseWriter, r *http.Request) {
	bots, _ := s.DB.Bots(r.Context())
	s.render(w, r, "bots.html", map[string]any{"Title": "Боты", "Bots": bots})
}

func (s *Server) botEdit(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	bot, err := s.DB.BotByID(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	toolRows, _ := s.DB.Tools(r.Context(), bot.ID)

	s.render(w, r, "bot.html", map[string]any{
		"Title": bot.Name, "Bot": bot, "Tools": toolRows,
		"Caps": bot.Caps(), "Probed": bot.Capabilities != "" && bot.Capabilities != "{}",
	})
}

func (s *Server) botSave(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	bot, err := s.DB.BotByID(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	bot.Name = r.FormValue("name")
	bot.Instructions = r.FormValue("instructions")
	bot.Greeting = r.FormValue("greeting")
	bot.Provider = r.FormValue("provider")
	bot.BaseURL = strings.TrimSpace(r.FormValue("base_url"))
	bot.Model = strings.TrimSpace(r.FormValue("model"))
	if key := strings.TrimSpace(r.FormValue("api_key")); key != "" {
		// Пустое поле означает «не меняли»: иначе ключ стирался бы при каждом
		// сохранении формы, где его не показывают.
		bot.APIKey = key
	}
	bot.MaxTokens = atoiDefault(r.FormValue("max_tokens"), 1200)
	bot.Temperature = strings.TrimSpace(r.FormValue("temperature"))
	bot.Reasoning = r.FormValue("reasoning")
	bot.PriceIn = int64(atoiDefault(r.FormValue("price_in"), 0))
	bot.PriceOut = int64(atoiDefault(r.FormValue("price_out"), 0))
	bot.Accent = r.FormValue("accent")
	bot.Position = r.FormValue("position")
	bot.LauncherText = r.FormValue("launcher_text")
	bot.AllowedOrigins = r.FormValue("allowed_origins")
	bot.EscalateAfter = atoiDefault(r.FormValue("escalate_after"), 2)
	bot.Enabled = r.FormValue("enabled") == "on"

	if err := s.DB.SaveBot(r.Context(), bot); err != nil {
		http.Error(w, "не удалось сохранить: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/bots/"+strconv.FormatInt(bot.ID, 10), http.StatusSeeOther)
}

// botProbe спрашивает у модели, что она принимает. Кнопкой, а не при каждом
// сохранении: у думающих моделей даже «скажи привет» стоит сотни токенов.
func (s *Server) botProbe(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	bot, err := s.DB.BotByID(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	var provider llm.Provider
	if bot.Provider == "anthropic" {
		provider = llm.NewAnthropic(bot.BaseURL, bot.APIKey)
	} else {
		provider = llm.NewOpenAI(bot.BaseURL, bot.APIKey)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()

	caps := llm.Probe(ctx, provider, bot.Model)
	encoded, _ := json.Marshal(caps)
	bot.Capabilities = string(encoded)
	if err := s.DB.SaveBot(ctx, bot); err != nil {
		http.Error(w, "не удалось сохранить пробу", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/bots/"+strconv.FormatInt(bot.ID, 10), http.StatusSeeOther)
}

func (s *Server) toolSave(w http.ResponseWriter, r *http.Request) {
	botID, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	toolID, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)

	tool := &store.Tool{
		ID: toolID, BotID: botID,
		Kind:          r.FormValue("kind"),
		Name:          strings.TrimSpace(r.FormValue("name")),
		Description:   r.FormValue("description"),
		Parameters:    r.FormValue("parameters"),
		Method:        r.FormValue("method"),
		URL:           strings.TrimSpace(r.FormValue("url")),
		Headers:       r.FormValue("headers"),
		DSN:           strings.TrimSpace(r.FormValue("dsn")),
		Driver:        r.FormValue("driver"),
		AllowedTables: r.FormValue("allowed_tables"),
		RowLimit:      atoiDefault(r.FormValue("row_limit"), 50),
		TimeoutMS:     atoiDefault(r.FormValue("timeout_ms"), 5000),
		Enabled:       r.FormValue("enabled") == "on",
	}
	if tool.Kind == "sql" && strings.TrimSpace(tool.Parameters) == "" {
		// У SQL-инструмента схема всегда одна: модель пишет запрос.
		schema, _ := json.Marshal(tools.QuerySchema())
		tool.Parameters = string(schema)
	}
	if tool.Headers == "" {
		tool.Headers = "{}"
	}
	if tool.Parameters == "" {
		tool.Parameters = "{}"
	}

	if err := s.DB.SaveTool(r.Context(), tool); err != nil {
		http.Error(w, "не удалось сохранить инструмент: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/bots/"+strconv.FormatInt(botID, 10)+"#tools", http.StatusSeeOther)
}

func (s *Server) toolDelete(w http.ResponseWriter, r *http.Request) {
	botID, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	toolID, _ := strconv.ParseInt(chi.URLParam(r, "toolID"), 10, 64)
	_ = s.DB.DeleteTool(r.Context(), botID, toolID)
	http.Redirect(w, r, "/bots/"+strconv.FormatInt(botID, 10)+"#tools", http.StatusSeeOther)
}

func (s *Server) inbox(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	rows, _ := s.DB.Conversations(r.Context(), 0, state, 100)

	bots, _ := s.DB.Bots(r.Context())
	names := map[int64]string{}
	for _, bot := range bots {
		names[bot.ID] = bot.Name
	}

	s.render(w, r, "inbox.html", map[string]any{
		"Title": "Разговоры", "Conversations": rows, "BotNames": names, "State": state,
	})
}

// conversation — главный экран продукта: разговор, а у каждого ответа бота
// раскрывается чек.
func (s *Server) conversation(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	conv, err := s.DB.ConversationByID(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	bot, _ := s.DB.BotByID(r.Context(), conv.BotID)
	messages, _ := s.DB.Messages(r.Context(), conv.ID)
	receipts, _ := s.DB.ReceiptsFor(r.Context(), conv.ID)

	type line struct {
		Message *store.Message
		Receipt *store.Receipt
	}
	lines := make([]line, 0, len(messages))
	var totalCost int64
	var totalReasoning, totalCompletion int
	for _, m := range messages {
		lines = append(lines, line{Message: m, Receipt: receipts[m.ID]})
		if receipt := receipts[m.ID]; receipt != nil {
			totalCost += receipt.CostMicro
			totalReasoning += receipt.ReasoningTokens
			totalCompletion += receipt.CompletionTokens
		}
	}

	s.render(w, r, "conversation.html", map[string]any{
		"Title": "Разговор", "Conversation": conv, "Bot": bot, "Lines": lines,
		"TotalCost": totalCost, "TotalReasoning": totalReasoning,
		"TotalCompletion": totalCompletion,
		"ReasoningShare":  share(totalReasoning, totalCompletion),
	})
}

func (s *Server) conversationReply(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	text := strings.TrimSpace(r.FormValue("text"))
	if text == "" {
		http.Redirect(w, r, "/conversations/"+chi.URLParam(r, "id"), http.StatusSeeOther)
		return
	}
	if err := s.DB.AddMessage(r.Context(), &store.Message{
		ConversationID: id, Role: "human", Text: text, Author: managerName(r),
	}); err != nil {
		http.Error(w, "не удалось отправить", http.StatusInternalServerError)
		return
	}
	// Ответил человек — разговор остаётся за человеком, пока его не вернут боту.
	_ = s.DB.SetConversationState(r.Context(), id, "human", "")
	http.Redirect(w, r, "/conversations/"+chi.URLParam(r, "id"), http.StatusSeeOther)
}

func (s *Server) conversationState(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	state := r.FormValue("state")
	switch state {
	case "open", "waiting", "human", "closed":
		_ = s.DB.SetConversationState(r.Context(), id, state, r.FormValue("reason"))
	}
	http.Redirect(w, r, "/conversations/"+chi.URLParam(r, "id"), http.StatusSeeOther)
}

func (s *Server) analytics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	days := atoiDefault(r.URL.Query().Get("days"), 14)

	type day struct {
		Date      string
		Dialogues int
		Answers   int
		Escalated int
		Cost      int64
		Reasoning int
		Output    int
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT date(r.created_at) AS d,
		       COUNT(DISTINCT m.conversation_id),
		       COUNT(*),
		       SUM(r.cost_micro),
		       SUM(r.reasoning_tokens),
		       SUM(r.completion_tokens)
		FROM receipts r
		JOIN messages m ON m.id = r.message_id
		WHERE r.created_at >= date('now', ?)
		GROUP BY d ORDER BY d DESC`, "-"+strconv.Itoa(days)+" days")
	if err != nil {
		http.Error(w, "ошибка выборки", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var series []day
	for rows.Next() {
		var d day
		if err := rows.Scan(&d.Date, &d.Dialogues, &d.Answers, &d.Cost,
			&d.Reasoning, &d.Output); err != nil {
			continue
		}
		series = append(series, d)
	}

	// Передачи человеку считаются по разговорам, а не по чекам.
	escalations := map[string]int{}
	esc, err := s.DB.QueryContext(ctx, `
		SELECT date(escalated_at), COUNT(*) FROM conversations
		WHERE escalated_at IS NOT NULL AND escalated_at >= date('now', ?)
		GROUP BY 1`, "-"+strconv.Itoa(days)+" days")
	if err == nil {
		defer esc.Close()
		for esc.Next() {
			var date string
			var count int
			if esc.Scan(&date, &count) == nil {
				escalations[date] = count
			}
		}
	}
	for i := range series {
		series[i].Escalated = escalations[series[i].Date]
	}

	s.render(w, r, "analytics.html", map[string]any{
		"Title": "Аналитика", "Series": series, "Days": days,
		"Summary": s.summary(ctx, days),
	})
}

type summary struct {
	Dialogues      int
	Answers        int
	Escalated      int
	Cost           int64
	Reasoning      int
	Output         int
	ReasoningShare int
}

func (s *Server) summary(ctx context.Context, days int) summary {
	var out summary
	window := "-" + strconv.Itoa(days) + " days"

	_ = s.DB.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(cost_micro),0), COALESCE(SUM(reasoning_tokens),0),
		       COALESCE(SUM(completion_tokens),0)
		FROM receipts WHERE created_at >= date('now', ?)`, window).
		Scan(&out.Answers, &out.Cost, &out.Reasoning, &out.Output)

	_ = s.DB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM conversations WHERE created_at >= date('now', ?)`, window).
		Scan(&out.Dialogues)

	_ = s.DB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM conversations
		WHERE escalated_at IS NOT NULL AND escalated_at >= date('now', ?)`, window).
		Scan(&out.Escalated)

	out.ReasoningShare = share(out.Reasoning, out.Output)
	return out
}

// share считает долю рассуждения в выводе. Это число объясняет счёт: у
// думающих моделей оно доходит до девяноста процентов.
func share(part, whole int) int {
	if whole <= 0 {
		return 0
	}
	return part * 100 / whole
}

func atoiDefault(s string, fallback int) int {
	if value, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return value
	}
	return fallback
}

var _ = chat.Cost
