package web

import (
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"
)

func (s *Server) parseTemplates() error {
	funcs := template.FuncMap{
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
		"stateName": func(state string) string {
			switch state {
			case "waiting":
				return "ждёт человека"
			case "human":
				return "отвечает человек"
			case "closed":
				return "закрыт"
			default:
				return "ведёт бот"
			}
		},
		"roleName": func(role string) string {
			switch role {
			case "user":
				return "посетитель"
			case "human":
				return "менеджер"
			case "assistant":
				return "бот"
			default:
				return role
			}
		},
		"stepName": func(kind string) string {
			switch kind {
			case "model":
				return "модель"
			case "tool":
				return "инструмент"
			default:
				return "сбой"
			}
		},
		"plural": func(n int, one, few, many string) string {
			mod100, mod10 := n%100, n%10
			switch {
			case mod100 >= 11 && mod100 <= 14:
				return many
			case mod10 == 1:
				return one
			case mod10 >= 2 && mod10 <= 4:
				return few
			default:
				return many
			}
		},
		"trimTo": func(limit int, s string) string {
			if len(s) <= limit {
				return s
			}
			return s[:limit] + "…"
		},
		"lines": func(s string) []string { return strings.Split(s, "\n") },
		"sub":   func(a, b int) int { return a - b },
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
