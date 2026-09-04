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

	"github.com/dripips/hark/internal/notify"
	"github.com/dripips/hark/internal/store"
	"github.com/dripips/hark/internal/tools"
	"github.com/go-chi/chi/v5"
)

// Страница бота разбита на четыре с собственными адресами.
//
// Раньше это была одна форма на восемнадцать полей, и подключения лежали в ней
// на 2129-м пикселе, ниже кнопки «Сохранить». Владелец их не нашёл — и был
// прав: визуально там подвал страницы, а не раздел. Теперь у каждой части свой
// адрес, своя кнопка сохранения, и открытие бота ведёт на подключения.
var botTabs = []struct{ Slug, Title string }{
	{"connections", "Подключения"},
	{"answers", "Как отвечает"},
	{"model", "Модель"},
	{"widget", "Внешность"},
}

func (s *Server) botFromURL(w http.ResponseWriter, r *http.Request) *store.Bot {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	bot, err := s.DB.BotByID(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return nil
	}
	return bot
}

// botHome ведёт на подключения: это то, ради чего к боту заходят чаще всего.
func (s *Server) botHome(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/bots/"+chi.URLParam(r, "id")+"/connections", http.StatusSeeOther)
}

func (s *Server) botPage(w http.ResponseWriter, r *http.Request, bot *store.Bot,
	tab, template string, extra map[string]any) {

	data := map[string]any{
		"Title": bot.Name, "Bot": bot, "Tab": tab, "Tabs": botTabs,
	}
	for key, value := range extra {
		data[key] = value
	}
	s.render(w, r, template, data)
}

// ── Подключения ─────────────────────────────────────────────────────────

func (s *Server) botConnections(w http.ResponseWriter, r *http.Request) {
	bot := s.botFromURL(w, r)
	if bot == nil {
		return
	}
	list, _ := s.DB.Tools(r.Context(), bot.ID)
	s.botPage(w, r, bot, "connections", "bot-connections.html", map[string]any{
		"Tools":     list,
		"Installed": widgetSnippet(r, bot.Slug),
		"Checked":   r.URL.Query().Get("checked"),
		"CheckText": r.URL.Query().Get("text"),
	})
}

// connectionForm рисует форму одного вида. Вид известен серверу заранее,
// поэтому лишних полей на экране нет: раньше в одной форме соседствовали
// метод с адресом и строка подключения к базе, и было непонятно, что заполнять.
func (s *Server) connectionForm(w http.ResponseWriter, r *http.Request) {
	bot := s.botFromURL(w, r)
	if bot == nil {
		return
	}

	tool := &store.Tool{
		BotID: bot.ID, Kind: r.URL.Query().Get("kind"),
		Method: "GET", Driver: "sqlite", RowLimit: 20, TimeoutMS: 5000, Enabled: true,
	}
	if raw := chi.URLParam(r, "toolID"); raw != "" {
		id, _ := strconv.ParseInt(raw, 10, 64)
		list, _ := s.DB.Tools(r.Context(), bot.ID)
		found := false
		for _, item := range list {
			if item.ID == id {
				tool = item
				found = true
				break
			}
		}
		if !found {
			http.NotFound(w, r)
			return
		}
	}
	if tool.Kind != "sql" {
		tool.Kind = "http"
	}

	s.botPage(w, r, bot, "connections", "bot-connection.html", map[string]any{
		"Tool":  tool,
		"IsNew": tool.ID == 0,
		"Error": r.URL.Query().Get("error"),
	})
}

// connectionSave проверяет данные до записи. Раньше в базу писалось что угодно,
// а отказ рождался в живом разговоре с посетителем — шагом в чеке.
func (s *Server) connectionSave(w http.ResponseWriter, r *http.Request) {
	bot := s.botFromURL(w, r)
	if bot == nil {
		return
	}
	toolID, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)

	tool := &store.Tool{
		ID: toolID, BotID: bot.ID,
		Kind:          r.FormValue("kind"),
		Name:          strings.TrimSpace(r.FormValue("name")),
		Description:   strings.TrimSpace(r.FormValue("description")),
		Parameters:    strings.TrimSpace(r.FormValue("parameters")),
		Method:        r.FormValue("method"),
		URL:           strings.TrimSpace(r.FormValue("url")),
		Headers:       strings.TrimSpace(r.FormValue("headers")),
		BodyTemplate:  r.FormValue("body_template"),
		DSN:           strings.TrimSpace(r.FormValue("dsn")),
		Driver:        r.FormValue("driver"),
		AllowedTables: strings.TrimSpace(r.FormValue("allowed_tables")),
		RowLimit:      atoiDefault(r.FormValue("row_limit"), 20),
		TimeoutMS:     atoiDefault(r.FormValue("timeout_ms"), 5000),
		Enabled:       r.FormValue("enabled") == "on",
	}

	// Пустой ключ означает «не меняли»: секрет в форме не показывается.
	if tool.ID != 0 && tool.DSN == "" {
		if existing := s.findTool(r, bot.ID, tool.ID); existing != nil {
			tool.DSN = existing.DSN
		}
	}

	if problem := validateTool(tool); problem != "" {
		back := fmt.Sprintf("/bots/%d/connections/new?kind=%s&error=%s",
			bot.ID, tool.Kind, url.QueryEscape(problem))
		if tool.ID != 0 {
			back = fmt.Sprintf("/bots/%d/connections/%d?error=%s",
				bot.ID, tool.ID, url.QueryEscape(problem))
		}
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}

	if tool.Kind == "sql" && tool.Parameters == "" {
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
		http.Redirect(w, r, fmt.Sprintf("/bots/%d/connections/new?kind=%s&error=%s",
			bot.ID, tool.Kind, url.QueryEscape(err.Error())), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/bots/%d/connections", bot.ID), http.StatusSeeOther)
}

// validateTool возвращает понятную человеку причину отказа или пустую строку.
func validateTool(t *store.Tool) string {
	if t.Name == "" {
		return "Не задано имя: по нему модель зовёт подключение"
	}
	if strings.ContainsAny(t.Name, " \t") {
		return "В имени нельзя ставить пробелы: модель зовёт его как функцию"
	}
	if t.Description == "" {
		return "Опишите, что делает подключение: это описание читает модель, чтобы решить, когда его звать"
	}
	if t.Headers != "" {
		var probe map[string]string
		if err := json.Unmarshal([]byte(t.Headers), &probe); err != nil {
			return "Заголовки должны быть объектом JSON: " + err.Error()
		}
	}
	if t.Parameters != "" {
		var probe map[string]any
		if err := json.Unmarshal([]byte(t.Parameters), &probe); err != nil {
			return "Схема параметров должна быть объектом JSON: " + err.Error()
		}
	}

	switch t.Kind {
	case "http":
		if t.URL == "" {
			return "Не задан адрес"
		}
		parsed, err := url.Parse(t.URL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return "Адрес должен начинаться с http:// или https://"
		}
	case "sql":
		if t.DSN == "" {
			return "Не задана строка подключения"
		}
		if t.AllowedTables == "" {
			return "Перечислите разрешённые таблицы: без списка подключение не работает вовсе"
		}
	default:
		return "Неизвестный вид подключения"
	}
	return ""
}

func (s *Server) findTool(r *http.Request, botID, toolID int64) *store.Tool {
	list, _ := s.DB.Tools(r.Context(), botID)
	for _, item := range list {
		if item.ID == toolID {
			return item
		}
	}
	return nil
}

// connectionCheck дёргает подключение по-настоящему. У модели такая кнопка
// была с самого начала, а у базы и своего API — нет, и опечатка в строке
// подключения всплывала только в разговоре с посетителем.
func (s *Server) connectionCheck(w http.ResponseWriter, r *http.Request) {
	bot := s.botFromURL(w, r)
	if bot == nil {
		return
	}
	toolID, _ := strconv.ParseInt(chi.URLParam(r, "toolID"), 10, 64)
	tool := s.findTool(r, bot.ID, toolID)
	if tool == nil {
		http.NotFound(w, r)
		return
	}

	status, text := "ошибка", ""
	runner, err := tools.Build(tool)
	if err != nil {
		text = err.Error()
	} else {
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()

		var result tools.Result
		if tool.Kind == "sql" {
			// Берём первую разрешённую таблицу: так проверка ловит и опечатку
			// в строке подключения, и опечатку в имени таблицы.
			table := firstTable(tool.AllowedTables)
			result = runner.Run(ctx, map[string]any{
				"query": fmt.Sprintf("SELECT * FROM %s", table),
			})
		} else {
			result = runner.Run(ctx, map[string]any{})
		}

		switch {
		case result.Err != nil:
			text = result.Err.Error()
		case result.Status == "отклонён" || strings.HasPrefix(result.Status, "ошибка"):
			text = result.Response
		default:
			status = "ок"
			text = result.Status + " · " + truncateText(result.Response, 220)
		}
	}

	http.Redirect(w, r, fmt.Sprintf("/bots/%d/connections?checked=%s&text=%s",
		bot.ID, status, url.QueryEscape(truncateText(text, 300))), http.StatusSeeOther)
}

func firstTable(list string) string {
	for _, item := range strings.FieldsFunc(list, func(r rune) bool {
		return r == ',' || r == '\n' || r == ' '
	}) {
		if item = strings.TrimSpace(item); item != "" {
			return item
		}
	}
	return "sqlite_master"
}

func (s *Server) connectionDelete(w http.ResponseWriter, r *http.Request) {
	bot := s.botFromURL(w, r)
	if bot == nil {
		return
	}
	toolID, _ := strconv.ParseInt(chi.URLParam(r, "toolID"), 10, 64)
	_ = s.DB.DeleteTool(r.Context(), bot.ID, toolID)
	http.Redirect(w, r, fmt.Sprintf("/bots/%d/connections", bot.ID), http.StatusSeeOther)
}

// ── Остальные вкладки ───────────────────────────────────────────────────

func (s *Server) botAnswers(w http.ResponseWriter, r *http.Request) {
	bot := s.botFromURL(w, r)
	if bot == nil {
		return
	}
	s.botPage(w, r, bot, "answers", "bot-answers.html", map[string]any{
		"Notify": notify.ParseTarget(bot.Notify),
		"Tested": r.URL.Query().Get("tested"),
	})
}

// notifyTest звонит по настроенному адресу прямо сейчас и показывает исход.
//
// Кнопка шлёт настоящее тело на настоящий адрес, а не проверяет, что адрес
// похож на адрес. Мост может ответить «200» и ничего человеку не показать —
// поэтому проверяется «дошло до человека», а не «сервер жив».
func (s *Server) notifyTest(w http.ResponseWriter, r *http.Request) {
	bot := s.botFromURL(w, r)
	if bot == nil {
		return
	}

	target := targetFromForm(r)
	if !target.Enabled() {
		http.Redirect(w, r, fmt.Sprintf("/bots/%d/answers?tested=%s", bot.ID,
			url.QueryEscape("Адрес не задан")), http.StatusSeeOther)
		return
	}

	// Сохраняем то, что в форме: иначе человек проверит одно, а сохранит другое.
	if raw, err := json.Marshal(target); err == nil {
		bot.Notify = string(raw)
		_ = s.DB.SaveBot(r.Context(), bot)
	}

	event := s.notifyEvent(r, bot, nil, "проверка настройки из админки", true)
	status := s.Notify.Send(target, event)
	_ = s.DB.NoteNotify(r.Context(), bot.ID, status)

	http.Redirect(w, r, fmt.Sprintf("/bots/%d/answers?tested=%s",
		bot.ID, url.QueryEscape(status)), http.StatusSeeOther)
}

func targetFromForm(r *http.Request) notify.Target {
	return notify.ParseTarget(mustJSON(notify.Target{
		URL:      strings.TrimSpace(r.FormValue("notify_url")),
		Method:   r.FormValue("notify_method"),
		Headers:  strings.TrimSpace(r.FormValue("notify_headers")),
		Template: r.FormValue("notify_template"),
		Quiet:    r.FormValue("notify_quiet") == "on",
	}))
}

func mustJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(raw)
}

func (s *Server) botModel(w http.ResponseWriter, r *http.Request) {
	bot := s.botFromURL(w, r)
	if bot == nil {
		return
	}
	s.botPage(w, r, bot, "model", "bot-model.html", map[string]any{
		"Caps":   bot.Caps(),
		"Probed": bot.Capabilities != "" && bot.Capabilities != "{}",
	})
}

func (s *Server) botWidget(w http.ResponseWriter, r *http.Request) {
	bot := s.botFromURL(w, r)
	if bot == nil {
		return
	}
	// Set — ключи, заданные владельцем явно. Остальные цвета выводятся из
	// полотна и текста, и форма показывает их с галочкой «авто»: сняв её,
	// владелец забирает цвет себе, оставив — получает пересчёт при смене фона.
	set := map[string]bool{}
	if bot.Theme != "" {
		var raw map[string]any
		if json.Unmarshal([]byte(bot.Theme), &raw) == nil {
			for key, value := range raw {
				if text, ok := value.(string); !ok || text != "" {
					set[key] = true
				}
			}
		}
	}
	s.botPage(w, r, bot, "widget", "bot-widget.html", map[string]any{
		"Installed": widgetSnippet(r, bot.Slug),
		"Set":       set,
	})
}

// widgetSnippet собирает готовую строку установки. Раньше в шаблоне стоял
// литерал «{{адрес Hark}}», и его приходилось править руками.
func widgetSnippet(r *http.Request, slug string) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return fmt.Sprintf(`<script src="%s://%s/widget/hark.js" data-bot="%s" defer></script>`,
		scheme, r.Host, slug)
}

func truncateText(s string, limit int) string { return store.Clip(s, limit) }

// Каждая вкладка сохраняет только свои поля. Одна общая форма стирала бы
// чужие значения, потому что их нет в отправленных данных.

func (s *Server) botAnswersSave(w http.ResponseWriter, r *http.Request) {
	bot := s.botFromURL(w, r)
	if bot == nil {
		return
	}
	bot.Name = strings.TrimSpace(r.FormValue("name"))
	bot.Instructions = r.FormValue("instructions")
	bot.Greeting = r.FormValue("greeting")
	bot.EscalateAfter = atoiDefault(r.FormValue("escalate_after"), 2)
	bot.Enabled = r.FormValue("enabled") == "on"
	bot.Notify = mustJSON(targetFromForm(r))
	s.saveAndBack(w, r, bot, "answers")
}

func (s *Server) botModelSave(w http.ResponseWriter, r *http.Request) {
	bot := s.botFromURL(w, r)
	if bot == nil {
		return
	}
	bot.Provider = r.FormValue("provider")
	bot.BaseURL = strings.TrimSpace(r.FormValue("base_url"))
	bot.Model = strings.TrimSpace(r.FormValue("model"))
	if key := strings.TrimSpace(r.FormValue("api_key")); key != "" {
		bot.APIKey = key
	}
	bot.MaxTokens = atoiDefault(r.FormValue("max_tokens"), 1200)
	bot.Temperature = strings.TrimSpace(r.FormValue("temperature"))
	bot.Reasoning = r.FormValue("reasoning")
	bot.PriceIn = int64(atoiDefault(r.FormValue("price_in"), 0))
	bot.PriceOut = int64(atoiDefault(r.FormValue("price_out"), 0))
	s.saveAndBack(w, r, bot, "model")
}

func (s *Server) botWidgetSave(w http.ResponseWriter, r *http.Request) {
	bot := s.botFromURL(w, r)
	if bot == nil {
		return
	}
	bot.Accent = r.FormValue("accent")
	bot.Position = r.FormValue("position")
	bot.LauncherText = r.FormValue("launcher_text")
	bot.LauncherStyle = r.FormValue("launcher_style")
	bot.AvatarEmoji = strings.TrimSpace(r.FormValue("avatar_emoji"))
	bot.CornerRadius = atoiDefault(r.FormValue("corner_radius"), 18)
	bot.WelcomeTitle = strings.TrimSpace(r.FormValue("welcome_title"))
	bot.WelcomeText = strings.TrimSpace(r.FormValue("welcome_text"))
	bot.QuickReplies = r.FormValue("quick_replies")
	bot.Disclaimer = strings.TrimSpace(r.FormValue("disclaimer"))
	bot.PrivacyURL = strings.TrimSpace(r.FormValue("privacy_url"))
	bot.PrivacyLabel = strings.TrimSpace(r.FormValue("privacy_label"))
	bot.AllowedOrigins = r.FormValue("allowed_origins")

	// Тему собрал браузер и прислал одним JSON. Проверяем, что это объект, и
	// кладём как есть: разбирать её на поля здесь — значит завести вторую
	// копию списка ручек, которая рано или поздно разойдётся с первой.
	if theme := strings.TrimSpace(r.FormValue("theme")); theme != "" {
		var probe map[string]any
		if err := json.Unmarshal([]byte(theme), &probe); err == nil && probe != nil {
			// В базу кладём вычищенное, а не присланное. Иначе туда попадают
			// и битые байты, и значения вне допустимых границ — до первого
			// чтения они выглядят нормальной темой.
			checked := store.Bot{Theme: theme, Accent: bot.Accent, CornerRadius: bot.CornerRadius}
			design := checked.Design()
			bot.Theme = design.Sanitized()
			// Акцент и скругление дублируются в своих колонках: их читает
			// список ботов и места, которые про тему ещё не знают. Берём
			// проверенные значения, а не присланные.
			bot.Accent = design.Accent
			bot.CornerRadius = design.Radius
		}
	}
	s.saveAndBack(w, r, bot, "widget")

}

// botWidgetPreview рисует условную чужую страницу с настоящим виджетом.
//
// Внутри рамки крутится тот же hark.js, что попадёт на сайт, и настройки он
// берёт через тот же обработчик конфигурации. Поэтому «в предпросмотре было
// иначе» тут не случается: макета, который мог бы разойтись с виджетом, нет.
func (s *Server) botWidgetPreview(w http.ResponseWriter, r *http.Request) {
	bot := s.botFromURL(w, r)
	if bot == nil {
		return
	}

	query := url.Values{}
	if theme := r.URL.Query().Get("theme"); theme != "" && json.Valid([]byte(theme)) {
		query.Set("preview", theme)
	}
	for _, key := range []string{"launcher", "side"} {
		if value := r.URL.Query().Get(key); value != "" {
			query.Set(key, value)
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Рамку показываем только внутри админки: снаружи её открывать незачем.
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	if err := s.templates.ExecuteTemplate(w, "preview", map[string]any{
		"Slug": bot.Slug, "Query": query.Encode(),
	}); err != nil {
		http.Error(w, "ошибка шаблона: "+err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) saveAndBack(w http.ResponseWriter, r *http.Request, bot *store.Bot, tab string) {
	if err := s.DB.SaveBot(r.Context(), bot); err != nil {
		http.Error(w, "не удалось сохранить: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/bots/%d/%s", bot.ID, tab), http.StatusSeeOther)
}

// connectionsAll — сводный экран по всем ботам. Нужен, чтобы слово
// «Подключения» жило в верхней навигации: владелец ищет его там.
func (s *Server) connectionsAll(w http.ResponseWriter, r *http.Request) {
	bots, _ := s.DB.Bots(r.Context())

	type row struct {
		Bot  *store.Bot
		Tool *store.Tool
	}
	var rows []row
	for _, bot := range bots {
		list, _ := s.DB.Tools(r.Context(), bot.ID)
		for _, tool := range list {
			rows = append(rows, row{Bot: bot, Tool: tool})
		}
	}
	s.render(w, r, "connections.html", map[string]any{
		"Title": "Подключения", "Rows": rows, "Bots": bots,
	})
}
