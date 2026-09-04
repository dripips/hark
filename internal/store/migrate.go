package store

import (
	"database/sql"
	"fmt"
)

// Hark ставят себе и обновляют поверх своей базы, поэтому новые поля должны
// доезжать без ручных ALTER. Схема создаёт таблицы, а этот список добавляет
// колонки, появившиеся после первого выпуска.
//
// Правило простое: колонку сюда дописывают, из списка не удаляют и порядок не
// меняют. Тогда база любого возраста доходит до текущего вида за один запуск.
var columnAdditions = []struct {
	table  string
	column string
	spec   string
}{
	// Виджет: приветственный экран вместо пустой ленты.
	{"bots", "welcome_title", "TEXT NOT NULL DEFAULT ''"},
	{"bots", "welcome_text", "TEXT NOT NULL DEFAULT ''"},
	// Готовые вопросы, по одному в строке: посетителю не надо ничего печатать.
	{"bots", "quick_replies", "TEXT NOT NULL DEFAULT ''"},
	// Оговорка под полем ввода и ссылка на политику.
	{"bots", "disclaimer", "TEXT NOT NULL DEFAULT ''"},
	{"bots", "privacy_url", "TEXT NOT NULL DEFAULT ''"},
	{"bots", "privacy_label", "TEXT NOT NULL DEFAULT ''"},
	// Внешность кнопки: round — круглая с иконкой, pill — с подписью.
	{"bots", "launcher_style", "TEXT NOT NULL DEFAULT 'pill'"},
	{"bots", "avatar_emoji", "TEXT NOT NULL DEFAULT ''"},
	{"bots", "corner_radius", "INTEGER NOT NULL DEFAULT 18"},
	// Вся внешность одним JSON: новые ручки дизайна не требуют миграции.
	{"bots", "theme", "TEXT NOT NULL DEFAULT ''"},
}

func migrate(db *sql.DB) error {
	for _, item := range columnAdditions {
		has, err := hasColumn(db, item.table, item.column)
		if err != nil {
			return err
		}
		if has {
			continue
		}
		stmt := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", item.table, item.column, item.spec)
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("%s.%s: %w", item.table, item.column, err)
		}
	}
	return nil
}

func hasColumn(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name, kind string
			notNull    int
			dflt       sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &kind, &notNull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}
