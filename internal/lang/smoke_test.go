package lang

import "testing"

// Дым: словари читаются, отступление работает, подстановка работает.
func TestСловариЧитаются(t *testing.T) {
	if err := Err(); err != nil {
		t.Fatalf("словари не прочитались: %v", err)
	}
	if len(Codes()) < 2 {
		t.Fatalf("языков всего %d: %v", len(Codes()), Codes())
	}
}

func TestИсточникРаботаетБезСловаря(t *testing.T) {
	if got := T("ru", "Ждут человека"); got != "Ждут человека" {
		t.Fatalf("источник исказился: %q", got)
	}
}

// Непереведённое остаётся русским, а не превращается в пустоту или в ключ.
func TestПропущенныйПереводОстаётсяРусским(t *testing.T) {
	if got := T("en", "Такой строки в словаре нет"); got != "Такой строки в словаре нет" {
		t.Fatalf("получили %q", got)
	}
	if got := T("несуществующий язык", "Ждут человека"); got != "Ждут человека" {
		t.Fatalf("неизвестный язык сломал вывод: %q", got)
	}
}

func TestПодстановка(t *testing.T) {
	if got := T("ru", "Менеджер %s заведён", "ivan@example.com"); got != "Менеджер ivan@example.com заведён" {
		t.Fatalf("получили %q", got)
	}
}

func TestЯзыкИзЗаголовкаБраузера(t *testing.T) {
	for header, want := range map[string]string{
		"en-US,en;q=0.9,ru;q=0.8": "en",
		"ru-RU,ru;q=0.9":          "ru",
		"fr-FR,fr;q=0.9":          "ru",
		"":                        "ru",
	} {
		if got := FromHeader(header); got != want {
			t.Errorf("%q → %q, ждали %q", header, got, want)
		}
	}
}
