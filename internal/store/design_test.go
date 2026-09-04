package store

import (
	"encoding/json"
	"strings"
	"testing"
)

func design(theme string) Design {
	return Bot{Theme: theme}.Design()
}

// Значения темы попадают прямо в текст CSS виджета. Всё, что не цвет, не
// должно доезжать до него в принципе.
func TestДизайнОтсекаетПодстановкуВCSS(t *testing.T) {
	cases := []struct {
		имя   string
		тема  string
		поле  func(Design) string
		плохо string
	}{
		{
			имя:   "цвет с закрывающей скобкой",
			тема:  `{"accent":"#fff;} .hk__panel{display:none} .x{a:b"}`,
			поле:  func(d Design) string { return d.Accent },
			плохо: "}",
		},
		{
			имя:   "название цвета вместо кода",
			тема:  `{"surface":"red"}`,
			поле:  func(d Design) string { return d.Surface },
			плохо: "red",
		},
		{
			имя:   "свой шрифт с кавычкой",
			тема:  `{"font":"custom","font_stack":"Bad\") ; } .hk{display:none} .z{a:(\""}`,
			поле:  func(d Design) string { return d.FontStack },
			плохо: "{",
		},
		{
			имя:   "картинка по javascript",
			тема:  `{"backdrop_image":"javascript:alert(1)"}`,
			поле:  func(d Design) string { return d.BackdropImage },
			плохо: "javascript",
		},
		{
			имя:   "шрифт по незащищённому протоколу",
			тема:  `{"font_url":"http://evil.example.com/x.css"}`,
			поле:  func(d Design) string { return d.FontURL },
			плохо: "http://",
		},
	}

	for _, c := range cases {
		t.Run(c.имя, func(t *testing.T) {
			got := c.поле(design(c.тема))
			if strings.Contains(got, c.плохо) {
				t.Fatalf("в теме осталось %q: %q", c.плохо, got)
			}
		})
	}
}

func TestДизайнЗагоняетЧислаВГраницы(t *testing.T) {
	d := design(`{"font_size":9999,"width":-50,"height":100000,"offset":0,"radius":999,"backdrop_angle":-7}`)

	for _, c := range []struct {
		имя      string
		значение int
		от, до   int
	}{
		{"кегль", d.FontSize, 13, 19},
		{"ширина", d.Width, 320, 460},
		{"высота", d.Height, 420, 680},
		{"отступ", d.Offset, 8, 48},
		{"скругление", d.Radius, 0, 28},
		{"угол", d.BackdropAngle, 0, 360},
	} {
		if c.значение < c.от || c.значение > c.до {
			t.Errorf("%s = %d, ждали от %d до %d", c.имя, c.значение, c.от, c.до)
		}
	}
}

func TestДизайнОтбрасываетНеизвестныеВарианты(t *testing.T) {
	d := design(`{"scheme":"bogus","shadow":"'; drop","header_kind":"../..","density":"x","backdrop_kind":"y","font":"z"}`)

	for _, c := range []struct{ имя, было, ждём string }{
		{"схема", d.Scheme, "light"},
		{"тень", d.Shadow, "soft"},
		{"шапка", d.HeaderKind, "accent"},
		{"плотность", d.Density, "normal"},
		{"фон", d.BackdropKind, "solid"},
		{"шрифт", d.Font, "system"},
	} {
		if c.было != c.ждём {
			t.Errorf("%s = %q, ждали %q", c.имя, c.было, c.ждём)
		}
	}
}

// Подпись не в UTF-8 — это сбой переноса, а не текст. В чужой шапке она
// превратилась бы в цепочку ромбов.
func TestДизайнОтбрасываетБитуюПодпись(t *testing.T) {
	broken := Design{Subtitle: "\xd0\x9e\xd1\x82\xd0"}
	raw, _ := json.Marshal(map[string]string{"subtitle": "ok"})
	_ = raw

	if got := clip(broken.Subtitle, 80); got != "" {
		t.Fatalf("битая подпись прошла: %q", got)
	}
	if got := clip("Отвечаем круглосуточно", 80); got != "Отвечаем круглосуточно" {
		t.Fatalf("целая подпись потерялась: %q", got)
	}
	if got := clip(strings.Repeat("я", 200), 80); len([]rune(got)) != 80 {
		t.Fatalf("длина обрезана до %d рун", len([]rune(got)))
	}
}

// Приглушённый текст, линии и фон реплики выводятся из полотна и текста.
// Иначе бежевая панель получала бы серые линии от прошлой темы.
func TestПроизводныеЦветаСледуютЗаПолотном(t *testing.T) {
	светлая := design(`{"surface":"#ffffff","text":"#14161a"}`)
	тёмная := design(`{"surface":"#101014","text":"#f0f0f4"}`)

	if luma(светлая.Border) < luma(светлая.Text) {
		t.Error("на светлом полотне линии должны быть светлее текста")
	}
	if luma(тёмная.Border) > luma(тёмная.Text) {
		t.Error("на тёмном полотне линии должны быть темнее текста")
	}
	if светлая.BotBubble == тёмная.BotBubble {
		t.Error("фон реплики не изменился вслед за полотном")
	}
}

// Заданный цвет производные не трогают: сняв «авто», владелец забирает его себе.
func TestЗаданныйЦветПобеждаетПроизводный(t *testing.T) {
	d := design(`{"surface":"#ffffff","text":"#14161a","muted":"#ff00ff"}`)
	if d.Muted != "#ff00ff" {
		t.Fatalf("приглушённый = %q, ждали заданный #ff00ff", d.Muted)
	}
}

// Один цвет служит и заливкой, и надписью. Как заливка бледный акцент ещё
// читается, как текст — уже нет.
func TestЧернилаДотягиваютКонтрастДоНормы(t *testing.T) {
	d := design(`{"accent":"#ffe066","surface":"#ffffff"}`)

	if contrast(d.Accent, d.Surface) >= 4.5 {
		t.Skip("подопытный акцент оказался достаточно тёмным")
	}
	if got := contrast(d.Ink(d.Surface), d.Surface); got < 4.5 {
		t.Fatalf("контраст чернил %.2f, нужно не меньше 4,5", got)
	}
}

func TestНадписьНаЗаливкеБерётКонтрастнуюПару(t *testing.T) {
	светлый := design(`{"accent":"#ffe066"}`)
	тёмный := design(`{"accent":"#101820"}`)

	if contrast(светлый.OnAccent, светлый.Accent) < 4.5 {
		t.Errorf("на жёлтой заливке надпись нечитаема: %q", светлый.OnAccent)
	}
	if тёмный.OnAccent != "#ffffff" {
		t.Errorf("на тёмной заливке ждали белую надпись, получили %q", тёмный.OnAccent)
	}
}

// Сохраняем вычищенное: иначе мусор лежит в базе до первого чтения.
func TestВычищеннаяТемаСноваЧитается(t *testing.T) {
	once := design(`{"accent":"#8a5a2b","surface":"#fffaf3","font_size":16,"scheme":"auto"}`)
	twice := design(once.Sanitized())

	if twice.Accent != once.Accent || twice.Surface != once.Surface ||
		twice.FontSize != once.FontSize || twice.Scheme != once.Scheme {
		t.Fatalf("тема изменилась после круга через JSON:\n%+v\n%+v", once, twice)
	}
}

func TestСвойШрифтВсегдаССистемнымХвостом(t *testing.T) {
	stack := design(`{"font":"custom","font_stack":"Manrope"}`).Stack()
	if !strings.HasPrefix(stack, "Manrope, ") {
		t.Fatalf("свой шрифт не первый: %q", stack)
	}
	if !strings.Contains(stack, "sans-serif") {
		t.Fatalf("нет запасного шрифта: %q", stack)
	}
	if empty := design(`{"font":"custom"}`).Stack(); !strings.Contains(empty, "-apple-system") {
		t.Fatalf("пустой свой шрифт не откатился к системному: %q", empty)
	}
}

// Скругление живёт и в теме, и в старой колонке. Тема главнее, колонка —
// запасной вариант для ботов, заведённых до неё.
func TestСкруглениеБерётсяИзТемыИлиИзКолонки(t *testing.T) {
	if got := (Bot{CornerRadius: 8}).Design().Radius; got != 8 {
		t.Errorf("без темы ждали скругление из колонки, получили %d", got)
	}
	if got := (Bot{CornerRadius: 8, Theme: `{"radius":24}`}).Design().Radius; got != 24 {
		t.Errorf("с темой ждали 24, получили %d", got)
	}
}
