package chat

import (
	"context"
	"testing"

	"github.com/dripips/hark/internal/llm"
)

// Посетитель может закрыть вкладку ровно в ту секунду, когда бот сдаётся.
// Контекст запроса при этом отменяется на середине записи.
//
// Раньше это давало худший из исходов: реплика «Передаю разговор менеджеру»
// уже в базе и уже у посетителя, а разговор оставался в состоянии open — то
// есть в очередь не попадал и на глаза менеджеру не показывался. Человека
// пообещали и не позвали.
func TestЭскалацияПереживаетЗакрытуюВкладку(t *testing.T) {
	db, bot, conv := setup(t)
	// Пустой ответ модели — тот самый путь, где бот сдаётся сам.
	withFake(t, &fakeProvider{turns: []llm.Response{{Text: ""}}})

	engine := &Engine{DB: db}

	ctx, cancel := context.WithCancel(context.Background())
	// Отменяем до вызова: так ведёт себя запрос, оборванный на полуслове.
	cancel()

	reply, err := engine.Answer(ctx, bot, conv, nil)
	if err != nil {
		t.Fatalf("ответ не собрался: %v", err)
	}
	if !reply.Escalated {
		t.Fatal("бот не сдался там, где должен был")
	}

	again, err := db.ConversationByID(context.Background(), conv.ID)
	if err != nil {
		t.Fatalf("разговор не читается: %v", err)
	}
	if again.State != "waiting" {
		t.Fatalf("разговор в состоянии %q, а посетителю уже пообещали человека", again.State)
	}
	if again.EscalateReason == "" {
		t.Error("причина не записана: менеджер не поймёт, зачем его позвали")
	}
	if !again.EscalatedAt.Valid {
		t.Error("время эскалации не записано: не посчитать, сколько человек ждёт")
	}

	// Реплика тоже должна быть на месте: посетитель её уже видел.
	messages, err := db.Messages(context.Background(), conv.ID)
	if err != nil {
		t.Fatalf("сообщения не читаются: %v", err)
	}
	if len(messages) == 0 {
		t.Fatal("реплика не сохранилась")
	}
}

// Обычный путь не должен пострадать от починки: когда всё в порядке,
// разговор так же уходит в очередь.
func TestЭскалацияПоЗовуИнструмента(t *testing.T) {
	db, bot, conv := setup(t)
	withFake(t, &fakeProvider{turns: []llm.Response{
		{ToolCalls: []llm.ToolCall{{
			ID: "1", Name: "call_human", Arguments: `{"reason":"нужен доступ к возвратам"}`,
		}}},
		{Text: "Подключаю менеджера."},
	}})

	reply, err := (&Engine{DB: db}).Answer(context.Background(), bot, conv, nil)
	if err != nil {
		t.Fatalf("ответ: %v", err)
	}
	if !reply.Escalated {
		t.Fatal("зов инструмента не привёл к эскалации")
	}

	again, _ := db.ConversationByID(context.Background(), conv.ID)
	if again.State != "waiting" {
		t.Fatalf("состояние %q", again.State)
	}
	if again.EscalateReason != "нужен доступ к возвратам" {
		t.Errorf("причина = %q", again.EscalateReason)
	}
}
