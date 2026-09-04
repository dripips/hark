// Package store хранит настройки ботов, разговоры и чеки в SQLite.
//
// SQLite выбран намеренно: Hark ставят себе, и отдельная база рядом — это
// лишний повод не поставить. Подключённая база клиента живёт отдельно и
// только на чтение (см. internal/tools).
package store

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

type DB struct{ *sql.DB }

func Open(path string) (*DB, error) {
	// busy_timeout нужен: виджет пишет реплики, а админка читает их же.
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	handle, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := handle.Ping(); err != nil {
		return nil, err
	}
	if _, err := handle.Exec(schema); err != nil {
		return nil, fmt.Errorf("схема: %w", err)
	}
	// База могла быть создана прошлой версией: досыпаем недостающие колонки.
	if err := migrate(handle); err != nil {
		return nil, fmt.Errorf("миграция: %w", err)
	}
	return &DB{handle}, nil
}

// ── Модели ──────────────────────────────────────────────────────────────

type Bot struct {
	ID             int64
	Slug           string
	Name           string
	Instructions   string
	Greeting       string
	Provider       string
	BaseURL        string
	Model          string
	APIKey         string
	MaxTokens      int
	Temperature    string
	Reasoning      string
	Capabilities   string
	PriceIn        int64
	PriceOut       int64
	Accent         string
	Position       string
	LauncherText   string
	AllowedOrigins string
	EscalateAfter  int
	Enabled        bool

	// Приветственный экран и подсказки виджета.
	WelcomeTitle  string
	WelcomeText   string
	QuickReplies  string
	Disclaimer    string
	PrivacyURL    string
	PrivacyLabel  string
	LauncherStyle string
	AvatarEmoji   string
	CornerRadius  int

	// Theme — вся внешность одним JSON. Разбор в Design().
	Theme string

	// Lang — на каком языке бот говорит с посетителем. Отдельно от языка
	// админки: владелец смотрит её по-русски, а сайт держит английский.
	Lang string

	// Notify — куда звонить, когда бот сдался. Тоже одним JSON: см. Theme.
	Notify           string
	NotifyLastAt     sql.NullString
	NotifyLastStatus string
}

// LangOr — язык бота с отступлением на русский. Пустое поле встречается у
// ботов, заведённых до появления языка.
func (b Bot) LangOr() string {
	if b.Lang == "" {
		return "ru"
	}
	return b.Lang
}

// Quick разбирает готовые вопросы: по одному в строке, пустые пропускаются.
func (b Bot) Quick() []string {
	var out []string
	for _, item := range strings.Split(b.QuickReplies, "\n") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// Caps разбирает сохранённую пробу. Пустая или битая строка означает
// «ещё не проверяли», и настройки показывают всё с оговоркой.
func (b Bot) Caps() map[string]any {
	out := map[string]any{}
	_ = json.Unmarshal([]byte(b.Capabilities), &out)
	return out
}

func (b Bot) Origins() []string {
	var list []string
	for _, item := range strings.Split(b.AllowedOrigins, "\n") {
		if item = strings.TrimSpace(item); item != "" {
			list = append(list, item)
		}
	}
	return list
}

type Tool struct {
	ID            int64
	BotID         int64
	Kind          string
	Name          string
	Description   string
	Parameters    string
	Method        string
	URL           string
	Headers       string
	BodyTemplate  string
	DSN           string
	Driver        string
	AllowedTables string
	RowLimit      int
	TimeoutMS     int
	Enabled       bool
	Position      int
}

type Conversation struct {
	ID             int64
	BotID          int64
	Token          string
	Visitor        string
	PageURL        string
	State          string
	EscalatedAt    sql.NullString
	EscalateReason string
	Failures       int
	ClaimedBy      sql.NullInt64
	ClaimedAt      sql.NullString
	CreatedAt      string
	UpdatedAt      string
}

type Message struct {
	ID             int64
	ConversationID int64
	Role           string
	Text           string
	Author         string
	CreatedAt      string
}

// Step — одна строка чека: обращение к модели или вызов инструмента.
type Step struct {
	Kind     string `json:"kind"` // model, tool, error
	Name     string `json:"name,omitempty"`
	Detail   string `json:"detail,omitempty"`
	Request  string `json:"request,omitempty"`
	Response string `json:"response,omitempty"`
	Status   string `json:"status,omitempty"`
	TookMS   int64  `json:"took_ms"`
}

type Receipt struct {
	ID               int64
	MessageID        int64
	BotID            int64
	Provider         string
	Model            string
	Steps            []Step
	PromptTokens     int
	CompletionTokens int
	ReasoningTokens  int
	CachedTokens     int
	CostMicro        int64
	TookMS           int64
	Error            string
	CreatedAt        string
}

// ── Боты ────────────────────────────────────────────────────────────────

const botColumns = `id, slug, name, instructions, greeting, provider, base_url, model,
	api_key, max_tokens, temperature, reasoning, capabilities, price_in, price_out,
	accent, position, launcher_text, allowed_origins, escalate_after, enabled,
	welcome_title, welcome_text, quick_replies, disclaimer, privacy_url, privacy_label,
	launcher_style, avatar_emoji, corner_radius, theme,
	notify, notify_last_at, notify_last_status, lang`

func scanBot(row interface{ Scan(...any) error }) (*Bot, error) {
	var b Bot
	err := row.Scan(&b.ID, &b.Slug, &b.Name, &b.Instructions, &b.Greeting, &b.Provider,
		&b.BaseURL, &b.Model, &b.APIKey, &b.MaxTokens, &b.Temperature, &b.Reasoning,
		&b.Capabilities, &b.PriceIn, &b.PriceOut, &b.Accent, &b.Position, &b.LauncherText,
		&b.AllowedOrigins, &b.EscalateAfter, &b.Enabled,
		&b.WelcomeTitle, &b.WelcomeText, &b.QuickReplies, &b.Disclaimer,
		&b.PrivacyURL, &b.PrivacyLabel, &b.LauncherStyle, &b.AvatarEmoji, &b.CornerRadius, &b.Theme,
		&b.Notify, &b.NotifyLastAt, &b.NotifyLastStatus, &b.Lang)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (db *DB) Bots(ctx context.Context) ([]*Bot, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+botColumns+` FROM bots ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Bot
	for rows.Next() {
		bot, err := scanBot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, bot)
	}
	return out, rows.Err()
}

func (db *DB) BotBySlug(ctx context.Context, slug string) (*Bot, error) {
	return scanBot(db.QueryRowContext(ctx,
		`SELECT `+botColumns+` FROM bots WHERE slug = ?`, slug))
}

func (db *DB) BotByID(ctx context.Context, id int64) (*Bot, error) {
	return scanBot(db.QueryRowContext(ctx,
		`SELECT `+botColumns+` FROM bots WHERE id = ?`, id))
}

func (db *DB) SaveBot(ctx context.Context, b *Bot) error {
	if b.ID == 0 {
		result, err := db.ExecContext(ctx, `
			INSERT INTO bots (slug, name, instructions, greeting, provider, base_url, model,
				api_key, max_tokens, temperature, reasoning, capabilities, price_in, price_out,
				accent, position, launcher_text, allowed_origins, escalate_after, enabled,
				welcome_title, welcome_text, quick_replies, disclaimer, privacy_url,
				privacy_label, launcher_style, avatar_emoji, corner_radius, theme, notify, lang)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			b.Slug, b.Name, b.Instructions, b.Greeting, b.Provider, b.BaseURL, b.Model,
			b.APIKey, b.MaxTokens, b.Temperature, b.Reasoning, b.Capabilities,
			b.PriceIn, b.PriceOut, b.Accent, b.Position, b.LauncherText,
			b.AllowedOrigins, b.EscalateAfter, b.Enabled,
			b.WelcomeTitle, b.WelcomeText, b.QuickReplies, b.Disclaimer,
			b.PrivacyURL, b.PrivacyLabel, b.LauncherStyle, b.AvatarEmoji, b.CornerRadius,
			b.Theme, b.Notify, b.LangOr())
		if err != nil {
			return err
		}
		b.ID, err = result.LastInsertId()
		return err
	}
	_, err := db.ExecContext(ctx, `
		UPDATE bots SET slug=?, name=?, instructions=?, greeting=?, provider=?, base_url=?,
			model=?, api_key=?, max_tokens=?, temperature=?, reasoning=?, capabilities=?,
			price_in=?, price_out=?, accent=?, position=?, launcher_text=?, allowed_origins=?,
			escalate_after=?, enabled=?, welcome_title=?, welcome_text=?, quick_replies=?,
			disclaimer=?, privacy_url=?, privacy_label=?, launcher_style=?, avatar_emoji=?,
			corner_radius=?, theme=?, notify=?, lang=?, updated_at=datetime('now')
		WHERE id=?`,
		b.Slug, b.Name, b.Instructions, b.Greeting, b.Provider, b.BaseURL, b.Model,
		b.APIKey, b.MaxTokens, b.Temperature, b.Reasoning, b.Capabilities,
		b.PriceIn, b.PriceOut, b.Accent, b.Position, b.LauncherText, b.AllowedOrigins,
		b.EscalateAfter, b.Enabled,
		b.WelcomeTitle, b.WelcomeText, b.QuickReplies, b.Disclaimer,
		b.PrivacyURL, b.PrivacyLabel, b.LauncherStyle, b.AvatarEmoji, b.CornerRadius,
		b.Theme, b.Notify, b.LangOr(), b.ID)
	return err
}

// ── Инструменты ─────────────────────────────────────────────────────────

const toolColumns = `id, bot_id, kind, name, description, parameters, method, url, headers,
	body_template, dsn, driver, allowed_tables, row_limit, timeout_ms, enabled, position`

func (db *DB) Tools(ctx context.Context, botID int64) ([]*Tool, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT `+toolColumns+` FROM tools WHERE bot_id = ? ORDER BY position, id`, botID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Tool
	for rows.Next() {
		var t Tool
		if err := rows.Scan(&t.ID, &t.BotID, &t.Kind, &t.Name, &t.Description, &t.Parameters,
			&t.Method, &t.URL, &t.Headers, &t.BodyTemplate, &t.DSN, &t.Driver,
			&t.AllowedTables, &t.RowLimit, &t.TimeoutMS, &t.Enabled, &t.Position); err != nil {
			return nil, err
		}
		out = append(out, &t)
	}
	return out, rows.Err()
}

func (db *DB) SaveTool(ctx context.Context, t *Tool) error {
	if t.ID == 0 {
		result, err := db.ExecContext(ctx, `
			INSERT INTO tools (bot_id, kind, name, description, parameters, method, url,
				headers, body_template, dsn, driver, allowed_tables, row_limit, timeout_ms,
				enabled, position)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			t.BotID, t.Kind, t.Name, t.Description, t.Parameters, t.Method, t.URL,
			t.Headers, t.BodyTemplate, t.DSN, t.Driver, t.AllowedTables, t.RowLimit,
			t.TimeoutMS, t.Enabled, t.Position)
		if err != nil {
			return err
		}
		t.ID, err = result.LastInsertId()
		return err
	}
	_, err := db.ExecContext(ctx, `
		UPDATE tools SET kind=?, name=?, description=?, parameters=?, method=?, url=?,
			headers=?, body_template=?, dsn=?, driver=?, allowed_tables=?, row_limit=?,
			timeout_ms=?, enabled=?, position=? WHERE id=?`,
		t.Kind, t.Name, t.Description, t.Parameters, t.Method, t.URL, t.Headers,
		t.BodyTemplate, t.DSN, t.Driver, t.AllowedTables, t.RowLimit, t.TimeoutMS,
		t.Enabled, t.Position, t.ID)
	return err
}

func (db *DB) DeleteTool(ctx context.Context, botID, id int64) error {
	_, err := db.ExecContext(ctx, `DELETE FROM tools WHERE id = ? AND bot_id = ?`, id, botID)
	return err
}

// ── Разговоры ───────────────────────────────────────────────────────────

const convColumns = `id, bot_id, token, visitor, page_url, state, escalated_at,
	escalate_reason, failures, claimed_by, claimed_at, created_at, updated_at`

func scanConversation(row interface{ Scan(...any) error }) (*Conversation, error) {
	var c Conversation
	err := row.Scan(&c.ID, &c.BotID, &c.Token, &c.Visitor, &c.PageURL, &c.State,
		&c.EscalatedAt, &c.EscalateReason, &c.Failures, &c.ClaimedBy, &c.ClaimedAt,
		&c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (db *DB) ConversationByToken(ctx context.Context, token string) (*Conversation, error) {
	return scanConversation(db.QueryRowContext(ctx,
		`SELECT `+convColumns+` FROM conversations WHERE token = ?`, token))
}

func (db *DB) ConversationByID(ctx context.Context, id int64) (*Conversation, error) {
	return scanConversation(db.QueryRowContext(ctx,
		`SELECT `+convColumns+` FROM conversations WHERE id = ?`, id))
}

func (db *DB) CreateConversation(ctx context.Context, c *Conversation) error {
	result, err := db.ExecContext(ctx,
		`INSERT INTO conversations (bot_id, token, visitor, page_url) VALUES (?,?,?,?)`,
		c.BotID, c.Token, c.Visitor, c.PageURL)
	if err != nil {
		return err
	}
	c.ID, err = result.LastInsertId()
	return err
}

// Conversations отдаёт ленту для админки. Пустое state означает «все».
func (db *DB) Conversations(ctx context.Context, botID int64, state string, limit int) ([]*Conversation, error) {
	query := `SELECT ` + convColumns + ` FROM conversations WHERE 1=1`
	args := []any{}
	if botID > 0 {
		query += ` AND bot_id = ?`
		args = append(args, botID)
	}
	if state != "" {
		query += ` AND state = ?`
		args = append(args, state)
	}
	// Ждущие человека всегда наверху: это очередь, а не архив.
	query += ` ORDER BY CASE state WHEN 'waiting' THEN 0 WHEN 'human' THEN 1 ELSE 2 END,
		updated_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Conversation
	for rows.Next() {
		c, err := scanConversation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// NoteNotify записывает исход последнего зова.
//
// Отдельным узким запросом, а не через SaveBot: зов уходит из фоновой
// горутины и приземляется тогда, когда владелец, может быть, как раз правит
// настройки. Общее сохранение затёрло бы одно другим.
func (db *DB) NoteNotify(ctx context.Context, botID int64, status string) error {
	_, err := db.ExecContext(ctx, `
		UPDATE bots SET notify_last_at = datetime('now'), notify_last_status = ?
		WHERE id = ?`, status, botID)
	return err
}

// WaitingCount — сколько разговоров ждут человека у одного бота.
func (db *DB) WaitingCount(ctx context.Context, botID int64) (int, error) {
	var count int
	err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM conversations WHERE bot_id = ? AND state = 'waiting'`,
		botID).Scan(&count)
	return count, err
}

// CountWaiting — сколько разговоров ждут человека. Индекс по (state,
// updated_at) уже есть, поэтому счётчик дёшев и его можно считать на каждый
// показ страницы, а не хранить где-то рядом и рассинхронизировать.
func (db *DB) CountWaiting(ctx context.Context) (int, error) {
	var count int
	err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM conversations WHERE state = 'waiting'`).Scan(&count)
	return count, err
}

func (db *DB) SetConversationState(ctx context.Context, id int64, state, reason string) error {
	escalated := ""
	if state == "waiting" {
		escalated = time.Now().UTC().Format(time.RFC3339)
	}
	_, err := db.ExecContext(ctx, `
		UPDATE conversations
		SET state = ?, escalate_reason = ?,
		    escalated_at = CASE WHEN ? <> '' THEN ? ELSE escalated_at END,
		    updated_at = datetime('now')
		WHERE id = ?`, state, reason, escalated, escalated, id)
	return err
}

func (db *DB) BumpFailures(ctx context.Context, id int64, reset bool) (int, error) {
	if reset {
		_, err := db.ExecContext(ctx,
			`UPDATE conversations SET failures = 0, updated_at = datetime('now') WHERE id = ?`, id)
		return 0, err
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE conversations SET failures = failures + 1, updated_at = datetime('now')
		 WHERE id = ?`, id); err != nil {
		return 0, err
	}
	var failures int
	err := db.QueryRowContext(ctx,
		`SELECT failures FROM conversations WHERE id = ?`, id).Scan(&failures)
	return failures, err
}

// ── Реплики ─────────────────────────────────────────────────────────────

func (db *DB) AddMessage(ctx context.Context, m *Message) error {
	result, err := db.ExecContext(ctx,
		`INSERT INTO messages (conversation_id, role, text, author) VALUES (?,?,?,?)`,
		m.ConversationID, m.Role, m.Text, m.Author)
	if err != nil {
		return err
	}
	if m.ID, err = result.LastInsertId(); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx,
		`UPDATE conversations SET updated_at = datetime('now') WHERE id = ?`, m.ConversationID)
	return err
}

func (db *DB) Messages(ctx context.Context, conversationID int64) ([]*Message, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, conversation_id, role, text, author, created_at
		 FROM messages WHERE conversation_id = ? ORDER BY id`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Text,
			&m.Author, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &m)
	}
	return out, rows.Err()
}

// ── Чеки ────────────────────────────────────────────────────────────────

func (db *DB) SaveReceipt(ctx context.Context, r *Receipt) error {
	steps, err := json.Marshal(r.Steps)
	if err != nil {
		return err
	}
	result, err := db.ExecContext(ctx, `
		INSERT INTO receipts (message_id, bot_id, provider, model, steps, prompt_tokens,
			completion_tokens, reasoning_tokens, cached_tokens, cost_micro, took_ms, error)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.MessageID, r.BotID, r.Provider, r.Model, string(steps), r.PromptTokens,
		r.CompletionTokens, r.ReasoningTokens, r.CachedTokens, r.CostMicro,
		r.TookMS, r.Error)
	if err != nil {
		return err
	}
	r.ID, err = result.LastInsertId()
	return err
}

func (db *DB) ReceiptsFor(ctx context.Context, conversationID int64) (map[int64]*Receipt, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT r.id, r.message_id, r.bot_id, r.provider, r.model, r.steps, r.prompt_tokens,
		       r.completion_tokens, r.reasoning_tokens, r.cached_tokens, r.cost_micro,
		       r.took_ms, r.error, r.created_at
		FROM receipts r
		JOIN messages m ON m.id = r.message_id
		WHERE m.conversation_id = ?`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int64]*Receipt{}
	for rows.Next() {
		var r Receipt
		var steps string
		if err := rows.Scan(&r.ID, &r.MessageID, &r.BotID, &r.Provider, &r.Model, &steps,
			&r.PromptTokens, &r.CompletionTokens, &r.ReasoningTokens, &r.CachedTokens,
			&r.CostMicro, &r.TookMS, &r.Error, &r.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(steps), &r.Steps)
		out[r.MessageID] = &r
	}
	return out, rows.Err()
}
