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
	"github.com/dripips/hark/internal/lang"
	"github.com/dripips/hark/internal/notify"
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
	// Hub рассылает открытым вкладкам админки очередь ожидающих. Живёт в
	// памяти процесса и умирает вместе с ним: очередь всё равно лежит в базе,
	// а хаб — лишь способ узнать о ней быстрее, чем перезагрузкой страницы.
	Hub *hub
	// Notify зовёт человека наружу, когда админку никто не держит открытой.
	// Пустой адрес у бота означает выключено, поэтому отправитель заводится
	// всегда и ничего не делает, пока его не настроили.
	Notify *notify.Sender

	templates *template.Template
	router    chi.Router
}

func New(db *store.DB) (*Server, error) {
	s := &Server{DB: db, Engine: &chat.Engine{DB: db}, Hub: newHub(), Notify: notify.New()}
	// Исход зова записывает та же горутина, что его сделала: своим контекстом,
	// не привязанным к запросу, которого к тому моменту уже нет.
	s.Notify.Note = func(botID int64, status string) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.DB.NoteNotify(ctx, botID, status)
	}
	s.Notify.Start()
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
		r.Post("/bots/{id}/answers/test", s.notifyTest)
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
		r.Post("/conversations/{id}/claim", s.conversationClaim)
		r.Post("/conversations/{id}/release", s.conversationRelease)
		r.Get("/analytics", s.analytics)
		r.Get("/events", s.events)
		r.Get("/queue.json", s.queueJSON)
		r.Get("/managers", s.managers)
		r.Post("/managers", s.managerCreate)
		r.Post("/managers/{id}/rename", s.managerRename)
		r.Post("/managers/{id}/password", s.managerPassword)
		r.Post("/managers/{id}/delete", s.managerDelete)
		r.Post("/managers/lang", s.managerLang)
	})

	s.router = r
}

// ── Доступ ──────────────────────────────────────────────────────────────

type ctxKey string

const (
	managerKey ctxKey = "manager"
	tokenKey   ctxKey = "token"
)

// requireManager кладёт в контекст самого человека, а не только его имя.
//
// Имени хватало, пока админка умела только подписывать ответы. Страница
// управления менеджерами должна знать, кто именно смотрит: себя удалять
// нельзя, чужой пароль менять нельзя, свой — можно.
func (s *Server) requireManager(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		var id int64
		err = s.DB.QueryRowContext(r.Context(), `
			SELECT manager_id FROM sessions
			WHERE token = ? AND expires_at > datetime('now')`,
			cookie.Value).Scan(&id)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		manager, err := s.DB.ManagerByID(r.Context(), id)
		if err != nil {
			// Человека удалили, а печенье осталось. Сессия каскадом уже
			// снесена, но запрос мог прийти раньше — выпроваживаем.
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		ctx := context.WithValue(r.Context(), managerKey, manager)
		ctx = context.WithValue(ctx, tokenKey, cookie.Value)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func currentManager(r *http.Request) *store.Manager {
	manager, _ := r.Context().Value(managerKey).(*store.Manager)
	return manager
}

func currentToken(r *http.Request) string {
	token, _ := r.Context().Value(tokenKey).(string)
	return token
}

// language выбирает язык страницы.
//
// Порядок: выбор менеджера → язык браузера → русский. Первое живёт в базе и
// переживает смену устройства, второе избавляет от настройки того, кто зашёл
// впервые, третье — язык, на котором написан продукт.
func language(r *http.Request) string {
	if manager := currentManager(r); manager != nil && manager.Lang != "" {
		return lang.Pick(manager.Lang)
	}
	return lang.FromHeader(r.Header.Get("Accept-Language"))
}

func managerName(r *http.Request) string {
	if manager := currentManager(r); manager != nil {
		return manager.Name
	}
	return ""
}

func (s *Server) loginForm(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "login.html", map[string]any{"Title": "Вход"})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")

	manager, err := s.DB.ManagerByEmail(r.Context(), email)
	var hash string
	if err == nil {
		hash, err = s.DB.PasswordHash(r.Context(), manager.ID)
	}
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
		 VALUES (?, ?, datetime('now', '+30 days'))`, token, manager.ID); err != nil {
		http.Error(w, "не удалось создать сессию", http.StatusInternalServerError)
		return
	}
	_ = s.DB.TouchManager(r.Context(), manager.ID)
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

// comparePassword сверяет пароль с хешем. Обёртка нужна, чтобы bcrypt не
// расползался по обработчикам.
func comparePassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// HashPassword нужен установщику и тестам.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hash), err
}
