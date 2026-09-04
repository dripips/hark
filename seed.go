package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"github.com/dripips/hark/internal/store"
	"github.com/dripips/hark/internal/tools"
	"github.com/dripips/hark/internal/web"
)

// seedDemo наполняет базу так, чтобы экраны было на чём смотреть, а бот
// работал по-настоящему: демонстрационный магазин лежит отдельным файлом,
// и SQL-инструмент читает именно его.
//
// Числа в чеках взяты из живого прогона на gpt-5-nano, а не выдуманы: там
// рассуждение занимает большую часть вывода, и в этом весь смысл экрана.
func seedDemo(db *store.DB) error {
	ctx := context.Background()

	shopPath, err := seedShop()
	if err != nil {
		return err
	}

	hash, err := web.HashPassword("hark")
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO managers (email, name, password_hash) VALUES (?,?,?)
		 ON CONFLICT(email) DO UPDATE SET password_hash = excluded.password_hash`,
		"manager@example.com", "Ирина", hash); err != nil {
		return err
	}

	bot := &store.Bot{
		Slug: "shop", Name: "Помощник магазина",
		Instructions: "Ты помощник интернет-магазина «Полка». Отвечай коротко, " +
			"по-русски и только по делу. Статусы заказов и наличие товара смотри " +
			"в базе, а не по памяти. Возвраты и претензии передавай человеку.",
		Greeting: "Здравствуйте! Подскажу по заказу, доставке и наличию.",
		Provider: "openai", Model: "gpt-5-nano",
		APIKey: os.Getenv("OPENAI_API_KEY"), MaxTokens: 1200, Reasoning: "low",
		// Цена gpt-5-nano на сентябрь 2026, в копейках за миллион токенов.
		PriceIn: 4, PriceOut: 32,
		Accent: store.DefaultAccent, Position: "right", LauncherText: "Спросить",
		EscalateAfter: 2, Enabled: true,
	}
	if err := db.SaveBot(ctx, bot); err != nil {
		return err
	}

	schema, _ := json.Marshal(tools.QuerySchema())
	sqlTool := &store.Tool{
		BotID: bot.ID, Kind: "sql", Name: "shop_db",
		Description: "База магазина, только чтение. Таблицы: " +
			"orders(id, customer, status, eta, total_rub), " +
			"products(id, title, price_rub, in_stock).",
		Parameters: string(schema), DSN: shopPath, Driver: "sqlite",
		AllowedTables: "orders, products", RowLimit: 20, TimeoutMS: 4000,
		Enabled: true, Position: 1,
	}
	if err := db.SaveTool(ctx, sqlTool); err != nil {
		return err
	}

	httpTool := &store.Tool{
		BotID: bot.ID, Kind: "http", Name: "delivery_estimate",
		Description: "Срок доставки в город по индексу.",
		Parameters: `{"type":"object","properties":` +
			`{"zip":{"type":"string","description":"почтовый индекс"}},"required":["zip"]}`,
		Method: "GET", URL: "https://api.example.com/delivery/{zip}",
		Headers: `{}`, TimeoutMS: 5000, Enabled: false, Position: 2,
	}
	if err := db.SaveTool(ctx, httpTool); err != nil {
		return err
	}

	return seedConversations(ctx, db, bot)
}

// seedShop кладёт рядом базу магазина: это та самая «подключённая база»,
// в которую ходит бот.
func seedShop() (string, error) {
	path, err := filepath.Abs("hark-demo-shop.db")
	if err != nil {
		return "", err
	}
	_ = os.Remove(path)

	shop, err := sql.Open("sqlite", path)
	if err != nil {
		return "", err
	}
	defer shop.Close()

	if _, err := shop.Exec(`
		CREATE TABLE orders (
			id INTEGER PRIMARY KEY, customer TEXT, status TEXT, eta TEXT, total_rub INTEGER);
		CREATE TABLE products (
			id INTEGER PRIMARY KEY, title TEXT, price_rub INTEGER, in_stock INTEGER);`); err != nil {
		return "", err
	}

	orders := [][]any{
		{4127, "Анна К.", "в пути", "12 сентября", 5400},
		{4128, "Игорь С.", "собирается", "15 сентября", 12800},
		{4129, "Мария Л.", "доставлен", "1 сентября", 3200},
		{4130, "Пётр В.", "ждёт оплаты", "", 8900},
	}
	for _, row := range orders {
		if _, err := shop.Exec(
			`INSERT INTO orders (id, customer, status, eta, total_rub) VALUES (?,?,?,?,?)`,
			row...); err != nil {
			return "", err
		}
	}

	products := [][]any{
		{1, "Полка дубовая, 80 см", 5400, 12},
		{2, "Полка дубовая, 120 см", 7900, 0},
		{3, "Кронштейн стальной", 890, 140},
		{4, "Комплект крепежа", 320, 65},
	}
	for _, row := range products {
		if _, err := shop.Exec(
			`INSERT INTO products (id, title, price_rub, in_stock) VALUES (?,?,?,?)`,
			row...); err != nil {
			return "", err
		}
	}

	fmt.Printf("магазин для демонстрации: %s\n", path)
	return path, nil
}

// seedConversations записывает разговоры вместе с чеками. Модель здесь не
// зовётся: экраны должны наполняться без ключа и без трат.
func seedConversations(ctx context.Context, db *store.DB, bot *store.Bot) error {
	type turn struct {
		user      string
		answer    string
		steps     []store.Step
		prompt    int
		out       int
		reasoning int
		took      int64
	}
	type dialogue struct {
		page      string
		turns     []turn
		escalated string
		state     string
		ago       time.Duration
	}

	dialogues := []dialogue{
		{
			page: "https://polka.example.com/orders", ago: 40 * time.Minute, state: "open",
			turns: []turn{{
				user:   "Где мой заказ 4127?",
				answer: "Заказ 4127 в пути, доставим 12 сентября. Сумма 5400 ₽.",
				steps: []store.Step{
					{Kind: "model", Name: "gpt-5-nano-2025-08-07", Status: "tool_calls",
						Detail: "ввод 191, вывод 96, рассуждение 64", TookMS: 1840,
						Response: "просит инструменты: shop_db"},
					{Kind: "tool", Name: "shop_db", Status: "1 строк", TookMS: 12,
						Request:  "SELECT * FROM (SELECT id, status, eta, total_rub FROM orders WHERE id = 4127) AS hark_limited LIMIT 20",
						Response: `[{"eta":"12 сентября","id":4127,"status":"в пути","total_rub":5400}]`},
					{Kind: "model", Name: "gpt-5-nano-2025-08-07", Status: "stop",
						Detail: "ввод 318, вывод 422, рассуждение 384", TookMS: 3120,
						Response: "Заказ 4127 в пути, доставим 12 сентября. Сумма 5400 ₽."},
				},
				prompt: 509, out: 518, reasoning: 448, took: 5010,
			}},
		},
		{
			page: "https://polka.example.com/catalog", ago: 3 * time.Hour, state: "open",
			turns: []turn{{
				user:   "Полка 120 см есть в наличии?",
				answer: "Полки 120 см сейчас нет. Есть 80 см за 5400 ₽, осталось 12 штук.",
				steps: []store.Step{
					{Kind: "model", Name: "gpt-5-nano-2025-08-07", Status: "tool_calls",
						Detail: "ввод 188, вывод 88, рассуждение 56", TookMS: 1610,
						Response: "просит инструменты: shop_db"},
					{Kind: "tool", Name: "shop_db", Status: "2 строк", TookMS: 9,
						Request:  "SELECT * FROM (SELECT title, price_rub, in_stock FROM products WHERE title LIKE '%Полка%') AS hark_limited LIMIT 20",
						Response: `[{"in_stock":12,"price_rub":5400,"title":"Полка дубовая, 80 см"},{"in_stock":0,"price_rub":7900,"title":"Полка дубовая, 120 см"}]`},
					{Kind: "model", Name: "gpt-5-nano-2025-08-07", Status: "stop",
						Detail: "ввод 342, вывод 356, рассуждение 296", TookMS: 2740,
						Response: "Полки 120 см сейчас нет. Есть 80 см за 5400 ₽, осталось 12 штук."},
				},
				prompt: 530, out: 444, reasoning: 352, took: 4400,
			}},
		},
		{
			page: "https://polka.example.com/orders/4130", ago: 25 * time.Minute,
			state: "waiting", escalated: "просит вернуть товар, у бота нет прав на возврат",
			turns: []turn{{
				user:   "Хочу вернуть заказ 4130, он мне не подошёл",
				answer: "Передаю разговор менеджеру, он ответит здесь же.",
				steps: []store.Step{
					{Kind: "model", Name: "gpt-5-nano-2025-08-07", Status: "tool_calls",
						Detail: "ввод 204, вывод 148, рассуждение 112", TookMS: 2210,
						Response: "просит инструменты: call_human"},
					{Kind: "tool", Name: "call_human", Status: "зовём человека",
						Detail: "просит вернуть товар, у бота нет прав на возврат"},
					{Kind: "model", Name: "gpt-5-nano-2025-08-07", Status: "stop",
						Detail: "ввод 268, вывод 96, рассуждение 64", TookMS: 1490,
						Response: "Передаю разговор менеджеру, он ответит здесь же."},
				},
				prompt: 472, out: 244, reasoning: 176, took: 3700,
			}},
		},
		{
			page: "https://polka.example.com/", ago: 90 * time.Minute, state: "open",
			turns: []turn{{
				user:   "Сколько стоит крепёж?",
				answer: "Комплект крепежа 320 ₽, в наличии 65 штук. Кронштейн стальной 890 ₽.",
				steps: []store.Step{
					{Kind: "model", Name: "gpt-5-nano-2025-08-07", Status: "tool_calls",
						Detail: "ввод 186, вывод 84, рассуждение 52", TookMS: 1520,
						Response: "просит инструменты: shop_db"},
					{Kind: "tool", Name: "shop_db", Status: "отклонён", TookMS: 0,
						Request:  "SELECT * FROM suppliers",
						Response: `таблица "suppliers" не в списке разрешённых`},
					{Kind: "tool", Name: "shop_db", Status: "2 строк", TookMS: 8,
						Request:  "SELECT * FROM (SELECT title, price_rub, in_stock FROM products WHERE price_rub < 1000) AS hark_limited LIMIT 20",
						Response: `[{"in_stock":140,"price_rub":890,"title":"Кронштейн стальной"},{"in_stock":65,"price_rub":320,"title":"Комплект крепежа"}]`},
					{Kind: "model", Name: "gpt-5-nano-2025-08-07", Status: "stop",
						Detail: "ввод 398, вывод 312, рассуждение 248", TookMS: 2510,
						Response: "Комплект крепежа 320 ₽, в наличии 65 штук. Кронштейн стальной 890 ₽."},
				},
				prompt: 584, out: 396, reasoning: 300, took: 4040,
			}},
		},
	}

	random := rand.New(rand.NewSource(42))

	for _, item := range dialogues {
		conv := &store.Conversation{
			BotID: bot.ID, Token: fmt.Sprintf("demo-%d", random.Int63()),
			PageURL: item.page,
		}
		if err := db.CreateConversation(ctx, conv); err != nil {
			return err
		}
		if bot.Greeting != "" {
			if err := db.AddMessage(ctx, &store.Message{
				ConversationID: conv.ID, Role: "assistant", Text: bot.Greeting,
			}); err != nil {
				return err
			}
		}

		for _, t := range item.turns {
			if err := db.AddMessage(ctx, &store.Message{
				ConversationID: conv.ID, Role: "user", Text: t.user,
			}); err != nil {
				return err
			}
			answer := &store.Message{
				ConversationID: conv.ID, Role: "assistant", Text: t.answer,
			}
			if err := db.AddMessage(ctx, answer); err != nil {
				return err
			}
			receipt := &store.Receipt{
				MessageID: answer.ID, BotID: bot.ID, Provider: "openai",
				Model: "gpt-5-nano-2025-08-07", Steps: t.steps,
				PromptTokens: t.prompt, CompletionTokens: t.out,
				ReasoningTokens: t.reasoning, TookMS: t.took,
				CostMicro: int64(t.prompt)*bot.PriceIn/100 +
					int64(t.out)*bot.PriceOut/100,
			}
			if err := db.SaveReceipt(ctx, receipt); err != nil {
				return err
			}
		}

		if item.escalated != "" {
			if err := db.SetConversationState(ctx, conv.ID, "waiting", item.escalated); err != nil {
				return err
			}
		}

		// Разводим разговоры по времени, иначе лента выглядит как один миг.
		stamp := time.Now().Add(-item.ago).UTC().Format("2006-01-02 15:04:05")
		if _, err := db.ExecContext(ctx,
			`UPDATE conversations SET created_at = ?, updated_at = ? WHERE id = ?`,
			stamp, stamp, conv.ID); err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx,
			`UPDATE receipts SET created_at = ? WHERE message_id IN
			 (SELECT id FROM messages WHERE conversation_id = ?)`, stamp, conv.ID); err != nil {
			return err
		}
	}

	fmt.Println("демонстрационные данные готовы")
	fmt.Println("вход: manager@example.com / hark")
	return nil
}
