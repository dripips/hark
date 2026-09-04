// Package web поднимает две поверхности сразу: админку для владельца и
// менеджера и публичный API для виджета на чужом сайте.
//
// Они живут в одном процессе, но не смешиваются: у виджета нет сессии и он
// не видит ни настроек, ни чеков, а у админки нет доступа без входа.
package web

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/dripips/hark/internal/chat"
	"github.com/dripips/hark/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"golang.org/x/crypto/bcrypt"
)

//go:embed all:templates all:static all:widget
var assets embed.FS

const sessionCookie = "hark_session"

type Server struct {
	DB     *store.DB
	Engine *chat.Engine

	templates *template.Template
	router    chi.Router
}

func New(db *store.DB) (*Server, error) {
	s := &Server{DB: db, Engine: &chat.Engine{DB: db}}
	if err := s.parseTemplates(); err != nil {
		return nil, err
	}
	s.routes()
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.router.ServeHTTP(w, r) }

func (s *Server) routes() {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)

	// Статика админки и файл виджета. Виджет отдаётся всем: его подключают
	// с чужого сайта тегом script.
	r.Handle("/static/*", http.FileServer(http.FS(assets)))
	r.Get("/widget/hark.js", s.widgetScript)

	// Публичный API виджета. Ключей нет: разговор опознаётся по токену, а
	// доступ ограничивается списком доменов у бота.
	r.Route("/api/widget", func(r chi.Router) {
		r.Use(s.widgetCORS)
		r.Options("/*", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
		r.Get("/config", s.widgetConfig)
		r.Post("/start", s.widgetStart)
		r.Post("/send", s.widgetSend)
		r.Get("/poll", s.widgetPoll)
	})

	r.Get("/login", s.loginForm)
	r.Post("/login", s.login)
	r.Post("/logout", s.logout)

	// Админка.
	r.Group(func(r chi.Router) {
		r.Use(s.requireManager)
		r.Get("/", s.dashboard)
		r.Get("/bots", s.botList)
		r.Post("/bots", s.botCreate)
		// Страница бота разбита на вкладки со своими адресами: подключения
		// перестали быть подвалом длинной формы.
		r.Get("/bots/{id}", s.botHome)
		r.Get("/bots/{id}/connections", s.botConnections)
		r.Get("/bots/{id}/connections/new", s.connectionForm)
		r.Get("/bots/{id}/connections/{toolID}", s.connectionForm)
		r.Post("/bots/{id}/connections", s.connectionSave)
		r.Post("/bots/{id}/connections/{toolID}/check", s.connectionCheck)
		r.Post("/bots/{id}/connections/{toolID}/delete", s.connectionDelete)
		r.Get("/bots/{id}/answers", s.botAnswers)
		r.Post("/bots/{id}/answers", s.botAnswersSave)
		r.Get("/bots/{id}/model", s.botModel)
		r.Post("/bots/{id}/model", s.botModelSave)
		r.Get("/bots/{id}/widget", s.botWidget)
		r.Post("/bots/{id}/widget", s.botWidgetSave)
		r.Get("/bots/{id}/widget/preview", s.botWidgetPreview)
		r.Post("/bots/{id}/probe", s.botProbe)
		r.Get("/connections", s.connectionsAll)
		r.Get("/inbox", s.inbox)
		r.Get("/conversations/{id}", s.conversation)
		r.Post("/conversations/{id}/reply", s.conversationReply)
		r.Post("/conversations/{id}/state", s.conversationState)
		r.Get("/analytics", s.analytics)
	})

	s.router = r
}

// ── Доступ ──────────────────────────────────────────────────────────────

type ctxKey string

const managerKey ctxKey = "manager"

func (s *Server) requireManager(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		var id int64
		var name string
		err = s.DB.QueryRowContext(r.Context(), `
			SELECT m.id, m.name FROM sessions s
			JOIN managers m ON m.id = s.manager_id
			WHERE s.token = ? AND s.expires_at > datetime('now')`,
			cookie.Value).Scan(&id, &name)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		ctx := context.WithValue(r.Context(), managerKey, name)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func managerName(r *http.Request) string {
	name, _ := r.Context().Value(managerKey).(string)
	return name
}

func (s *Server) loginForm(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "login.html", map[string]any{"Title": "Вход"})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")

	var id int64
	var hash string
	err := s.DB.QueryRowContext(r.Context(),
		`SELECT id, password_hash FROM managers WHERE email = ?`, email).Scan(&id, &hash)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		// Одна и та же формулировка на неверную почту и неверный пароль:
		// иначе форма подсказывает, какие адреса заведены.
		s.render(w, r, "login.html", map[string]any{
			"Title": "Вход", "Error": "Неверная почта или пароль",
		})
		return
	}

	token := randomToken()
	if _, err := s.DB.ExecContext(r.Context(),
		`INSERT INTO sessions (token, manager_id, expires_at)
		 VALUES (?, ?, datetime('now', '+30 days'))`, token, id); err != nil {
		http.Error(w, "не удалось создать сессию", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Expires: time.Now().Add(30 * 24 * time.Hour),
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		_, _ = s.DB.ExecContext(r.Context(), `DELETE FROM sessions WHERE token = ?`, cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func randomToken() string {
	buf := make([]byte, 24)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

// HashPassword нужен установщику и тестам.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hash), err
}
