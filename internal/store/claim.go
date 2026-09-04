package store

import (
	"context"
	"database/sql"
	"errors"
)

// Кто взял разговор.
//
// Разговор, ждущий человека, видят все менеджеры сразу. Пока никто не
// отмечен, двое открывают его одновременно, оба пишут посетителю, и тот
// получает два ответа от разных людей. Отметка «взял» это не решает
// волшебным образом — она делает занятость видимой до того, как второй
// начнёт печатать.
//
// Полноценной системы заявок здесь нет и не нужно: одно поле, две кнопки и
// правило, что взятие атомарно.

// ErrAlreadyClaimed — разговор уже за кем-то. Возвращается проигравшему в
// гонке, чтобы он увидел чужое имя, а не молча перехватил чужую работу.
var ErrAlreadyClaimed = errors.New("разговор уже взят")

// Claim отмечает разговор за менеджером.
//
// Условие claimed_by IS NULL стоит в самом UPDATE, а не в отдельной проверке
// перед ним: два одновременных нажатия иначе оба увидели бы «свободен» и оба
// записали бы себя, а выиграл бы тот, кто записал последним.
func (db *DB) Claim(ctx context.Context, convID, managerID int64) error {
	result, err := db.ExecContext(ctx, `
		UPDATE conversations
		SET claimed_by = ?, claimed_at = datetime('now'), updated_at = datetime('now')
		WHERE id = ? AND (claimed_by IS NULL OR claimed_by = ?)`,
		managerID, convID, managerID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return ErrAlreadyClaimed
	}
	return nil
}

// Release отпускает разговор обратно в общую очередь.
//
// Отпустить может любой менеджер, а не только взявший: человек мог уйти на
// обед, и запирать посетителя за ушедшим — худшее, что можно сделать.
func (db *DB) Release(ctx context.Context, convID int64) error {
	_, err := db.ExecContext(ctx, `
		UPDATE conversations
		SET claimed_by = NULL, claimed_at = NULL, updated_at = datetime('now')
		WHERE id = ?`, convID)
	return err
}

// ClaimedBy — имя взявшего, по одному разговору.
//
// Соединение внешнее: удалённый менеджер оставляет в колонке висячий номер,
// и внутреннее соединение спрятало бы такой разговор из списка целиком.
func (db *DB) ClaimedBy(ctx context.Context, convID int64) (string, bool) {
	var name sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT m.name FROM conversations c
		LEFT JOIN managers m ON m.id = c.claimed_by
		WHERE c.id = ?`, convID).Scan(&name)
	if err != nil || !name.Valid {
		return "", false
	}
	return name.String, true
}

// ClaimNames отдаёт имена взявших для списка разговоров: одним запросом, а
// не по обращению на строку.
func (db *DB) ClaimNames(ctx context.Context) (map[int64]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT c.id, m.name FROM conversations c
		JOIN managers m ON m.id = c.claimed_by
		WHERE c.claimed_by IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}

// ToolCounts — сколько подключений у каждого бота и сколько из них включено.
// Одним запросом на всю страницу списка ботов.
func (db *DB) ToolCounts(ctx context.Context) (map[int64][2]int, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT bot_id, count(*), sum(enabled) FROM tools GROUP BY bot_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int64][2]int{}
	for rows.Next() {
		var botID int64
		var total, enabled sql.NullInt64
		if err := rows.Scan(&botID, &total, &enabled); err != nil {
			return nil, err
		}
		out[botID] = [2]int{int(total.Int64), int(enabled.Int64)}
	}
	return out, rows.Err()
}
