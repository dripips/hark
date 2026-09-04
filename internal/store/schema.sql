-- Схема Hark. SQLite: продукт ставят себе, и лишняя база рядом — это лишний
-- повод не поставить. Подключаемая база клиента живёт отдельно и только на
-- чтение, см. internal/tools/sql.go.

PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;

-- Бот. Их может быть несколько: один на сайт, другой на раздел поддержки.
CREATE TABLE IF NOT EXISTS bots (
  id             INTEGER PRIMARY KEY,
  slug           TEXT    NOT NULL UNIQUE,
  name           TEXT    NOT NULL,
  instructions   TEXT    NOT NULL DEFAULT '',
  greeting       TEXT    NOT NULL DEFAULT '',
  -- Поставщик и модель. Меняются без правок кода: openai или anthropic,
  -- база — любой совместимый адрес, включая свой сервер.
  provider       TEXT    NOT NULL DEFAULT 'openai',
  base_url       TEXT    NOT NULL DEFAULT '',
  model          TEXT    NOT NULL DEFAULT 'gpt-5-nano',
  api_key        TEXT    NOT NULL DEFAULT '',
  max_tokens     INTEGER NOT NULL DEFAULT 1200,
  -- Хранится строкой, потому что «не задано» и «ноль» — разные вещи, а
  -- модель может вовсе не принимать этот параметр.
  temperature    TEXT    NOT NULL DEFAULT '',
  reasoning      TEXT    NOT NULL DEFAULT '',
  -- Что модель приняла на последней пробе, в JSON. Настройки показывают
  -- только те ручки, которые не сломают разговор.
  capabilities   TEXT    NOT NULL DEFAULT '{}',
  -- Цена за миллион токенов в копейках: так её и публикуют поставщики.
  price_in       INTEGER NOT NULL DEFAULT 0,
  price_out      INTEGER NOT NULL DEFAULT 0,
  -- Внешность виджета.
  accent         TEXT    NOT NULL DEFAULT '#2563eb',
  position       TEXT    NOT NULL DEFAULT 'right',
  launcher_text  TEXT    NOT NULL DEFAULT '',
  -- Домены, которым разрешено встраивать виджет. Пусто — любой.
  allowed_origins TEXT   NOT NULL DEFAULT '',
  -- Сколько раз подряд бот может не справиться, прежде чем позвать человека.
  escalate_after INTEGER NOT NULL DEFAULT 2,
  enabled        INTEGER NOT NULL DEFAULT 1,
  created_at     TEXT    NOT NULL DEFAULT (datetime('now')),
  updated_at     TEXT    NOT NULL DEFAULT (datetime('now'))
);

-- Инструмент бота: вызов чужого API или запрос в подключённую базу.
CREATE TABLE IF NOT EXISTS tools (
  id           INTEGER PRIMARY KEY,
  bot_id       INTEGER NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  kind         TEXT    NOT NULL CHECK (kind IN ('http', 'sql')),
  name         TEXT    NOT NULL,
  description  TEXT    NOT NULL DEFAULT '',
  -- Схема параметров в формате JSON Schema, как её ждёт модель.
  parameters   TEXT    NOT NULL DEFAULT '{}',
  -- Для http: метод, шаблон адреса, заголовки, тело.
  method       TEXT    NOT NULL DEFAULT 'GET',
  url          TEXT    NOT NULL DEFAULT '',
  headers      TEXT    NOT NULL DEFAULT '{}',
  body_template TEXT   NOT NULL DEFAULT '',
  -- Для sql: строка подключения и белый список таблиц.
  dsn          TEXT    NOT NULL DEFAULT '',
  driver       TEXT    NOT NULL DEFAULT 'sqlite',
  allowed_tables TEXT  NOT NULL DEFAULT '',
  row_limit    INTEGER NOT NULL DEFAULT 50,
  timeout_ms   INTEGER NOT NULL DEFAULT 5000,
  enabled      INTEGER NOT NULL DEFAULT 1,
  position     INTEGER NOT NULL DEFAULT 0,
  created_at   TEXT    NOT NULL DEFAULT (datetime('now')),
  UNIQUE (bot_id, name)
);

-- Разговор с одним посетителем.
CREATE TABLE IF NOT EXISTS conversations (
  id           INTEGER PRIMARY KEY,
  bot_id       INTEGER NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  token        TEXT    NOT NULL UNIQUE,
  visitor      TEXT    NOT NULL DEFAULT '',
  page_url     TEXT    NOT NULL DEFAULT '',
  -- open — говорит бот, waiting — ждёт человека, human — отвечает человек,
  -- closed — закрыт.
  state        TEXT    NOT NULL DEFAULT 'open'
                 CHECK (state IN ('open', 'waiting', 'human', 'closed')),
  escalated_at TEXT,
  escalate_reason TEXT NOT NULL DEFAULT '',
  failures     INTEGER NOT NULL DEFAULT 0,
  created_at   TEXT    NOT NULL DEFAULT (datetime('now')),
  updated_at   TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_conversations_bot   ON conversations(bot_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_conversations_state ON conversations(state, updated_at DESC);

-- Реплика. Роль human отличается от assistant: посетителю видно, что ответил
-- человек, а не бот.
CREATE TABLE IF NOT EXISTS messages (
  id              INTEGER PRIMARY KEY,
  conversation_id INTEGER NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  role            TEXT    NOT NULL
                    CHECK (role IN ('user', 'assistant', 'human', 'system')),
  text            TEXT    NOT NULL DEFAULT '',
  author          TEXT    NOT NULL DEFAULT '',
  created_at      TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_messages_conversation ON messages(conversation_id, id);

-- Чек ответа: почему бот сказал именно это. Одна строка на ответ бота.
CREATE TABLE IF NOT EXISTS receipts (
  id            INTEGER PRIMARY KEY,
  message_id    INTEGER NOT NULL UNIQUE REFERENCES messages(id) ON DELETE CASCADE,
  bot_id        INTEGER NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  provider      TEXT    NOT NULL DEFAULT '',
  model         TEXT    NOT NULL DEFAULT '',
  -- Шаги: обращения к модели и вызовы инструментов, в порядке выполнения.
  steps         TEXT    NOT NULL DEFAULT '[]',
  prompt_tokens     INTEGER NOT NULL DEFAULT 0,
  completion_tokens INTEGER NOT NULL DEFAULT 0,
  -- Токены рассуждения не видны в ответе, но оплачиваются. Держим отдельно,
  -- иначе стоимость занижается втрое.
  reasoning_tokens  INTEGER NOT NULL DEFAULT 0,
  cached_tokens     INTEGER NOT NULL DEFAULT 0,
  -- Стоимость в микрорублях, миллионных долях рубля. В копейках один ответ
  -- округляется в ноль: тысяча токенов по 32 копейки за миллион — это две
  -- сотых копейки, и весь чек показывал бы 0,00.
  cost_micro    INTEGER NOT NULL DEFAULT 0,
  took_ms       INTEGER NOT NULL DEFAULT 0,
  error         TEXT    NOT NULL DEFAULT '',
  created_at    TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_receipts_bot ON receipts(bot_id, created_at DESC);

-- Менеджер: тот, кто заходит в админку и принимает разговоры.
CREATE TABLE IF NOT EXISTS managers (
  id            INTEGER PRIMARY KEY,
  email         TEXT    NOT NULL UNIQUE,
  name          TEXT    NOT NULL DEFAULT '',
  password_hash TEXT    NOT NULL,
  created_at    TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS sessions (
  token      TEXT PRIMARY KEY,
  manager_id INTEGER NOT NULL REFERENCES managers(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
