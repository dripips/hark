// Команда locale готовит файл переводчику.
//
//	go run ./cmd/locale          что есть и чего не хватает
//	go run ./cmd/locale fr       заготовка нового языка в stdout
//	go run ./cmd/locale en       чего не хватает в существующем
//
// Ключ перевода — сама русская строка, поэтому «собрать файл» здесь значит
// обойти шаблоны и код и выписать всё, что проходит через lang.T.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/dripips/hark/internal/lang"
)

func main() {
	sources, err := lang.Sources(".")
	if err != nil {
		fmt.Fprintln(os.Stderr, "не удалось обойти исходники:", err)
		os.Exit(1)
	}

	if len(os.Args) < 2 {
		fmt.Printf("строк в интерфейсе: %d\n\n", len(sources))
		for _, code := range lang.Codes() {
			if code == lang.Source {
				fmt.Printf("  %-4s %-10s источник, словарь не нужен\n", code, lang.Title(code))
				continue
			}
			missing := lang.Missing(code, sources)
			orphans := lang.Orphans(code, sources)
			fmt.Printf("  %-4s %-10s переведено %d из %d",
				code, lang.Title(code), len(sources)-len(missing), len(sources))
			if len(orphans) > 0 {
				fmt.Printf(", лишних %d", len(orphans))
			}
			fmt.Println()
		}
		fmt.Println("\nЗаготовка нового языка: go run ./cmd/locale <код> > internal/lang/locales/<код>.json")
		return
	}

	code := os.Args[1]
	skeleton := map[string]string{}
	for _, source := range sources {
		// Уже переведённое переносим, чтобы файл можно было пересобрать
		// поверх себя и не потерять работу.
		skeleton[source] = lang.T(code, source)
		if skeleton[source] == source && code != lang.Source {
			skeleton[source] = ""
		}
	}

	out, err := json.MarshalIndent(skeleton, "", " ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "не удалось собрать файл:", err)
		os.Exit(1)
	}
	os.Stdout.Write(append(out, '\n'))

	done := 0
	for _, v := range skeleton {
		if v != "" {
			done++
		}
	}
	fmt.Fprintf(os.Stderr, "%s: заполнено %d из %d\n", code, done, len(skeleton))
}
