package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dripips/hark/internal/chat"
	"github.com/dripips/hark/internal/lang"
	"github.com/dripips/hark/internal/llm"
	"github.com/dripips/hark/internal/store"
	"github.com/go-chi/chi/v5"
)

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bots, _ := s.DB.Bots(ctx)
	waiting, _ := s.DB.Conversations(ctx, 0, "waiting", 5)
	recent, _ := s.DB.Conversations(ctx, 0, "", 8)

	summary := s.summary(ctx, 7)

	s.render(w, r, "dashboard.html", map[string]any{
		"Title": lang.T(language(r), "Обзор"), "Bots": bots, "Waiting": waiting,
		"Recent": recent, "Summary": summary,
	})
}

func (s *Server) botList(w http.ResponseWriter, r *http.Request) {
	bots, _ := s.DB.Bots(r.Context())
	// Сколько у бота подключений — то же, что «врёт он или знает». Считаем
	// одним запросом на всю страницу, а не по обращению на карточку.
	counts, _ := s.DB.ToolCounts(r.Context())
	s.render(w, r, "bots.html", map[string]any{
		"Title": lang.T(language(r), "Боты"), "Bots": bots, "ToolCounts": counts,
	})
}

// botCreate заводит бота с рабочими значениями по умолчанию.
//
// Без этого продукт на чистой установке недостижим: инструменты живут внутри
// бота, а бота завести было нечем — только список и надпись «Ботов пока нет».
func (s *Server) botCreate(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = "Новый бот"
	}
	slug := slugify(r.FormValue("slug"))
	if slug == "" {
		slug = slugify(name)
	}
	if slug == "" {
		slug = "bot"
	}

	// Слаг попадает в тег script на чужом сайте, поэтому он должен быть
	// единственным; при совпадении просто дописываем число.
	base := slug
	for attempt := 2; attempt < 100; attempt++ {
		if _, err := s.DB.BotBySlug(r.Context(), slug); err != nil {
			break
		}
		slug = fmt.Sprintf("%s-%d", base, attempt)
	}

	bot := &store.Bot{
		Slug: slug, Name: name,
		Instructions:  "Отвечай коротко и по делу. Данные бери из инструментов, не выдумывай.",
		Greeting:      "Здравствуйте! Чем помочь?",
		Provider:      "openai",
		Model:         "gpt-5-nano",
		MaxTokens:     1200,
		Accent:        store.DefaultAccent,
		Position:      "right",
		LauncherText:  "Спросить",
		LauncherStyle: "pill",
		CornerRadius:  18,
		EscalateAfter: 2,
		Enabled:       true,
	}
	if err := s.DB.SaveBot(r.Context(), bot); err != nil {
		http.Error(w, lang.T(language(r), "не удалось создать бота: ")+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/bots/"+strconv.FormatInt(bot.ID, 10), http.StatusSeeOther)
}

// slugify оставляет то, что можно писать в адресе и в атрибуте data-bot.
func slugify(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(raw)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '_' || r == '-':
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
				b.WriteRune('-')
			}
		}
	}
	return strings.Trim(b.String(), "-")
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
		http.Error(w, lang.T(language(r), "не удалось сохранить пробу"), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/bots/"+strconv.FormatInt(bot.ID, 10), http.StatusSeeOther)
}

func (s *Server) inbox(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	rows, _ := s.DB.Conversations(r.Context(), 0, state, 100)

	bots, _ := s.DB.Bots(r.Context())
	names := map[int64]string{}
	for _, bot := range bots {
		names[bot.ID] = bot.Name
	}

	claims, _ := s.DB.ClaimNames(r.Context())

	s.render(w, r, "inbox.html", map[string]any{
		"Title": lang.T(language(r), "Разговоры"), "Conversations": rows, "BotNames": names, "State": state,
		"Claims": claims,
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

	claimedBy, claimed := s.DB.ClaimedBy(r.Context(), conv.ID)
	me := currentManager(r)
	mine := claimed && me != nil && conv.ClaimedBy.Valid && conv.ClaimedBy.Int64 == me.ID

	s.render(w, r, "conversation.html", map[string]any{
		"Title": lang.T(language(r), "Разговор"), "Conversation": conv, "Bot": bot, "Lines": lines,
		"ClaimedBy": claimedBy, "Claimed": claimed, "Mine": mine,
		// Проигравший гонку приходит сюда с именем победителя в адресе.
		"Taken":     r.URL.Query().Get("taken"),
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
		http.Error(w, lang.T(language(r), "не удалось отправить"), http.StatusInternalServerError)
		return
	}
	// Ответил человек — разговор остаётся за человеком, пока его не вернут боту.
	_ = s.DB.SetConversationState(r.Context(), id, "human", "")
	// И заодно записывается на ответившего, если был свободен. Чужой разговор
	// не перехватываем: Claim молча откажет, и в списке останется прежнее имя.
	if me := currentManager(r); me != nil {
		_ = s.DB.Claim(r.Context(), id, me.ID)
	}
	// Разговор ушёл из очереди: у коллег цифра падает, и видно, что его взяли.
	s.notifyQueue(r)
	http.Redirect(w, r, "/conversations/"+chi.URLParam(r, "id"), http.StatusSeeOther)
}

func (s *Server) conversationState(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	state := r.FormValue("state")
	switch state {
	case "open", "waiting", "human", "closed":
		_ = s.DB.SetConversationState(r.Context(), id, state, r.FormValue("reason"))
		// Вернули боту или закрыли — отметка «взял» больше ни о чём не говорит.
		if state == "open" || state == "closed" {
			_ = s.DB.Release(r.Context(), id)
		}
		s.notifyQueue(r)
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
		http.Error(w, lang.T(language(r), "ошибка выборки"), http.StatusInternalServerError)
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
		"Title": lang.T(language(r), "Аналитика"), "Series": series, "Days": days,
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

// conversationClaim отмечает разговор за тем, кто нажал.
//
// Кнопка нужна отдельно от ответа: увидеть занятость надо ДО того, как
// второй менеджер начнёт печатать, а не после того, как посетитель получит
// два ответа от разных людей.
func (s *Server) conversationClaim(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	me := currentManager(r)
	if me == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if err := s.DB.Claim(r.Context(), id, me.ID); err != nil {
		// Проиграл гонку: показываем чужое имя, а не молча перехватываем.
		name, _ := s.DB.ClaimedBy(r.Context(), id)
		http.Redirect(w, r, fmt.Sprintf("/conversations/%d?taken=%s", id,
			url.QueryEscape(name)), http.StatusSeeOther)
		return
	}
	s.notifyQueue(r)
	http.Redirect(w, r, fmt.Sprintf("/conversations/%d", id), http.StatusSeeOther)
}

func (s *Server) conversationRelease(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	_ = s.DB.Release(r.Context(), id)
	s.notifyQueue(r)
	http.Redirect(w, r, fmt.Sprintf("/conversations/%d", id), http.StatusSeeOther)
}
