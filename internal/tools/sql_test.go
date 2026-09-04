package tools

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dripips/hark/internal/store"
)

func guard(t *testing.T, tables string) *sqlRunner {
	t.Helper()
	runner, err := newSQLRunner(&store.Tool{
		Name: "shop", DSN: "file:test.db", Driver: "sqlite", AllowedTables: tables,
	}, "ru")
	if err != nil {
		t.Fatalf("не создался: %v", err)
	}
	return runner.(*sqlRunner)
}

func TestGuardAllowsPlainSelect(t *testing.T) {
	r := guard(t, "orders, products")
	for _, query := range []string{
		"SELECT * FROM orders",
		"select id, total from orders where id = 4127",
		"  SELECT o.id FROM orders o JOIN products p ON p.id = o.product_id  ",
		"WITH recent AS (SELECT * FROM orders) SELECT * FROM recent",
	} {
		if err := r.Guard(query); err != nil {
			t.Errorf("должен пройти, но отклонён: %q -> %v", query, err)
		}
	}
}

func TestGuardBlocksWrites(t *testing.T) {
	r := guard(t, "orders")
	cases := map[string]string{
		"вставка":           "INSERT INTO orders (id) VALUES (1)",
		"обновление":        "UPDATE orders SET total = 0",
		"удаление":          "DELETE FROM orders",
		"снос таблицы":      "DROP TABLE orders",
		"второй запрос":     "SELECT 1; DROP TABLE orders",
		"не select":         "PRAGMA table_info(orders)",
		"подключение файла": "SELECT * FROM orders; ATTACH DATABASE 'x' AS y",
		"чтение файла":      "SELECT load_file('/etc/passwd') FROM orders",
		"выгрузка в файл":   "SELECT * FROM orders INTO OUTFILE '/tmp/x'",
	}
	for name, query := range cases {
		if err := r.Guard(query); err == nil {
			t.Errorf("%s: запрос прошёл, а не должен был: %q", name, query)
		}
	}
}

func TestGuardChecksTableAllowlist(t *testing.T) {
	r := guard(t, "orders")

	if err := r.Guard("SELECT * FROM users"); err == nil {
		t.Error("чужая таблица прошла")
	} else if !strings.Contains(err.Error(), "users") {
		t.Errorf("в отказе нет имени таблицы: %v", err)
	}

	// Спрятать таблицу в подзапросе тоже не выйдет.
	if err := r.Guard("SELECT * FROM (SELECT * FROM secrets) x"); err == nil {
		t.Error("таблица из подзапроса прошла мимо списка")
	}
}

func TestGuardRequiresAllowlist(t *testing.T) {
	if _, err := newSQLRunner(&store.Tool{Name: "x", DSN: "file:t.db"}, "ru"); err == nil {
		t.Error("инструмент без списка таблиц создался, а это дыра")
	}
}

// Живой прогон против настоящей SQLite: проверяем и ограничение строк.
func TestRunLimitsRows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shop.db")

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE orders (id INTEGER PRIMARY KEY, total INTEGER)`); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 40; i++ {
		if _, err := db.Exec(`INSERT INTO orders (total) VALUES (?)`, i*100); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	runner, err := newSQLRunner(&store.Tool{
		Name: "shop", DSN: path, Driver: "sqlite",
		AllowedTables: "orders", RowLimit: 5, TimeoutMS: 3000,
	}, "ru")
	if err != nil {
		t.Fatal(err)
	}

	result := runner.Run(context.Background(), map[string]any{"query": "SELECT * FROM orders"})
	if result.Err != nil {
		t.Fatalf("запрос упал: %v", result.Err)
	}
	if result.Status != "5 строк" {
		t.Errorf("ожидали 5 строк из-за ограничителя, получили %q", result.Status)
	}
	if strings.Count(result.Text, "\"id\"") != 5 {
		t.Errorf("в ответе не пять записей: %s", result.Text)
	}

	// Отклонённый запрос тоже должен вернуться понятным текстом, а не паникой.
	denied := runner.Run(context.Background(), map[string]any{"query": "DELETE FROM orders"})
	if !strings.Contains(denied.Text, "отклонён") {
		t.Errorf("отказ не объяснён: %q", denied.Text)
	}
	if denied.Status != "отклонён" {
		t.Errorf("статус отказа: %q", denied.Status)
	}
}

func TestMain(m *testing.M) { os.Exit(m.Run()) }
