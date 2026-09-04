package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/dripips/hark/internal/lang"
	"github.com/dripips/hark/internal/store"
)

// Живая очередь ожидающих.
//
// До этого о том, что бот сдался и позвал человека, узнавали единственным
// способом: перезагрузив страницу руками. Посетитель в это время сидел и
// ждал. Теперь открытая вкладка админки узнаёт об этом за доли секунды.
//
// Источник правды остаётся в базе: событие несёт не «стало на одного
// больше», а полный снимок очереди. Поэтому потерянное событие, уснувший
// ноутбук и перезапуск сервера чинятся сами — следующим снимком, а не
// расхождением, которое накапливается.

// maxSubscribers — потолок открытых потоков. Одна горутина и один сокет на
// вкладку: первый ресурс Hark, который растёт с числом людей, а не запросов.
// Упёршиеся получают опрос раз в полминуты вместо потока.
const maxSubscribers = 64

type queueSnapshot struct {
	Count int         `json:"count"`
	Seq   int64       `json:"seq"`
	Top   []queueLine `json:"top"`
}

type queueLine struct {
	ID      int64  `json:"id"`
	Bot     string `json:"bot"`
	Reason  string `json:"reason"`
	Waiting string `json:"waiting"`
	// Claimed — имя взявшего. Пусто, если разговор свободен.
	Claimed string `json:"claimed,omitempty"`
}

type subscriber struct {
	events chan queueSnapshot
	// stale — событие не влезло в буфер. Не беда: ближайший пульс придёт
	// полным снимком, и счётчик сойдётся.
	stale bool
}

type hub struct {
	mu     sync.Mutex
	subs   map[int64]*subscriber
	nextID int64
	seq    int64
	closed bool
}

func newHub() *hub { return &hub{subs: map[int64]*subscriber{}} }

func (h *hub) add() (int64, *subscriber, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || len(h.subs) >= maxSubscribers {
		return 0, nil, false
	}
	h.nextID++
	sub := &subscriber{events: make(chan queueSnapshot, 8)}
	h.subs[h.nextID] = sub
	return h.nextID, sub, true
}

// takeStale отвечает, отставала ли вкладка, и снимает пометку. По ней пульс
// превращается в полный снимок, и счётчик сходится, не дожидаясь минуты.
func (h *hub) takeStale(id int64) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	sub, ok := h.subs[id]
	if !ok || !sub.stale {
		return false
	}
	sub.stale = false
	return true
}

func (h *hub) drop(id int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if sub, ok := h.subs[id]; ok {
		delete(h.subs, id)
		close(sub.events)
	}
}

// send раздаёт снимок всем вкладкам. Не блокируется ни на миллисекунду:
// оповещение не имеет права задержать ответ посетителю.
func (h *hub) send(snapshot queueSnapshot) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.seq++
	snapshot.Seq = h.seq

	for _, sub := range h.subs {
		select {
		case sub.events <- snapshot:
		default:
			sub.stale = true
		}
	}
}

// Close закрывает потоки при остановке сервера.
//
// Открытый поток событий — это незавершённый запрос, и Shutdown честно ждал
// бы его до своих двадцати секунд на каждом перезапуске. Здесь он завершается
// за миллисекунды, а вкладки переподключаются сами.
func (h *hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for id, sub := range h.subs {
		delete(h.subs, id)
		close(sub.events)
	}
}

// snapshot собирает очередь: сколько ждут и кто дольше всех.
func (s *Server) snapshot(r *http.Request) queueSnapshot {
	ctx := r.Context()
	count, _ := s.DB.CountWaiting(ctx)

	rows, _ := s.DB.Conversations(ctx, 0, "waiting", 5)
	bots, _ := s.DB.Bots(ctx)
	claims, _ := s.DB.ClaimNames(ctx)
	names := map[int64]string{}
	for _, bot := range bots {
		names[bot.ID] = bot.Name
	}

	out := queueSnapshot{Count: count}
	for _, conv := range rows {
		out.Top = append(out.Top, queueLine{
			ID: conv.ID, Bot: names[conv.BotID],
			Reason:  conv.EscalateReason,
			Waiting: waitedFor(conv),
			Claimed: claims[conv.ID],
		})
	}
	return out
}

// waitedFor — сколько посетитель уже ждёт. Это единственное число, ради
// которого стоит открывать очередь: разговор, где ждут двадцать минут, и
// разговор минутной давности требуют разной спешки.
func waitedFor(conv *store.Conversation) string {
	if !conv.EscalatedAt.Valid {
		return ""
	}
	since, err := time.Parse(time.RFC3339, conv.EscalatedAt.String)
	if err != nil {
		return ""
	}
	minutes := int(time.Since(since).Minutes())
	switch {
	case minutes < 1:
		return "только что"
	case minutes < 60:
		return fmt.Sprintf("%d мин", minutes)
	default:
		return fmt.Sprintf("%d ч", minutes/60)
	}
}

// notifyQueue пересчитывает очередь и рассылает её открытым вкладкам.
// Зовётся отовсюду, где очередь могла измениться.
func (s *Server) notifyQueue(r *http.Request) {
	s.Hub.send(s.snapshot(r))
}

// events — поток событий для админки.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, lang.T(language(r), "поток не поддерживается"), http.StatusInternalServerError)
		return
	}

	id, sub, ok := s.Hub.add()
	if !ok {
		// Мест нет: вкладка переходит на опрос сама, увидев этот ответ.
		http.Error(w, lang.T(language(r), "слишком много открытых вкладок"), http.StatusServiceUnavailable)
		return
	}
	defer s.Hub.drop(id)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// У сервера WriteTimeout шесть минут: он нужен потоку виджета, но здесь
	// обрубил бы соединение ровно на шестой минуте. Снимаем дедлайн точечно.
	deadlineCleared := http.NewResponseController(w).SetWriteDeadline(time.Time{}) == nil

	writeSnapshot(w, flusher, s.snapshot(r))

	// Пульс держит соединение живым сквозь обратный прокси и обнаруживает
	// уснувшего клиента ошибкой записи.
	beat := time.NewTicker(20 * time.Second)
	defer beat.Stop()
	// Полный снимок раз в минуту чинит счётчик, разъехавшийся из-за
	// потерянного события или уснувшего ноутбука.
	resync := time.NewTicker(time.Minute)
	defer resync.Stop()

	// Если дедлайн снять не удалось, уходим раньше, чем сервер оборвёт запись
	// на середине события: пусть вкладка переподключится сама.
	var giveUp <-chan time.Time
	if !deadlineCleared {
		giveUp = time.After(5 * time.Minute)
	}

	for {
		select {
		case snapshot, alive := <-sub.events:
			if !alive {
				fmt.Fprint(w, "event: bye\ndata: {}\n\n")
				flusher.Flush()
				return
			}
			writeSnapshot(w, flusher, snapshot)
		case <-beat.C:
			// Вкладка отставала — вместо пульса шлём снимок целиком.
			if s.Hub.takeStale(id) {
				writeSnapshot(w, flusher, s.snapshot(r))
				continue
			}
			fmt.Fprint(w, ": пульс\n\n")
			flusher.Flush()
		case <-resync.C:
			writeSnapshot(w, flusher, s.snapshot(r))
		case <-giveUp:
			return
		case <-r.Context().Done():
			return
		}
	}
}

func writeSnapshot(w http.ResponseWriter, flusher http.Flusher, snapshot queueSnapshot) {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: queue\ndata: %s\n\n", data)
	flusher.Flush()
}

// queueJSON — тот же снимок обычным запросом. Запасной путь для вкладки без
// потока событий и для тех, кому не хватило места.
func (s *Server) queueJSON(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.snapshot(r))
}
