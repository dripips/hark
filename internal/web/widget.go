package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dripips/hark/internal/notify"
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
	// Предпросмотр в админке присылает несохранённую тему и подменяет ей
	// сохранённую. Правки видны только тому, кто открыл страницу с этими
	// параметрами: чужой запрос по-прежнему получает то, что в базе.
	if preview := r.URL.Query().Get("preview"); preview != "" && json.Valid([]byte(preview)) {
		copied := *bot
		copied.Theme = preview
		if value := r.URL.Query().Get("launcher"); value != "" {
			copied.LauncherStyle = value
		}
		if value := r.URL.Query().Get("side"); value != "" {
			copied.Position = value
		}
		bot = &copied
	}

	// Отдаём только то, что нужно нарисовать. Настройки модели, ключи и
	// подключения сюда не попадают: этот ответ читает чужая страница.
	writeJSON(w, map[string]any{
		"name":           bot.Name,
		"greeting":       bot.Greeting,
		"accent":         bot.Accent,
		"position":       bot.Position,
		"launcher":       bot.LauncherText,
		"launcher_style": bot.LauncherStyle,
		"avatar":         bot.AvatarEmoji,
		"radius":         bot.CornerRadius,
		"welcome_title":  bot.WelcomeTitle,
		"welcome_text":   bot.WelcomeText,
		"quick":          bot.Quick(),
		"disclaimer":     bot.Disclaimer,
		"privacy_url":    bot.PrivacyURL,
		"privacy_label":  bot.PrivacyLabel,
		"design":         designPayload(bot),
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
	if reply.Escalated {
		// Бот сдался. Открытые вкладки админки узнают об этом сейчас, а не
		// когда кто-нибудь надумает обновить страницу.
		s.notifyQueue(r)
		// А если админку никто не держит открытой — уходит зов наружу.
		s.fireNotify(r, bot, conv, reply.Reason)
	}

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

// trim режет по символам: у посетителя в имени и в адресе страницы бывает
// что угодно, а байтовая обрезка оставила бы половину буквы.
func trim(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit])
}

// designPayload превращает тему в готовые значения CSS. Считает сервер, а не
// виджет: чужая страница получает числа и цвета, а не правила их вывода —
// иначе предпросмотр в админке и живой виджет разъезжаются.
func designPayload(bot *store.Bot) map[string]any {
	d := bot.Design()

	out := map[string]any{
		"font":        d.Stack(),
		"font_url":    d.FontURL,
		"font_size":   d.FontSize,
		"line_height": d.LineHeight,
		"step":        d.Step(),
		"width":       d.Width,
		"height":      d.Height,
		"offset":      d.Offset,
		"radius":      d.Radius,
		"bubble":      d.Bubble,
		"shadow":      shadowCSS(d.Shadow),
		// Без тени панель на белом сайте теряет край, поэтому вместо неё
		// появляется волосяная линия.
		"panel_border": pickBorder(d),
		"scheme":       d.Scheme,
		"accent":       d.Accent,
		"on_accent":    d.OnAccent,
		// ink — акцент, пригодный для текста на полотне. Заливка и надпись
		// одним цветом читаются по-разному, и бледный акцент как текст пропадает.
		"ink":         d.Ink(d.Surface),
		"surface":     d.Surface,
		"text":        d.Text,
		"muted":       d.Muted,
		"border":      d.Border,
		"bot_bubble":  d.BotBubble,
		"bot_text":    d.BotText,
		"user_bubble": d.UserBubble,
		"user_text":   d.UserText,
		"backdrop":    backdropCSS(d, d.BackdropFrom, d.Surface),
		"header":      headerCSS(d),
		"head_text":   headText(d),
		"subtitle":    d.Subtitle,
	}

	// Тёмная палитра нужна только при «как на устройстве»: при явно выбранной
	// схеме виджет одинаков везде. Считается из цветов владельца, а не из
	// зашитого набора — иначе бежевая панель в тёмной системе становится чужой
	// серой.
	if d.Scheme == "auto" {
		dark := d
		dark.Surface = d.DarkSurface
		dark.Text = d.DarkText
		dark.Muted = ""
		dark.Border = ""
		dark.BotBubble = ""
		dark.BotText = ""
		dark.BackdropFrom = d.DarkBackdrop
		// Второй цвет градиента тоже сбрасываем: со светлым концом тёмная
		// лента уходила из чёрного в кремовый.
		dark.BackdropTo = ""
		dark = store.Bot{Theme: dark.Sanitized(), Accent: d.Accent}.Design()

		out["dark"] = map[string]any{
			"surface":    dark.Surface,
			"text":       dark.Text,
			"muted":      dark.Muted,
			"border":     dark.Border,
			"bot_bubble": dark.BotBubble,
			"bot_text":   dark.BotText,
			"ink":        dark.Ink(dark.Surface),
			"backdrop":   backdropCSS(dark, d.DarkBackdrop, dark.Surface),
			"header":     headerCSS(dark),
			"head_text":  headText(dark),
		}
	}
	return out
}

// backdropCSS собирает фон ленты. Точки и сетка рисуются градиентами, чтобы
// виджет остался одним файлом без картинок.
func backdropCSS(d store.Design, base, surface string) string {
	switch d.BackdropKind {
	case "gradient":
		return fmt.Sprintf("linear-gradient(%ddeg, %s, %s)", d.BackdropAngle, base, d.BackdropTo)
	case "dots":
		return fmt.Sprintf("radial-gradient(%s 1px, transparent 1px) 0 0/16px 16px, %s",
			d.Border, base)
	case "grid":
		return fmt.Sprintf(
			"linear-gradient(%s 1px, transparent 1px) 0 0/22px 22px, "+
				"linear-gradient(90deg, %s 1px, transparent 1px) 0 0/22px 22px, %s",
			d.Border, d.Border, base)
	case "image":
		if d.BackdropImage == "" {
			return base
		}
		// Полупрозрачная пелена цвета полотна поверх картинки: без неё текст
		// реплик читается ровно до первого светлого пятна.
		return fmt.Sprintf("linear-gradient(%s, %s), url(%s) center/cover no-repeat, %s",
			fade(surface, 0.72), fade(surface, 0.72), store.Quoted(d.BackdropImage), base)
	default:
		return base
	}
}

func headerCSS(d store.Design) string {
	switch d.HeaderKind {
	case "surface":
		return d.Surface
	case "gradient":
		return fmt.Sprintf("linear-gradient(135deg, %s, %s)", d.Accent, store.Shift(d.Accent, -28))
	default:
		return d.Accent
	}
}

// headText — цвет надписи в шапке. Шапка цвета полотна берёт обычный текст,
// залитая акцентом — контрастную к нему пару.
func headText(d store.Design) string {
	if d.HeaderKind == "surface" {
		return d.Text
	}
	return d.OnAccent
}

func pickBorder(d store.Design) string {
	if d.Shadow != "none" {
		return "none"
	}
	return "1px solid " + d.Border
}

func shadowCSS(kind string) string {
	switch kind {
	case "none":
		return "none"
	case "deep":
		return "0 32px 80px rgba(9,12,20,.34), 0 4px 14px rgba(9,12,20,.16)"
	default:
		return "0 18px 48px rgba(9,12,20,.18), 0 2px 8px rgba(9,12,20,.08)"
	}
}

// fade переводит шестнадцатеричный цвет в rgba с заданной непрозрачностью.
func fade(color string, alpha float64) string {
	var r, g, b int
	if _, err := fmt.Sscanf(color, "#%02x%02x%02x", &r, &g, &b); err != nil {
		return color
	}
	return fmt.Sprintf("rgba(%d,%d,%d,%.2f)", r, g, b, alpha)
}

// fireNotify зовёт человека наружу. Кладёт событие в очередь отправителя и
// сразу возвращается: ответ посетителю не имеет права ждать чужой сервер.
func (s *Server) fireNotify(r *http.Request, bot *store.Bot, conv *store.Conversation, reason string) {
	target := notify.ParseTarget(bot.Notify)
	if !target.Enabled() {
		return
	}
	s.Notify.Fire(target, s.notifyEvent(r, bot, conv, reason, false))
}

func (s *Server) notifyEvent(r *http.Request, bot *store.Bot, conv *store.Conversation,
	reason string, test bool) notify.Event {

	event := notify.Event{
		BotID: bot.ID, BotName: bot.Name, BotSlug: bot.Slug,
		Reason: reason, At: time.Now(), Test: test,
	}
	if conv != nil {
		event.ConvID = conv.ID
		event.PageURL = conv.PageURL
		event.AdminURL = fmt.Sprintf("%s/conversations/%d", publicBase(r), conv.ID)
	} else {
		event.AdminURL = publicBase(r) + "/inbox?state=waiting"
	}
	event.Waiting, _ = s.DB.WaitingCount(r.Context(), bot.ID)
	return event
}

// publicBase — адрес, по которому админка видна снаружи. Берётся из запроса,
// потому что своего адреса Hark не знает и спрашивать его отдельной настройкой
// значит завести поле, которое однажды разойдётся с действительностью.
func publicBase(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	return scheme + "://" + host
}
