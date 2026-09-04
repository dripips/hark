// Hark — самостоятельный чат-бот для сайта: один бинарник, база рядом файлом.
//
//	hark                          поднять сервер
//	hark -addr :9000              на другом порту
//	hark -manager you@example.com -password секрет   завести менеджера
//	hark -manager you@example.com -password новый -reset   сменить ему пароль
//	hark -managers                список заведённых менеджеров
//	hark -demo                    наполнить демонстрационными данными
//
// Ключи моделей хранятся в базе у каждого бота, а не в окружении: у разных
// ботов могут быть разные поставщики.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dripips/hark/internal/lang"
	"github.com/dripips/hark/internal/store"
	"github.com/dripips/hark/internal/web"
)

func main() {
	addr := flag.String("addr", envOr("HARK_ADDR", ":8080"), "адрес сервера")
	dbPath := flag.String("db", envOr("HARK_DB", "hark.db"), "файл базы")
	manager := flag.String("manager", "", "завести менеджера с этой почтой")
	password := flag.String("password", "", "пароль для нового менеджера")
	reset := flag.Bool("reset", false, "сменить пароль существующему менеджеру")
	list := flag.Bool("managers", false, "показать заведённых менеджеров")
	demo := flag.Bool("demo", false, "наполнить демонстрационными данными")
	// Язык командной строки и демонстрации. Админку каждый менеджер
	// переключает себе сам, а здесь менеджера ещё нет.
	code := flag.String("lang", envOr("HARK_LANG", lang.Source), "язык сообщений: ru, en")
	flag.Parse()

	L := lang.Pick(*code)
	if err := lang.Err(); err != nil {
		log.Fatalf("словари: %v", err)
	}

	db, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf(lang.T(L, "база: %v"), err)
	}
	defer db.Close()

	if *list {
		if err := listManagers(db, L); err != nil {
			log.Fatalf(lang.T(L, "менеджеры: %v"), err)
		}
		return
	}

	if *manager != "" {
		if *password == "" {
			log.Fatal(lang.T(L, "нужен -password"))
		}
		if err := addManager(db, L, *manager, *password, *reset); err != nil {
			log.Fatalf(lang.T(L, "менеджер: %v"), err)
		}
		return
	}

	if *demo {
		if err := seedDemo(db, L); err != nil {
			log.Fatalf(lang.T(L, "демонстрация: %v"), err)
		}
		return
	}

	server, err := web.New(db)
	if err != nil {
		log.Fatalf(lang.T(L, "сервер: %v"), err)
	}

	httpServer := &http.Server{
		Addr:    *addr,
		Handler: server,
		// Ответ модели идёт потоком и может занять минуту: короткий таймаут
		// записи оборвал бы разговор на середине.
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      6 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}

	// Поток событий админки — незавершённый запрос, и Shutdown ждал бы его
	// все свои двадцать секунд на каждом перезапуске. Хаб закрывает потоки
	// первым, вкладки переподключаются сами.
	httpServer.RegisterOnShutdown(server.Hub.Close)

	go func() {
		log.Printf(lang.T(L, "Hark слушает %s, база %s"), *addr, *dbPath)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf(lang.T(L, "сервер: %v"), err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(ctx)
	// Последний зов наружу успевает уйти: пять секунд и не дольше.
	server.Notify.Close()
	log.Println(lang.T(L, "остановлен"))
}

// addManager заводит человека или, если попросили явно, меняет ему пароль.
//
// Раньше здесь стоял ON CONFLICT DO UPDATE, и повторный запуск с той же
// почтой молча менял человеку пароль. Команда выглядела как «завести
// менеджера», а на деле выбивала из админки того, кто там уже работал, —
// и ничего об этом не говорила. Теперь перезапись требует -reset.
func addManager(db *store.DB, L, email, password string, reset bool) error {
	ctx := context.Background()
	email = store.NormalizeEmail(email)

	hash, err := web.HashPassword(password)
	if err != nil {
		return err
	}

	if reset {
		existing, err := db.ManagerByEmail(ctx, email)
		if err != nil {
			return fmt.Errorf(lang.T(L, "менеджера %s нет: заведите его без -reset"), email)
		}
		// Прошлые сессии гасим все: -reset зовут в том числе тогда, когда
		// доступ увели, и оставить чужую печеньку живой значит не помочь.
		if err := db.SetManagerPassword(ctx, existing.ID, hash, ""); err != nil {
			return err
		}
		fmt.Printf(lang.T(L, "пароль %s изменён, прошлые входы погашены")+"\n", email)
		return nil
	}

	if _, err := db.CreateManager(ctx, email, email, hash); err != nil {
		if errors.Is(err, store.ErrManagerExists) {
			return fmt.Errorf(lang.T(L, "менеджер %s уже заведён. "+
				"Сменить ему пароль: -manager %s -password новый -reset"), email, email)
		}
		return err
	}
	fmt.Printf(lang.T(L, "менеджер %s заведён")+"\n", email)
	return nil
}

// listManagers нужен для восстановления: прежде чем менять кому-то пароль,
// стоит увидеть, кто вообще заведён.
func listManagers(db *store.DB, L string) error {
	managers, err := db.Managers(context.Background())
	if err != nil {
		return err
	}
	if len(managers) == 0 {
		fmt.Println(lang.T(L, "менеджеров нет: заведите первого через -manager и -password"))
		return nil
	}
	for _, m := range managers {
		seen := lang.T(L, "ни разу не заходил")
		if m.LastSeen.Valid {
			seen = lang.T(L, "заходил") + " " + m.LastSeen.String
		}
		fmt.Printf("%-32s %-24s %s\n", m.Email, m.Name, seen)
	}
	return nil
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
