package lang

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Корень проекта: тест ходит по исходникам, а рабочий каталог у него свой.
func projectRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("не удалось определить путь к тесту")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// Словарь должен покрывать все строки, которые проходят через перевод.
//
// Ключ перевода — сама русская строка, и правка формулировки рвёт связь молча:
// на экране останется русский текст, а заметит это только тот, кто читает не
// на русском. Поэтому сверка живёт в тестах и падает громко.
func TestСловариПокрываютВсеСтроки(t *testing.T) {
	sources, err := Sources(projectRoot(t))
	if err != nil {
		t.Fatalf("обход исходников: %v", err)
	}
	if len(sources) < 100 {
		t.Fatalf("найдено всего %d строк — похоже, извлечение сломалось", len(sources))
	}

	for _, code := range Codes() {
		if code == Source {
			continue
		}
		if missing := Missing(code, sources); len(missing) > 0 {
			t.Errorf("в %s.json нет %d строк:\n  %s",
				code, len(missing), strings.Join(missing, "\n  "))
		}
	}
}

// Осиротевшая строка словаря — след правки формулировки. Сама по себе она
// безвредна, но копится и превращает файл в свалку, где не видно настоящих
// пропусков.
func TestВСловаряхНетЛишнего(t *testing.T) {
	sources, err := Sources(projectRoot(t))
	if err != nil {
		t.Fatalf("обход исходников: %v", err)
	}

	for _, code := range Codes() {
		if code == Source {
			continue
		}
		if extra := Orphans(code, sources); len(extra) > 0 {
			t.Errorf("в %s.json %d строк, которых больше нет в коде:\n  %s",
				code, len(extra), strings.Join(extra, "\n  "))
		}
	}
}

// Подстановки должны совпадать: перевод, потерявший %s, выведет мусор вместо
// значения, а лишний %d уронит вывод в «%!d(MISSING)».
func TestПодстановкиСовпадаютСИсточником(t *testing.T) {
	sources, err := Sources(projectRoot(t))
	if err != nil {
		t.Fatalf("обход исходников: %v", err)
	}

	count := func(s string) int {
		n := 0
		for i := 0; i < len(s)-1; i++ {
			if s[i] == '%' {
				if s[i+1] == '%' {
					i++
					continue
				}
				n++
			}
		}
		return n
	}

	for _, code := range Codes() {
		if code == Source {
			continue
		}
		for _, src := range sources {
			got := T(code, src)
			if got == src {
				continue
			}
			if want, have := count(src), count(got); want != have {
				t.Errorf("%s: подстановок было %d, стало %d\n  было: %s\n  стало: %s",
					code, want, have, src, got)
			}
		}
	}
}
