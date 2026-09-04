package notify

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func событие() Event {
	return Event{
		BotID: 1, BotName: "Магазин", BotSlug: "shop",
		Reason: "не нашёл заказ", ConvID: 412,
		AdminURL: "https://hark.example.com/conversations/412",
		PageURL:  "https://shop.example.com/delivery",
		Waiting:  3, At: time.Unix(1788500000, 0),
	}
}

// Причину пишет модель. Кавычка в ней ломала бы тело JSON, амперсанд —
// дописывал бы лишний параметр в адрес.
func TestПричинаНеЛомаетНиАдресНиТело(t *testing.T) {
	event := событие()
	event.Reason = `нет "заказа" & сломано` + "\n" + `второй строкой`

	address, body, err := build(Target{
		URL:      "https://api.example.com/send?text={reason}&who={bot}",
		Template: `{"text":"{reason}","bot":"{bot}"}`,
	}, event)
	if err != nil {
		t.Fatalf("сборка: %v", err)
	}

	parsed, err := url.Parse(address)
	if err != nil {
		t.Fatalf("адрес не разобрался: %v", err)
	}
	if got := parsed.Query().Get("text"); got != event.Reason {
		t.Errorf("причина в адресе искажена:\n%q\n%q", got, event.Reason)
	}
	if len(parsed.Query()) != 2 {
		t.Errorf("в адресе %d параметров вместо двух: амперсанд просочился", len(parsed.Query()))
	}

	var decoded map[string]string
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("тело перестало быть JSON: %v\n%s", err, body)
	}
	if decoded["text"] != event.Reason {
		t.Errorf("причина в теле искажена: %q", decoded["text"])
	}
}

func TestБезШаблонаУходитСтандартноеТело(t *testing.T) {
	_, body, err := build(Target{URL: "https://api.example.com/hook"}, событие())
	if err != nil {
		t.Fatalf("сборка: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("не JSON: %v", err)
	}
	for _, key := range []string{"event", "bot", "reason", "conversation_id", "url", "waiting", "text"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("в стандартном теле нет %q", key)
		}
	}
	if decoded["event"] != "escalated" {
		t.Errorf("событие = %v", decoded["event"])
	}
	text, _ := decoded["text"].(string)
	if !strings.Contains(text, "Магазин") || !strings.Contains(text, "Ждут: 3") {
		t.Errorf("готовая строка бесполезна: %q", text)
	}
}

// Причину пишет модель, и она может пересказать слова посетителя. Галочка
// «без причины» должна убирать её отовсюду, а не из одного места.
func TestБезПричиныНеПропускаетЕёНикуда(t *testing.T) {
	address, body, err := build(Target{
		URL:      "https://api.example.com/send?text={text}&r={reason}",
		Quiet:    true,
		Template: `{"text":"{text}","reason":"{reason}"}`,
	}, событие())
	if err != nil {
		t.Fatalf("сборка: %v", err)
	}
	if strings.Contains(address, "%D0%BD%D0%B5+%D0%BD%D0%B0%D1%88") || strings.Contains(address, "заказ") {
		t.Errorf("причина просочилась в адрес: %s", address)
	}
	if strings.Contains(string(body), "не нашёл заказ") {
		t.Errorf("причина просочилась в тело: %s", body)
	}
	if !strings.Contains(string(body), "Магазин") {
		t.Errorf("вместе с причиной пропало всё остальное: %s", body)
	}
}

func TestАдресБезСхемыОтклоняется(t *testing.T) {
	for _, bad := range []string{"api.example.com/hook", "javascript:alert(1)", "file:///etc/passwd"} {
		if _, _, err := build(Target{URL: bad}, событие()); err == nil {
			t.Errorf("адрес %q прошёл", bad)
		}
	}
}

func TestДоставкаИОтветСервера(t *testing.T) {
	var got struct {
		method string
		body   string
		auth   string
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got.method, got.body, got.auth = r.Method, string(body), r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := New()
	status, _ := sender.Send(Target{
		URL:     server.URL,
		Headers: `{"Authorization":"Bearer секрет"}`,
	}, событие())

	if !strings.HasPrefix(status, "доставлено") {
		t.Fatalf("исход: %s", status)
	}
	if got.method != http.MethodPost {
		t.Errorf("способ %s", got.method)
	}
	if got.auth != "Bearer секрет" {
		t.Errorf("заголовок не дошёл: %q", got.auth)
	}
	if !strings.Contains(got.body, "Магазин") {
		t.Errorf("тело: %s", got.body)
	}
}

func TestОтказСервераВидноВИсходе(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "chat not found", http.StatusNotFound)
	}))
	defer server.Close()

	status, _ := New().Send(Target{URL: server.URL}, событие())
	if !strings.Contains(status, "404") {
		t.Fatalf("исход не назвал код: %s", status)
	}
}

// Переход по редиректу увёл бы заголовок с ключом на чужой хост.
func TestРедиректНеСледуется(t *testing.T) {
	var reached bool
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))
	defer elsewhere.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL, http.StatusFound)
	}))
	defer server.Close()

	status, _ := New().Send(Target{URL: server.URL, Headers: `{"Authorization":"Bearer секрет"}`}, событие())
	if reached {
		t.Fatal("запрос ушёл на чужой хост вместе с заголовком")
	}
	if strings.HasPrefix(status, "доставлено") {
		t.Fatalf("редирект посчитан успехом: %s", status)
	}
}

// Когда ложится поставщик модели, эскалирует каждый разговор подряд. Без
// ограничителя получился бы шторм зовов.
func TestШтормГаситсяОдинЗовНаБота(t *testing.T) {
	sender := New()
	if !sender.allow(1) {
		t.Fatal("первый зов не прошёл")
	}
	for i := 0; i < 20; i++ {
		if sender.allow(1) {
			t.Fatalf("зов %d прошёл сразу за первым", i)
		}
	}
	if !sender.allow(2) {
		t.Fatal("другой бот заперт чужим ограничителем")
	}
}

// Пустой адрес — это «выключено», и никакой очереди он занимать не должен.
func TestПустойАдресНеЗвонит(t *testing.T) {
	sender := New()
	sender.Fire(Target{URL: "   "}, событие())
	if len(sender.queue) != 0 {
		t.Fatalf("в очереди %d зовов", len(sender.queue))
	}
	if !ParseTarget(`{"url":""}`).Enabled() == false {
		t.Fatal("пустой адрес считается включённым")
	}
}

func TestРазборНастроекПодставляетСпособ(t *testing.T) {
	if got := ParseTarget("").Method; got != http.MethodPost {
		t.Errorf("по умолчанию %q", got)
	}
	if got := ParseTarget(`{"method":"GET","url":"https://x"}`).Method; got != http.MethodGet {
		t.Errorf("GET не сохранился: %q", got)
	}
	// Ничего, кроме GET и POST, мы не отправляем: остальное — опечатка.
	if got := ParseTarget(`{"method":"DELETE"}`).Method; got != http.MethodPost {
		t.Errorf("неизвестный способ прошёл: %q", got)
	}
	if ParseTarget("не json").Enabled() {
		t.Error("мусор в настройках включил зов")
	}
}
