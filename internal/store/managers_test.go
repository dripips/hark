package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func открытьБазу(t *testing.T) (*DB, context.Context) {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "hark.db"))
	if err != nil {
		t.Fatalf("база: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, context.Background()
}

func завести(t *testing.T, db *DB, ctx context.Context, email string) *Manager {
	t.Helper()
	manager, err := db.CreateManager(ctx, email, "", "хеш-"+email)
	if err != nil {
		t.Fatalf("не завёлся %s: %v", email, err)
	}
	return manager
}

// Почта в разном регистре — это один человек. Без приведения к нижнему
// UNIQUE пропустит Ivan@example.com рядом с ivan@example.com, и войти можно
// будет под обоими.
func TestПочтаНечувствительнаКРегистру(t *testing.T) {
	db, ctx := открытьБазу(t)
	завести(t, db, ctx, "Ivan@Example.COM")

	if _, err := db.CreateManager(ctx, "ivan@example.com", "", "другой"); !errors.Is(err, ErrManagerExists) {
		t.Fatalf("тот же человек завёлся дважды: %v", err)
	}
	found, err := db.ManagerByEmail(ctx, "  IVAN@EXAMPLE.COM  ")
	if err != nil {
		t.Fatalf("не нашли по другому регистру: %v", err)
	}
	if found.Email != "ivan@example.com" {
		t.Fatalf("почта сохранена как %q", found.Email)
	}
}

func TestИмяПоУмолчаниюПочта(t *testing.T) {
	db, ctx := открытьБазу(t)
	manager := завести(t, db, ctx, "ivan@example.com")
	if manager.Name != "ivan@example.com" {
		t.Fatalf("имя = %q, ждали почту", manager.Name)
	}
}

// Последнего менеджера отдавать нельзя: после этого в админку не войдёт
// никто, а восстановиться можно только с самого сервера.
func TestПоследнегоМенеджераНеУбрать(t *testing.T) {
	db, ctx := открытьБазу(t)
	один := завести(t, db, ctx, "one@example.com")

	if err := db.DeleteManager(ctx, один.ID); !errors.Is(err, ErrLastManager) {
		t.Fatalf("последний удалился: %v", err)
	}

	два := завести(t, db, ctx, "two@example.com")
	if err := db.DeleteManager(ctx, один.ID); err != nil {
		t.Fatalf("при двоих удаление должно проходить: %v", err)
	}
	if err := db.DeleteManager(ctx, два.ID); !errors.Is(err, ErrLastManager) {
		t.Fatalf("оставшийся удалился: %v", err)
	}

	count, _ := db.CountManagers(ctx)
	if count != 1 {
		t.Fatalf("менеджеров осталось %d, ждали 1", count)
	}
}

func сессия(t *testing.T, db *DB, ctx context.Context, managerID int64, token string) {
	t.Helper()
	if _, err := db.ExecContext(ctx,
		`INSERT INTO sessions (token, manager_id, expires_at)
		 VALUES (?, ?, datetime('now', '+30 days'))`, token, managerID); err != nil {
		t.Fatalf("сессия: %v", err)
	}
}

func сессий(t *testing.T, db *DB, ctx context.Context, managerID int64) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM sessions WHERE manager_id = ?`, managerID).Scan(&count); err != nil {
		t.Fatalf("подсчёт сессий: %v", err)
	}
	return count
}

// Смену пароля зовут в том числе тогда, когда доступ увели. Оставить чужие
// открытые вкладки живыми — значит не сделать ничего.
func TestСменаПароляГаситПрочиеВходы(t *testing.T) {
	db, ctx := открытьБазу(t)
	manager := завести(t, db, ctx, "ivan@example.com")
	сессия(t, db, ctx, manager.ID, "своя")
	сессия(t, db, ctx, manager.ID, "чужая-1")
	сессия(t, db, ctx, manager.ID, "чужая-2")

	if err := db.SetManagerPassword(ctx, manager.ID, "новый-хеш", "своя"); err != nil {
		t.Fatalf("смена пароля: %v", err)
	}
	if got := сессий(t, db, ctx, manager.ID); got != 1 {
		t.Fatalf("сессий осталось %d, ждали одну", got)
	}

	var token string
	_ = db.QueryRowContext(ctx, `SELECT token FROM sessions WHERE manager_id = ?`,
		manager.ID).Scan(&token)
	if token != "своя" {
		t.Fatalf("выжила сессия %q, а не та, из которой меняли", token)
	}

	hash, _ := db.PasswordHash(ctx, manager.ID)
	if hash != "новый-хеш" {
		t.Fatalf("пароль не изменился: %q", hash)
	}
}

// Пустой keepToken означает «погасить все»: так делает флаг -reset, который
// зовут, когда доступ уже потерян.
func TestСбросБезИсключенийГаситВсе(t *testing.T) {
	db, ctx := открытьБазу(t)
	manager := завести(t, db, ctx, "ivan@example.com")
	сессия(t, db, ctx, manager.ID, "первая")
	сессия(t, db, ctx, manager.ID, "вторая")

	if err := db.SetManagerPassword(ctx, manager.ID, "хеш", ""); err != nil {
		t.Fatalf("сброс: %v", err)
	}
	if got := сессий(t, db, ctx, manager.ID); got != 0 {
		t.Fatalf("сессий осталось %d, ждали ноль", got)
	}
}

// За сессиями удалённого следит ON DELETE CASCADE, а он работает только при
// включённых внешних ключах. Проверяем, что они включены на самом деле.
func TestУдалениеУноситСессии(t *testing.T) {
	db, ctx := открытьБазу(t)
	уходит := завести(t, db, ctx, "leaves@example.com")
	остаётся := завести(t, db, ctx, "stays@example.com")
	сессия(t, db, ctx, уходит.ID, "печенье-1")
	сессия(t, db, ctx, остаётся.ID, "печенье-2")

	if err := db.DeleteManager(ctx, уходит.ID); err != nil {
		t.Fatalf("удаление: %v", err)
	}
	if got := сессий(t, db, ctx, уходит.ID); got != 0 {
		t.Fatalf("сессий удалённого осталось %d", got)
	}
	if got := сессий(t, db, ctx, остаётся.ID); got != 1 {
		t.Fatalf("задело чужую сессию: осталось %d", got)
	}
}

func TestПереименование(t *testing.T) {
	db, ctx := открытьБазу(t)
	manager := завести(t, db, ctx, "ivan@example.com")

	if err := db.RenameManager(ctx, manager.ID, "  Иван Петров  "); err != nil {
		t.Fatalf("переименование: %v", err)
	}
	again, _ := db.ManagerByID(ctx, manager.ID)
	if again.Name != "Иван Петров" {
		t.Fatalf("имя = %q", again.Name)
	}
	if err := db.RenameManager(ctx, manager.ID, "   "); err == nil {
		t.Fatal("пустое имя прошло")
	}
}

func TestОтметкаПоследнегоВхода(t *testing.T) {
	db, ctx := открытьБазу(t)
	manager := завести(t, db, ctx, "ivan@example.com")
	if manager.LastSeen.Valid {
		t.Fatal("только что заведённый уже отмечен зашедшим")
	}

	if err := db.TouchManager(ctx, manager.ID); err != nil {
		t.Fatalf("отметка: %v", err)
	}
	again, _ := db.ManagerByID(ctx, manager.ID)
	if !again.LastSeen.Valid || again.LastSeen.String == "" {
		t.Fatal("отметка не проставилась")
	}
}
