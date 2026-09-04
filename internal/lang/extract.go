package lang

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Извлечение строк из исходников.
//
// Живёт в обычном файле, а не в тесте, потому что нужно двоим: проверке
// полноты и команде, которая готовит файл переводчику. Ключ перевода — сама
// русская строка, поэтому «извлечь» здесь значит найти места, где её пишут.

var (
	// {{t $.L `строка`}} и {{t $L `строка`}} в шаблонах.
	reTemplate = regexp.MustCompile("\\{\\{-?\\s*t\\s+\\$[A-Za-z.]*\\s+`([^`]+)`")
	// lang.T(код, "строка") в коде. Строка идёт вторым доводом и может
	// продолжаться конкатенацией на следующей строке.
	reGo   = regexp.MustCompile(`lang\.T\([^,]+,\s*((?:"(?:[^"\\]|\\.)*"\s*\+?\s*)+)`)
	rePart = regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`)
	// Формы множественного числа перечислены прямо в шаблоне и уходят в
	// перевод через переменную, поэтому обычным поиском по lang.T их не видно.
	rePlural = regexp.MustCompile(`\{\{-?\s*plural\s+\$[A-Za-z.]*\s+[^"]*((?:"[^"]*"\s*){2,3})`)
)

// Sources обходит дерево и собирает все строки, которые проходят через
// перевод. Корень — каталог проекта.
func Sources(root string) ([]string, error) {
	found := map[string]bool{}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "node_modules", "out", "screenshots", "video", "post":
				return filepath.SkipDir
			}
			return nil
		}

		switch {
		case strings.HasSuffix(path, ".html"):
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, m := range reTemplate.FindAllStringSubmatch(string(raw), -1) {
				found[m[1]] = true
			}
			for _, m := range rePlural.FindAllStringSubmatch(string(raw), -1) {
				for _, part := range rePart.FindAllStringSubmatch(m[1], -1) {
					found[part[1]] = true
				}
			}

		case strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go"):
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, m := range reGo.FindAllStringSubmatch(string(raw), -1) {
				var b strings.Builder
				for _, part := range rePart.FindAllStringSubmatch(m[1], -1) {
					b.WriteString(unquote(part[1]))
				}
				if s := b.String(); s != "" {
					found[s] = true
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	out := make([]string, 0, len(found))
	for s := range found {
		out = append(out, s)
	}
	sort.Strings(out)
	return out, nil
}

// unquote разворачивает экранирование внутри строкового литерала Go. Полный
// разбор тут не нужен: в подписях встречаются только перевод строки и кавычка.
func unquote(s string) string {
	return strings.NewReplacer(`\n`, "\n", `\t`, "\t", `\"`, `"`, `\\`, `\`).Replace(s)
}
