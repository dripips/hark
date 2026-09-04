package web

import (
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/dripips/hark/internal/lang"
	"github.com/dripips/hark/internal/store"
)

func (s *Server) parseTemplates() error {
	funcs := template.FuncMap{
		// Перевод подписи. Ключ — сама русская строка: см. internal/lang.
		"t": lang.T,
		// Пара «язык плюс значение» для вложенных шаблонов, которым на вход
		// приходит не корень и $ внутри указывает не туда.
		// Название языка на нём самом: список на чужом языке бесполезен
		// ровно тому, кто ищет в нём свой.
		"langTitle": lang.Title,
		"pair": func(l string, v any) map[string]any {
			return map[string]any{"L": l, "V": v}
		},
		// Деньги хранятся в микрорублях. Один ответ стоит доли копейки, а
		// месячный итог — сотни рублей, поэтому точность выбирается по величине:
		// иначе либо всё нули, либо хвост из шести знаков.
		"money": func(micro int64) string {
			switch {
			case micro == 0:
				return "0 ₽"
			case micro >= 1_000_000:
				return fmt.Sprintf("%d,%02d ₽", micro/1_000_000, (micro%1_000_000)/10_000)
			case micro >= 10_000:
				return fmt.Sprintf("0,%02d ₽", micro/10_000)
			default:
				// Разделитель запятая, как принято в русском формате.
				return strings.Replace(
					fmt.Sprintf("%.4f ₽", float64(micro)/1_000_000), ".", ",", 1)
			}
		},
		"ms": func(value int64) string {
			if value < 1000 {
				return fmt.Sprintf("%d мс", value)
			}
			return fmt.Sprintf("%.1f с", float64(value)/1000)
		},
		"when": func(raw string) string {
			parsed, err := time.Parse("2006-01-02 15:04:05", raw)
			if err != nil {
				if parsed, err = time.Parse(time.RFC3339, raw); err != nil {
					return raw
				}
			}
			return parsed.Format("02.01.2006 15:04")
		},
		"stateName": func(l, state string) string {
			switch state {
			case "waiting":
				return lang.T(l, "ждёт человека")
			case "human":
				return lang.T(l, "отвечает человек")
			case "closed":
				return lang.T(l, "закрыт")
			default:
				return lang.T(l, "ведёт бот")
			}
		},
		"roleName": func(l, role string) string {
			switch role {
			case "user":
				return lang.T(l, "посетитель")
			case "human":
				return lang.T(l, "менеджер")
			case "assistant":
				return lang.T(l, "бот")
			default:
				return role
			}
		},
		"stepName": func(l, kind string) string {
			switch kind {
			case "model":
				return lang.T(l, "модель")
			case "tool":
				return lang.T(l, "подключение")
			default:
				return lang.T(l, "сбой")
			}
		},
		// Множественное число. Формы перечислены по-русски: их три, и они же
		// служат ключами перевода. В языке с двумя формами перевод у «дня» и
		// «дней» просто совпадёт — это дешевле, чем таблица правил на язык.
		"plural": func(l string, n int, one, few, many string) string {
			mod100, mod10 := n%100, n%10
			switch {
			case mod100 >= 11 && mod100 <= 14:
				return lang.T(l, many)
			case mod10 == 1:
				return lang.T(l, one)
			case mod10 >= 2 && mod10 <= 4:
				return lang.T(l, few)
			default:
				return lang.T(l, many)
			}
		},
		"trimTo": func(limit int, s string) string { return store.Clip(s, limit) },
		"lines":  func(s string) []string { return strings.Split(s, "\n") },
		"sub":    func(a, b int) int { return a - b },
		// Исход зова наружу начинается со слова «доставлено», когда он удался:
		// по нему страница выбирает, красить плашку зелёным или красным.
		"hasPrefix": strings.HasPrefix,
	}

	parsed, err := template.New("").Funcs(funcs).ParseFS(assets, "templates/*.html")
	if err != nil {
		return err
	}
	s.templates = parsed
	return nil
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	data["Manager"] = managerName(r)
	data["Path"] = r.URL.Path
	data["L"] = language(r)
	data["Languages"] = lang.Codes()

	// Счётчик ожидающих считает сервер при каждом показе страницы, а не
	// хранит рядом. Индекс по (state, updated_at) уже есть, запрос дешёвый,
	// зато цифра верна и с выключенным JavaScript, и сразу после перезапуска.
	if currentManager(r) != nil {
		if _, ok := data["Waiting"]; !ok {
			count, _ := s.DB.CountWaiting(r.Context())
			data["Waiting"] = count
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "ошибка шаблона: "+err.Error(), http.StatusInternalServerError)
	}
}
