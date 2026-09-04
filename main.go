// Hark — самостоятельный чат-бот для сайта: один бинарник, база рядом файлом.
//
//	hark                          поднять сервер
//	hark -addr :9000              на другом порту
//	hark -manager you@example.com -password секрет   завести менеджера
//	hark -demo                    наполнить демонстрационными данными
//
// Ключи моделей хранятся в базе у каждого бота, а не в окружении: у разных
// ботов могут быть разные поставщики.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dripips/hark/internal/store"
	"github.com/dripips/hark/internal/web"
)

func main() {
	addr := flag.String("addr", envOr("HARK_ADDR", ":8080"), "адрес сервера")
	dbPath := flag.String("db", envOr("HARK_DB", "hark.db"), "файл базы")
	manager := flag.String("manager", "", "завести менеджера с этой почтой")
	password := flag.String("password", "", "пароль для нового менеджера")
	demo := flag.Bool("demo", false, "наполнить демонстрационными данными")
	flag.Parse()

	db, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("база: %v", err)
	}
	defer db.Close()

	if *manager != "" {
		if *password == "" {
			log.Fatal("нужен -password")
		}
		if err := addManager(db, *manager, *password); err != nil {
			log.Fatalf("менеджер: %v", err)
		}
		fmt.Printf("менеджер %s заведён\n", *manager)
		return
	}

	if *demo {
		if err := seedDemo(db); err != nil {
			log.Fatalf("демонстрация: %v", err)
		}
		return
	}

	server, err := web.New(db)
	if err != nil {
		log.Fatalf("сервер: %v", err)
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

	go func() {
		log.Printf("Hark слушает %s, база %s", *addr, *dbPath)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("сервер: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(ctx)
	log.Println("остановлен")
}

func addManager(db *store.DB, email, password string) error {
	hash, err := web.HashPassword(password)
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO managers (email, name, password_hash) VALUES (?,?,?)
		ON CONFLICT(email) DO UPDATE SET password_hash = excluded.password_hash`,
		email, email, hash)
	return err
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
