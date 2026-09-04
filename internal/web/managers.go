package web

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/dripips/hark/internal/store"
	"github.com/go-chi/chi/v5"
)

// Страница менеджеров.
//
// До неё завести человека можно было только флагом командной строки на
// сервере, а сменить пароль — тем же флагом, который делал это молча. Всё,
// что здесь есть, раньше требовало доступа к машине.
//
// Ролей в Hark нет: каждый менеджер видит все разговоры и может убрать
// любого другого. Поэтому смена чужого пароля не даёт новых прав — она лишь
// избавляет от похода на сервер, когда коллега забыл свой.

const minPasswordLen = 8

func (s *Server) managers(w http.ResponseWriter, r *http.Request) {
	list, err := s.DB.Managers(r.Context())
	if err != nil {
		http.Error(w, "не удалось прочитать список", http.StatusInternalServerError)
		return
	}
	me := currentManager(r)

	s.render(w, r, "managers.html", map[string]any{
		"Title": "Менеджеры", "Managers": list, "Me": me,
		"Alone": len(list) < 2,
		"Error": r.URL.Query().Get("error"),
		"Done":  r.URL.Query().Get("done"),
	})
}

func (s *Server) managerCreate(w http.ResponseWriter, r *http.Request) {
	email := store.NormalizeEmail(r.FormValue("email"))
	name := strings.TrimSpace(r.FormValue("name"))
	password := r.FormValue("password")

	if problem := checkNewManager(email, password); problem != "" {
		s.managersBack(w, r, "error", problem)
		return
	}

	hash, err := HashPassword(password)
	if err != nil {
		s.managersBack(w, r, "error", "не удалось сохранить пароль")
		return
	}
	if _, err := s.DB.CreateManager(r.Context(), email, name, hash); err != nil {
		if errors.Is(err, store.ErrManagerExists) {
			s.managersBack(w, r, "error", "Менеджер "+email+" уже заведён")
			return
		}
		s.managersBack(w, r, "error", err.Error())
		return
	}
	s.managersBack(w, r, "done", "Менеджер "+email+" заведён")
}

func checkNewManager(email, password string) string {
	if email == "" || !strings.Contains(email, "@") {
		return "Нужна почта: по ней человек входит"
	}
	if len([]rune(password)) < minPasswordLen {
		return "Пароль короче " + strconv.Itoa(minPasswordLen) + " знаков"
	}
	return ""
}

func (s *Server) managerRename(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err := s.DB.RenameManager(r.Context(), id, r.FormValue("name")); err != nil {
		s.managersBack(w, r, "error", err.Error())
		return
	}
	s.managersBack(w, r, "done", "Имя изменено")
}

// managerPassword меняет пароль себе или коллеге.
//
// Себе — только предъявив нынешний: иначе чужая открытая вкладка в переговорке
// превращается в захват учётной записи. Коллеге — без этого, потому что
// нынешнего пароля коллеги мы и не знаем, а прав это не добавляет: менеджеры
// в Hark равны.
func (s *Server) managerPassword(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	me := currentManager(r)
	password := r.FormValue("password")

	if len([]rune(password)) < minPasswordLen {
		s.managersBack(w, r, "error", "Пароль короче "+strconv.Itoa(minPasswordLen)+" знаков")
		return
	}

	itsMe := me != nil && me.ID == id
	if itsMe && !s.passwordMatches(r, id, r.FormValue("current")) {
		s.managersBack(w, r, "error", "Нынешний пароль не подошёл")
		return
	}

	hash, err := HashPassword(password)
	if err != nil {
		s.managersBack(w, r, "error", "не удалось сохранить пароль")
		return
	}

	// Свою нынешнюю вкладку оставляем в живых, остальные входы гасим. Чужому
	// не оставляем ничего: смена его пароля означает, что доступ отзывают.
	keep := ""
	if itsMe {
		keep = currentToken(r)
	}
	if err := s.DB.SetManagerPassword(r.Context(), id, hash, keep); err != nil {
		s.managersBack(w, r, "error", err.Error())
		return
	}

	if itsMe {
		s.managersBack(w, r, "done", "Пароль изменён, прочие ваши входы погашены")
		return
	}
	s.managersBack(w, r, "done", "Пароль изменён, все его входы погашены")
}

func (s *Server) passwordMatches(r *http.Request, id int64, password string) bool {
	hash, err := s.DB.PasswordHash(r.Context(), id)
	if err != nil {
		return false
	}
	return comparePassword(hash, password)
}

func (s *Server) managerDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)

	// Себя не удаляем даже при живых коллегах: это ровно то движение, после
	// которого человек оказывается на странице входа с паролем, который он
	// только что стёр. Убрать вас может любой другой менеджер.
	if me := currentManager(r); me != nil && me.ID == id {
		s.managersBack(w, r, "error", "Себя убрать нельзя: попросите коллегу")
		return
	}

	if err := s.DB.DeleteManager(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrLastManager) {
			s.managersBack(w, r, "error", err.Error())
			return
		}
		s.managersBack(w, r, "error", err.Error())
		return
	}
	s.managersBack(w, r, "done", "Менеджер убран, его входы погашены")
}

func (s *Server) managersBack(w http.ResponseWriter, r *http.Request, kind, message string) {
	http.Redirect(w, r, "/managers?"+kind+"="+url.QueryEscape(message), http.StatusSeeOther)
}
