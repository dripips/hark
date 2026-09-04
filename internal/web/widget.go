package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/dripips/hark/internal/store"
)

// widgetCORS пускает виджет только с тех доменов, которые указал владелец.
// Пустой список означает «любой»: на своём сайте это удобно, а закрыть можно
// одной строкой в настройках.
func (s *Server) widgetCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		slug := botSlug(r)

		allowed := "*"
		if slug != "" {
			if bot, err := s.DB.BotBySlug(r.Context(), slug); err == nil {
				list := bot.Origins()
				if len(list) > 0 {
					allowed = ""
					for _, item := range list {
						if strings.EqualFold(item, origin) {
							allowed = origin
							break
						}
					}
					if allowed == "" {
						http.Error(w, "домен не разрешён", http.StatusForbidden)
						return
					}
				}
			}
		}

		w.Header().Set("Access-Control-Allow-Origin", allowed)
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Vary", "Origin")
		next.ServeHTTP(w, r)
	})
}

func botSlug(r *http.Request) string {
	if slug := r.URL.Query().Get("bot"); slug != "" {
		return slug
	}
	return r.URL.Query().Get("slug")
}

func (s *Server) widgetScript(w http.ResponseWriter, r *http.Request) {
	data, err := assets.ReadFile("widget/hark.js")
	if err != nil {
		http.Error(w, "виджет не найден", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(data)
}

func (s *Server) widgetConfig(w http.ResponseWriter, r *http.Request) {
	bot, err := s.DB.BotBySlug(r.Context(), botSlug(r))
	if err != nil || !bot.Enabled {
		http.Error(w, "бот недоступен", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{
		"name":     bot.Name,
		"greeting": bot.Greeting,
		"accent":   bot.Accent,
		"position": bot.Position,
		"launcher": bot.LauncherText,
	})
}

func (s *Server) widgetStart(w http.ResponseWriter, r *http.Request) {
	bot, err := s.DB.BotBySlug(r.Context(), botSlug(r))
	if err != nil || !bot.Enabled {
		http.Error(w, "бот недоступен", http.StatusNotFound)
		return
	}

	var body struct {
		PageURL string `json:"page_url"`
		Visitor string `json:"visitor"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	conv := &store.Conversation{
		BotID: bot.ID, Token: randomToken(),
		PageURL: trim(body.PageURL, 500), Visitor: trim(body.Visitor, 120),
	}
	if err := s.DB.CreateConversation(r.Context(), conv); err != nil {
		http.Error(w, "не удалось начать разговор", http.StatusInternalServerError)
		return
	}
	if bot.Greeting != "" {
		_ = s.DB.AddMessage(r.Context(), &store.Message{
			ConversationID: conv.ID, Role: "assistant", Text: bot.Greeting,
		})
	}
	writeJSON(w, map[string]any{"token": conv.Token, "greeting": bot.Greeting})
}

// widgetSend принимает реплику и отдаёт ответ потоком.
//
// Поток здесь не украшение: у думающей модели первый токен приходит через
// несколько секунд, и без потока виджет выглядит зависшим.
func (s *Server) widgetSend(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
		Text  string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "плохой запрос", http.StatusBadRequest)
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" {
		http.Error(w, "пустая реплика", http.StatusBadRequest)
		return
	}
	if len(text) > 4000 {
		text = text[:4000]
	}

	conv, err := s.DB.ConversationByToken(r.Context(), body.Token)
	if err != nil {
		http.Error(w, "разговор не найден", http.StatusNotFound)
		return
	}
	bot, err := s.DB.BotByID(r.Context(), conv.BotID)
	if err != nil {
		http.Error(w, "бот не найден", http.StatusNotFound)
		return
	}

	if err := s.DB.AddMessage(r.Context(), &store.Message{
		ConversationID: conv.ID, Role: "user", Text: text,
	}); err != nil {
		http.Error(w, "не удалось сохранить", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "поток не поддерживается", http.StatusInternalServerError)
		return
	}

	// Разговор уже у человека: бот не вмешивается, реплика просто ложится в
	// ленту и ждёт менеджера.
	if conv.State == "human" || conv.State == "waiting" {
		sendEvent(w, flusher, "waiting", map[string]any{
			"text": "Сообщение передано менеджеру.",
		})
		sendEvent(w, flusher, "done", map[string]any{"state": conv.State})
		return
	}

	sendEvent(w, flusher, "typing", map[string]any{})

	reply, err := s.Engine.Answer(r.Context(), bot, conv, func(chunk string) {
		sendEvent(w, flusher, "chunk", map[string]any{"text": chunk})
	})
	if err != nil {
		sendEvent(w, flusher, "error", map[string]any{"text": err.Error()})
		sendEvent(w, flusher, "done", map[string]any{})
		return
	}

	sendEvent(w, flusher, "message", map[string]any{
		"id": reply.Message.ID, "text": reply.Message.Text, "role": "assistant",
	})
	sendEvent(w, flusher, "done", map[string]any{
		"escalated": reply.Escalated,
		"state":     stateAfter(reply.Escalated),
	})
}

// widgetPoll отдаёт реплики после указанной: так виджет забирает ответ
// менеджера, не открывая второй поток.
func (s *Server) widgetPoll(w http.ResponseWriter, r *http.Request) {
	conv, err := s.DB.ConversationByToken(r.Context(), r.URL.Query().Get("token"))
	if err != nil {
		http.Error(w, "разговор не найден", http.StatusNotFound)
		return
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)

	messages, err := s.DB.Messages(r.Context(), conv.ID)
	if err != nil {
		http.Error(w, "ошибка чтения", http.StatusInternalServerError)
		return
	}
	var fresh []map[string]any
	for _, m := range messages {
		if m.ID <= after || m.Role == "user" {
			continue
		}
		fresh = append(fresh, map[string]any{
			"id": m.ID, "text": m.Text, "role": m.Role, "author": m.Author,
		})
	}
	writeJSON(w, map[string]any{"messages": fresh, "state": conv.State})
}

func stateAfter(escalated bool) string {
	if escalated {
		return "waiting"
	}
	return "open"
}

func sendEvent(w http.ResponseWriter, flusher http.Flusher, event string, payload map[string]any) {
	data, _ := json.Marshal(payload)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	flusher.Flush()
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(payload)
}

func trim(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit]
}
