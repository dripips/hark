// Пакет lang переводит подписи интерфейса.
//
// Ключ перевода — сама русская строка, как в gettext. Не «nav.bots», а
// «Разговоры». Решение неочевидное, поэтому вот причины.
//
// Шаблон остаётся читаемым: в нём видно, что выведется, без похода в словарь.
// Русский работает вообще без словаря — он источник. Пропущенный перевод
// оставляет на экране русскую фразу, а не имя ключа: непереведённое лучше
// сломанного. И разметка существующего кода становится механической заменой,
// а не выдумыванием четырёхсот имён, половина которых потом соврёт.
//
// Цена решения одна: правка русской формулировки рвёт связь с переводом. Её
// ловит проверка полноты в lang_test.go — она сверяет вызовы в коде со
// словарями и показывает осиротевшие строки.
//
// Добавить язык — положить рядом locales/<код>.json со словарём
// «русская строка → перевод». Ни кода, ни пересборки, ни внешних служб.
package lang

import (
	"embed"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
)

//go:embed locales/*.json
var files embed.FS

// Source — язык, на котором написан продукт. Словаря у него нет и не нужно.
const Source = "ru"

type catalogue map[string]string

var (
	once    sync.Once
	loaded  map[string]catalogue
	loadErr error
	codes   []string

	// Название языка на нём самом: список, написанный на чужом языке,
	// бесполезен ровно тому, кто в нём ищет свой.
	titles = map[string]string{
		"ru": "Русский",
		"en": "English",
		"de": "Deutsch",
	}
)

func load() {
	loaded = map[string]catalogue{Source: {}}
	codes = []string{Source}

	entries, err := files.ReadDir("locales")
	if err != nil {
		loadErr = err
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := files.ReadFile(path.Join("locales", entry.Name()))
		if err != nil {
			loadErr = fmt.Errorf("%s: %w", entry.Name(), err)
			return
		}
		var c catalogue
		if err := json.Unmarshal(raw, &c); err != nil {
			loadErr = fmt.Errorf("%s: %w", entry.Name(), err)
			return
		}
		code := strings.TrimSuffix(entry.Name(), ".json")
		if code == Source {
			continue
		}
		loaded[code] = c
		codes = append(codes, code)
	}
	sort.Strings(codes)
}

func ensure() { once.Do(load) }

// Err отдаёт ошибку разбора словарей. Проверяется при старте: сломанный JSON
// должен ронять запуск, а не всплывать через неделю пустой подписью.
func Err() error {
	ensure()
	return loadErr
}

// Codes — доступные языки, источник первым.
func Codes() []string {
	ensure()
	out := make([]string, len(codes))
	copy(out, codes)
	return out
}

func Title(code string) string {
	if t, ok := titles[code]; ok {
		return t
	}
	return code
}

func Known(code string) bool {
	ensure()
	_, ok := loaded[code]
	return ok
}

// Pick приводит что угодно к известному языку.
func Pick(code string) string {
	if Known(code) {
		return code
	}
	return Source
}

// T переводит строку. Лишние аргументы подставляются как в fmt.Sprintf.
//
//	lang.T(l, "Ждут человека")
//	lang.T(l, "Менеджер %s заведён", email)
func T(code, source string, args ...any) string {
	ensure()

	text := source
	if code != Source {
		if got, ok := loaded[Pick(code)][source]; ok && got != "" {
			text = got
		}
	}
	if len(args) == 0 {
		return text
	}
	return fmt.Sprintf(text, args...)
}

// Missing перечисляет строки, которых нет в словаре языка. Нужна проверке
// полноты и команде, которая готовит файл переводчику.
func Missing(code string, sources []string) []string {
	ensure()
	if code == Source {
		return nil
	}
	c := loaded[Pick(code)]

	var out []string
	seen := map[string]bool{}
	for _, s := range sources {
		if seen[s] {
			continue
		}
		seen[s] = true
		if got, ok := c[s]; !ok || got == "" {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// Orphans перечисляет строки словаря, которых больше нет в коде. Так видно
// правки формулировок, порвавшие связь с переводом.
func Orphans(code string, sources []string) []string {
	ensure()
	live := map[string]bool{}
	for _, s := range sources {
		live[s] = true
	}

	var out []string
	for s := range loaded[Pick(code)] {
		if !live[s] {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// FromHeader выбирает язык по заголовку браузера.
//
// Разбор грубый: нужен код из двух букв, а не полная поддержка весов из
// RFC 9110. Промахнёмся — человек выберет язык руками, и это дешевле сотни
// строк разбора ради того же результата.
func FromHeader(header string) string {
	for _, part := range strings.Split(header, ",") {
		code := strings.TrimSpace(part)
		if i := strings.Index(code, ";"); i >= 0 {
			code = code[:i]
		}
		if i := strings.Index(code, "-"); i >= 0 {
			code = code[:i]
		}
		if code = strings.ToLower(strings.TrimSpace(code)); Known(code) {
			return code
		}
	}
	return Source
}
