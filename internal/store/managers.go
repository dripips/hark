package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Менеджеры — люди, которые входят в админку и отвечают посетителям.
//
// Раньше единственным способом завести менеджера был флаг командной строки,
// а запросы к таблице лежали сырым SQL прямо в main.go. Здесь они собраны в
// одном месте, потому что теперь ими пользуются и установщик, и админка.
type Manager struct {
	ID        int64
	Email     string
	Name      string
	CreatedAt string
	LastSeen  sql.NullString
	// Lang — на каком языке человек хочет видеть админку. Пусто означает
	// «как в браузере»: навязывать русский тому, кто зашёл впервые, незачем.
	Lang string
}

// ErrLastManager — попытка убрать последнего человека, который может войти.
//
// Без этой проверки владелец запирает себя снаружи собственной админки:
// восстановиться можно только флагом командной строки на сервере, а до
// сервера в этот момент может не быть доступа.
var ErrLastManager = errors.New("это последний менеджер: без него в админку никто не войдёт")

// ErrManagerExists — почта уже занята.
var ErrManagerExists = errors.New("менеджер с такой почтой уже заведён")

const managerColumns = `id, email, name, created_at, last_seen, lang`

func scanManager(row interface{ Scan(...any) error }) (*Manager, error) {
	var m Manager
	if err := row.Scan(&m.ID, &m.Email, &m.Name, &m.CreatedAt, &m.LastSeen, &m.Lang); err != nil {
		return nil, err
	}
	return &m, nil
}

func (db *DB) Managers(ctx context.Context) ([]*Manager, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+managerColumns+` FROM managers ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Manager
	for rows.Next() {
		manager, err := scanManager(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, manager)
	}
	return out, rows.Err()
}

func (db *DB) ManagerByID(ctx context.Context, id int64) (*Manager, error) {
	return scanManager(db.QueryRowContext(ctx,
		`SELECT `+managerColumns+` FROM managers WHERE id = ?`, id))
}

func (db *DB) ManagerByEmail(ctx context.Context, email string) (*Manager, error) {
	return scanManager(db.QueryRowContext(ctx,
		`SELECT `+managerColumns+` FROM managers WHERE email = ?`, NormalizeEmail(email)))
}

// PasswordHash отдаёт хеш для сверки при входе. Отдельным запросом, чтобы хеш
// не таскался в структуре Manager по всем страницам админки.
func (db *DB) PasswordHash(ctx context.Context, id int64) (string, error) {
	var hash string
	err := db.QueryRowContext(ctx, `SELECT password_hash FROM managers WHERE id = ?`, id).Scan(&hash)
	return hash, err
}

func (db *DB) CountManagers(ctx context.Context) (int, error) {
	var count int
	err := db.QueryRowContext(ctx, `SELECT count(*) FROM managers`).Scan(&count)
	return count, err
}

// CreateManager заводит человека. Хеш пароля считает вызывающий: пакет
// хранения не должен знать про bcrypt.
func (db *DB) CreateManager(ctx context.Context, email, name, hash string) (*Manager, error) {
	email = NormalizeEmail(email)
	if email == "" {
		return nil, errors.New("нужна почта")
	}
	if name = strings.TrimSpace(name); name == "" {
		name = email
	}

	result, err := db.ExecContext(ctx,
		`INSERT INTO managers (email, name, password_hash) VALUES (?,?,?)`, email, name, hash)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrManagerExists
		}
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	return db.ManagerByID(ctx, id)
}

// SetManagerLang запоминает выбор языка.
func (db *DB) SetManagerLang(ctx context.Context, id int64, code string) error {
	_, err := db.ExecContext(ctx, `UPDATE managers SET lang = ? WHERE id = ?`, code, id)
	return err
}

func (db *DB) RenameManager(ctx context.Context, id int64, name string) error {
	if name = strings.TrimSpace(name); name == "" {
		return errors.New("нужно имя")
	}
	_, err := db.ExecContext(ctx, `UPDATE managers SET name = ? WHERE id = ?`, name, id)
	return err
}

// SetManagerPassword меняет пароль и гасит чужие сессии этого человека.
//
// Смена пароля — это либо «я забыл», либо «у меня увели доступ». Во втором
// случае оставить открытые сессии значит не сделать ничего: тот, кто увёл
// печенье, продолжит читать переписку с посетителями.
func (db *DB) SetManagerPassword(ctx context.Context, id int64, hash, keepToken string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`UPDATE managers SET password_hash = ? WHERE id = ?`, hash, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM sessions WHERE manager_id = ? AND token <> ?`, id, keepToken); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteManager убирает человека вместе с его сессиями — за сессии отвечает
// ON DELETE CASCADE в схеме, внешние ключи включены в Open.
//
// Последнего не отдаёт: см. ErrLastManager.
func (db *DB) DeleteManager(ctx context.Context, id int64) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Считаем и удаляем в одной сделке: иначе два одновременных удаления
	// каждое увидит «нас двое» и уберёт обоих.
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM managers`).Scan(&count); err != nil {
		return err
	}
	if count <= 1 {
		return ErrLastManager
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM managers WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// TouchManager отмечает, что человек заходил. По этой отметке видно, кто на
// связи, а кто завёлся и больше не появлялся.
func (db *DB) TouchManager(ctx context.Context, id int64) error {
	_, err := db.ExecContext(ctx,
		`UPDATE managers SET last_seen = datetime('now') WHERE id = ?`, id)
	return err
}

// NormalizeEmail приводит почту к одному виду: иначе Ivan@example.com и
// ivan@example.com станут двумя разными людьми, а UNIQUE этого не заметит.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
