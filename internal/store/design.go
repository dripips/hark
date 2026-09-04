package store

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// DefaultAccent — единственное место, где живёт цвет по умолчанию. Раньше он
// был записан в четырёх файлах четырьмя разными значениями, и новый бот
// выглядел не так, как обещала форма.
const DefaultAccent = "#059669"

// Design — внешность виджета целиком: шрифт, отступы, цвета, фон.
//
// Хранится одной колонкой JSON, а не двумя десятками полей. Причина не в
// лени: список колонок в scanBot и SaveBot позиционный, и каждая новая ручка
// дизайна означала бы правку в трёх местах с риском сдвига на единицу. Тема
// меняется чаще всего остального в боте, поэтому ей нужна форма, которая
// расширяется без миграций.
type Design struct {
	// Шрифт
	Font       string `json:"font"`        // ключ из fontStacks
	FontURL    string `json:"font_url"`    // свой шрифт: адрес CSS
	FontStack  string `json:"font_stack"`  // свой шрифт: значение font-family
	FontSize   int    `json:"font_size"`   // основной кегль, px
	LineHeight string `json:"line_height"` // множитель, строкой — чтобы не ловить погрешность float

	// Отступы и размер
	Density string `json:"density"` // compact | normal | roomy
	Width   int    `json:"width"`   // ширина окна, px
	Height  int    `json:"height"`  // высота окна, px
	Offset  int    `json:"offset"`  // отступ кнопки от края экрана, px
	Radius  int    `json:"radius"`  // скругление окна
	Bubble  int    `json:"bubble"`  // скругление реплик
	Shadow  string `json:"shadow"`  // none | soft | deep

	// Цвета
	Scheme     string `json:"scheme"` // light | dark | auto
	Accent     string `json:"accent"`
	OnAccent   string `json:"on_accent"`
	Surface    string `json:"surface"`
	Text       string `json:"text"`
	Muted      string `json:"muted"`
	Border     string `json:"border"`
	BotBubble  string `json:"bot_bubble"`
	BotText    string `json:"bot_text"`
	UserBubble string `json:"user_bubble"`
	UserText   string `json:"user_text"`

	// Тёмная палитра. Работает только при scheme=auto: посетитель с тёмной
	// системой получает эти цвета, остальные — те, что выше. Выводить их из
	// светлых мы не беремся — это гадание, а бежевую панель оно превращает в
	// чужую серую.
	DarkSurface  string `json:"dark_surface"`
	DarkText     string `json:"dark_text"`
	DarkBackdrop string `json:"dark_backdrop"`

	// Фон ленты
	BackdropKind  string `json:"backdrop_kind"`  // solid | gradient | dots | grid | image
	BackdropFrom  string `json:"backdrop_from"`  // цвет или верх градиента
	BackdropTo    string `json:"backdrop_to"`    // низ градиента
	BackdropAngle int    `json:"backdrop_angle"` // угол градиента, градусы
	BackdropImage string `json:"backdrop_image"` // адрес картинки

	// Шапка
	HeaderKind string `json:"header_kind"` // accent | surface | gradient
	Subtitle   string `json:"subtitle"`    // строка под названием
}

// fontStacks — готовые наборы. Свой шрифт задаётся отдельными полями, потому
// что подключать чужой CSS нужно осознанно: он тянет запрос к чужому домену.
//
// «inherit» — единственный набор без своего значения: виджет берёт шрифт
// страницы, на которой стоит. Ноль загрузки и точное попадание в чужую
// вёрстку, которого не даст ни один список.
var fontStacks = map[string]string{
	"inherit":   `inherit`,
	"system":    `-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif`,
	"inter":     `Inter, -apple-system, "Segoe UI", Roboto, sans-serif`,
	"geometric": `"Century Gothic", Futura, "Trebuchet MS", -apple-system, sans-serif`,
	"serif":     `Georgia, "Iowan Old Style", "Times New Roman", serif`,
	"rounded":   `"SF Pro Rounded", Nunito, "Segoe UI", system-ui, sans-serif`,
	"mono":      `"SF Mono", "JetBrains Mono", Consolas, "Liberation Mono", monospace`,
}

// densitySteps — шаг сетки для трёх плотностей. Всё в виджете считается от
// него, поэтому «просторнее» двигает сразу всё, а не один отступ.
var densitySteps = map[string]int{"compact": 3, "normal": 4, "roomy": 5}

var (
	reHex       = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	reNumber    = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)
	reFontStack = regexp.MustCompile(`^[A-Za-z0-9 ,"'_-]{1,120}$`)
	reHTTPS     = regexp.MustCompile(`^https://[A-Za-z0-9._~:/?#\[\]@!$&*+;=%-]+$`)
)

// Design разбирает тему, подставляет умолчания и вычищает всё, что не подошло.
//
// Чистка тут не формальность. Значения темы попадают прямо в текст CSS
// виджета, и цвет вида «#fff;} .hk__panel{display:none» выключил бы окно
// целиком. Проверяем в одном месте — через него проходят и сохранение, и
// предпросмотр, и чтение из базы.
func (b Bot) Design() Design {
	d := Design{}
	if b.Theme != "" {
		_ = json.Unmarshal([]byte(b.Theme), &d)
	}

	oneOf(&d.Font, "system", "inherit", "system", "inter", "geometric", "serif", "rounded", "mono", "custom")
	oneOf(&d.Density, "normal", "compact", "normal", "roomy")
	oneOf(&d.Shadow, "soft", "none", "soft", "deep")
	oneOf(&d.Scheme, "light", "light", "dark", "auto")
	oneOf(&d.BackdropKind, "solid", "solid", "gradient", "dots", "grid", "image")
	oneOf(&d.HeaderKind, "accent", "accent", "surface", "gradient")

	if !reNumber.MatchString(d.LineHeight) {
		d.LineHeight = "1.5"
	} else if value, err := strconv.ParseFloat(d.LineHeight, 64); err != nil || value < 1.1 || value > 2.2 {
		d.LineHeight = "1.5"
	}
	if !reFontStack.MatchString(d.FontStack) {
		d.FontStack = ""
	}
	if !reHTTPS.MatchString(d.FontURL) {
		d.FontURL = ""
	}
	if !reHTTPS.MatchString(d.BackdropImage) {
		d.BackdropImage = ""
	}
	d.Subtitle = clip(d.Subtitle, 80)

	d.FontSize = clamp(d.FontSize, 13, 19, 15)
	d.Width = clamp(d.Width, 320, 460, 384)
	d.Height = clamp(d.Height, 420, 680, 560)
	d.Offset = clamp(d.Offset, 8, 48, 20)
	d.Bubble = clamp(d.Bubble, 0, 22, 14)
	d.BackdropAngle = clamp(d.BackdropAngle, 0, 360, 160)
	d.Radius = clamp(d.Radius, 0, 28, orDefault(b.CornerRadius, 18))

	// Акцент живёт в собственной колонке с самой первой версии: тема его не
	// дублирует, а наследует, пока в ней не задали своё.
	color(&d.Accent, b.Accent)
	color(&d.Accent, DefaultAccent)

	dark := d.Scheme == "dark"
	color(&d.Surface, pick(dark, "#16181d", "#ffffff"))
	color(&d.Text, pick(dark, "#e8eaf0", "#14161a"))
	color(&d.OnAccent, readable(d.Accent))
	color(&d.Muted, mix(d.Text, d.Surface, 0.42))
	color(&d.Border, mix(d.Text, d.Surface, 0.86))
	color(&d.BotBubble, mix(d.Text, d.Surface, 0.94))
	color(&d.BotText, d.Text)
	color(&d.UserBubble, d.Accent)
	color(&d.UserText, readable(d.UserBubble))
	color(&d.BackdropFrom, mix(d.Text, d.Surface, 0.975))
	color(&d.BackdropTo, d.BackdropFrom)

	color(&d.DarkSurface, "#16181d")
	color(&d.DarkText, "#e8eaf0")
	color(&d.DarkBackdrop, "#101216")

	return d
}

// Step — шаг сетки отступов для выбранной плотности.
func (d Design) Step() int {
	if step, ok := densitySteps[d.Density]; ok {
		return step
	}
	return 4
}

// Stack — итоговое значение font-family.
func (d Design) Stack() string {
	if d.Font == "custom" {
		if d.FontStack == "" {
			return fontStacks["system"]
		}
		// Системный хвост дописывается всегда: если своего шрифта на странице
		// не окажется, виджет не свалится в times new roman.
		return d.FontStack + ", " + fontStacks["system"]
	}
	if stack, ok := fontStacks[d.Font]; ok {
		return stack
	}
	return fontStacks["system"]
}

// Ink — акцент, пригодный для текста на полотне.
//
// Один и тот же цвет служит и заливкой кнопки, и цветом ссылки в сноске.
// Жёлтый акцент на белом полотне как заливка ещё читается, а как текст уже
// нет — поэтому для текста его затемняем, пока контраст не дойдёт до 4,5:1.
func (d Design) Ink(on string) string {
	ink := d.Accent
	for i := 0; i < 24 && contrast(ink, on) < 4.5; i++ {
		if luma(on) > 0.5 {
			ink = shift(ink, -10)
		} else {
			ink = shift(ink, 10)
		}
	}
	return ink
}

// ── Вспомогательное ─────────────────────────────────────────────────────

func oneOf(field *string, fallback string, allowed ...string) {
	for _, item := range allowed {
		if *field == item {
			return
		}
	}
	*field = fallback
}

func color(field *string, fallback string) {
	if !reHex.MatchString(*field) {
		*field = fallback
	}
}

func clamp(value, low, high, fallback int) int {
	if value == 0 {
		return fallback
	}
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func orDefault(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

// Clip — обрезка по символам с многоточием. Отдельная от clip, потому что
// clip молча выбрасывает битую строку, а здесь нужен видимый хвост.
//
// Байтовая обрезка на кириллице врёт вдвое (два байта на букву) и рубит
// букву пополам, оставляя в интерфейсе ромб с вопросительным знаком.
func Clip(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "…"
}

func clip(s string, limit int) string {
	// Строка не в UTF-8 — это не подпись, а сбой переноса. Пустая лучше, чем
	// цепочка ромбов с вопросительными знаками в чужой шапке.
	if !utf8.ValidString(s) {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit])
}

func pick(cond bool, yes, no string) string {
	if cond {
		return yes
	}
	return no
}

func rgb(hex string) (int, int, int) {
	if !reHex.MatchString(hex) {
		return 0, 0, 0
	}
	r, _ := strconv.ParseInt(hex[1:3], 16, 32)
	g, _ := strconv.ParseInt(hex[3:5], 16, 32)
	b, _ := strconv.ParseInt(hex[5:7], 16, 32)
	return int(r), int(g), int(b)
}

func hex(r, g, b int) string {
	return fmt.Sprintf("#%02x%02x%02x", bound(r), bound(g), bound(b))
}

func bound(value int) int {
	if value < 0 {
		return 0
	}
	if value > 255 {
		return 255
	}
	return value
}

// mix смешивает два цвета: доля 0 — первый, 1 — второй. Из неё выводятся
// приглушённый текст, линии и фон реплики бота, чтобы владелец не подбирал
// пять оттенков вручную под каждое полотно.
func mix(from, to string, ratio float64) string {
	fr, fg, fb := rgb(from)
	tr, tg, tb := rgb(to)
	at := func(a, b int) int { return int(math.Round(float64(a) + (float64(b)-float64(a))*ratio)) }
	return hex(at(fr, tr), at(fg, tg), at(fb, tb))
}

// Shift двигает яркость: нужен для градиента шапки и подбора читаемого текста.
func Shift(color string, delta int) string { return shift(color, delta) }

func shift(color string, delta int) string {
	r, g, b := rgb(color)
	return hex(r+delta, g+delta, b+delta)
}

func luma(color string) float64 {
	r, g, b := rgb(color)
	channel := func(value int) float64 {
		v := float64(value) / 255
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(r) + 0.7152*channel(g) + 0.0722*channel(b)
}

func contrast(a, b string) float64 {
	la, lb := luma(a), luma(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// readable подбирает текст на заливке: белый или почти чёрный, смотря что
// контрастнее. Салатовая кнопка с белой надписью нечитаема, и владелец не
// обязан это высчитывать.
func readable(background string) string {
	if contrast("#ffffff", background) >= contrast("#101215", background) {
		return "#ffffff"
	}
	return "#101215"
}

// Sanitized возвращает тему обратно в JSON после чистки: сохраняем в базу уже
// проверенное, чтобы мусор не лежал в колонке до первого чтения.
func (d Design) Sanitized() string {
	out, err := json.Marshal(d)
	if err != nil {
		return ""
	}
	return string(out)
}

// Quoted экранирует адрес для url() в CSS. Кавычки и скобки внутри адреса
// закрыли бы правило и открыли своё.
func Quoted(url string) string {
	safe := strings.NewReplacer(`"`, "", `'`, "", `(`, "", `)`, "", "\\", "", "\n", "").Replace(url)
	return `"` + safe + `"`
}
