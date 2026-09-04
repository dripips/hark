package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func разговор(t *testing.T, db *DB, ctx context.Context, token string) *Conversation {
	t.Helper()
	bot := &Bot{Slug: "shop-" + token, Name: "Магазин", Enabled: true}
	if err := db.SaveBot(ctx, bot); err != nil {
		t.Fatalf("бот: %v", err)
	}
	conv := &Conversation{BotID: bot.ID, Token: token}
	if err := db.CreateConversation(ctx, conv); err != nil {
		t.Fatalf("разговор: %v", err)
	}
	return conv
}

// Двое открывают ждущий разговор одновременно и жмут «беру» вместе. Выиграть
// должен ровно один, иначе посетитель получит два ответа от разных людей.
func TestВзятьРазговорМожетТолькоОдин(t *testing.T) {
	db, ctx := открытьБазу(t)
	conv := разговор(t, db, ctx, "гонка")

	ирина := завести(t, db, ctx, "irina@example.com")
	пётр := завести(t, db, ctx, "petr@example.com")

	var wg sync.WaitGroup
	results := make([]error, 2)
	claimers := []int64{ирина.ID, пётр.ID}

	wg.Add(2)
	for i := range claimers {
		go func(i int) {
			defer wg.Done()
			results[i] = db.Claim(ctx, conv.ID, claimers[i])
		}(i)
	}
	wg.Wait()

	won := 0
	for _, err := range results {
		if err == nil {
			won++
		} else if !errors.Is(err, ErrAlreadyClaimed) {
			t.Fatalf("неожиданная ошибка: %v", err)
		}
	}
	if won != 1 {
		t.Fatalf("разговор взяли %d раз, ждали ровно один", won)
	}

	name, ok := db.ClaimedBy(ctx, conv.ID)
	if !ok || name == "" {
		t.Fatal("после взятия имя не читается")
	}
}

// Повторное нажатие тем же человеком — не гонка, а просто повтор.
func TestПовторноеВзятиеСвоегоРазговораНеОшибка(t *testing.T) {
	db, ctx := открытьБазу(t)
	conv := разговор(t, db, ctx, "повтор")
	ирина := завести(t, db, ctx, "irina@example.com")

	if err := db.Claim(ctx, conv.ID, ирина.ID); err != nil {
		t.Fatalf("первое взятие: %v", err)
	}
	if err := db.Claim(ctx, conv.ID, ирина.ID); err != nil {
		t.Fatalf("своё же взятие отклонено: %v", err)
	}
}

// Отпустить может любой: взявший мог уйти на обед, и запирать посетителя за
// ушедшим — худшее из возможного.
func TestОтпуститьМожетЛюбой(t *testing.T) {
	db, ctx := открытьБазу(t)
	conv := разговор(t, db, ctx, "обед")
	ирина := завести(t, db, ctx, "irina@example.com")
	пётр := завести(t, db, ctx, "petr@example.com")

	if err := db.Claim(ctx, conv.ID, ирина.ID); err != nil {
		t.Fatalf("взятие: %v", err)
	}
	if err := db.Release(ctx, conv.ID); err != nil {
		t.Fatalf("отпускание: %v", err)
	}
	if _, ok := db.ClaimedBy(ctx, conv.ID); ok {
		t.Fatal("разговор всё ещё за кем-то")
	}
	if err := db.Claim(ctx, conv.ID, пётр.ID); err != nil {
		t.Fatalf("отпущенный не берётся: %v", err)
	}
}

// Удалённый менеджер оставляет в колонке висячий номер. Внутреннее соединение
// спрятало бы такой разговор из списка целиком — а он ждёт человека.
func TestРазговорУволенногоНеПропадает(t *testing.T) {
	db, ctx := открытьБазу(t)
	conv := разговор(t, db, ctx, "уволенный")
	ушёл := завести(t, db, ctx, "leaves@example.com")
	завести(t, db, ctx, "stays@example.com")

	if err := db.Claim(ctx, conv.ID, ушёл.ID); err != nil {
		t.Fatalf("взятие: %v", err)
	}
	if err := db.DeleteManager(ctx, ушёл.ID); err != nil {
		t.Fatalf("удаление: %v", err)
	}

	again, err := db.ConversationByID(ctx, conv.ID)
	if err != nil {
		t.Fatalf("разговор пропал: %v", err)
	}
	if again.ID != conv.ID {
		t.Fatal("прочитался не тот разговор")
	}
	if _, ok := db.ClaimedBy(ctx, conv.ID); ok {
		t.Error("показываем имя того, кого больше нет")
	}

	names, err := db.ClaimNames(ctx)
	if err != nil {
		t.Fatalf("имена: %v", err)
	}
	if _, ok := names[conv.ID]; ok {
		t.Error("в списке осталось имя удалённого")
	}
}

// Счётчик подключений считает и общее число, и включённые: выключенное
// подключение в списке видно, но бот им не пользуется.
func TestСчётчикПодключений(t *testing.T) {
	db, ctx := открытьБазу(t)
	bot := &Bot{Slug: "shop", Name: "Магазин", Enabled: true}
	if err := db.SaveBot(ctx, bot); err != nil {
		t.Fatal(err)
	}

	for i, enabled := range []bool{true, true, false} {
		tool := &Tool{
			// Имя подключения единственно в пределах бота: так его зовёт модель.
			BotID: bot.ID, Kind: "http", Name: fmt.Sprintf("tool_%d", i), Description: "d",
			Method: "GET", URL: "https://example.com", Enabled: enabled,
			Headers: "{}", Parameters: "{}",
		}
		if err := db.SaveTool(ctx, tool); err != nil {
			t.Fatalf("подключение: %v", err)
		}
	}

	counts, err := db.ToolCounts(ctx)
	if err != nil {
		t.Fatalf("счётчик: %v", err)
	}
	got := counts[bot.ID]
	if got[0] != 3 || got[1] != 2 {
		t.Fatalf("получили всего %d, включено %d; ждали 3 и 2", got[0], got[1])
	}

	// Бот без подключений в карте отсутствовать может, но не врать.
	empty := &Bot{Slug: "bare", Name: "Голый", Enabled: true}
	_ = db.SaveBot(ctx, empty)
	if counts, _ := db.ToolCounts(ctx); counts[empty.ID][0] != 0 {
		t.Fatalf("у бота без подключений насчитали %d", counts[empty.ID][0])
	}
}
