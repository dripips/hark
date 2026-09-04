// Весь текст ролика в одном месте: два языка рядом, чтобы правка не забывала
// один из них. Числа здесь настоящие, из чеков демонстрационной базы Hark.

export type Lang = 'ru' | 'en';

export type Copy = {
  hook: {big: string; under: string; note: string};
  receipt: {
    title: string;
    lead: string;
    steps: {kind: string; name: string; detail: string}[];
    totalLabel: string;
    total: string;
    thinkLabel: string;
    think: string;
    foot: string;
  };
  model: {title: string; lead: string; providers: string[]; probe: string[]; foot: string};
  data: {title: string; lead: string; query: string; guards: string[]; foot: string};
  widget: {title: string; lead: string; knobs: string[]; foot: string};
  outro: {title: string; commands: string[]; repo: string; foot: string};
};

export const COPY: Record<Lang, Copy> = {
  ru: {
    hook: {
      big: '86%',
      under: 'оплаченных токенов ответа',
      note: 'посетитель не видит ни одного',
    },
    receipt: {
      title: 'Чек к каждому ответу',
      lead: 'Что дёрнули, сколько заняло, во что обошлось',
      steps: [
        {kind: 'модель', name: 'gpt-5-nano', detail: 'ввод 191 · вывод 96 · рассуждение 64 · 1,8 с'},
        {kind: 'подключение', name: 'shop_db', detail: 'SELECT по заказам · 1 строка · 12 мс'},
        {kind: 'модель', name: 'gpt-5-nano', detail: 'ввод 318 · вывод 422 · рассуждение 384 · 3,1 с'},
      ],
      totalLabel: 'вывод, за который заплачено',
      total: '518 токенов',
      thinkLabel: 'из них рассуждение',
      think: '448 · 86%',
      foot: 'Личный кабинет поставщика этого не разделяет',
    },
    model: {
      title: 'Модель — ваша',
      lead: 'OpenAI, Anthropic и всё, что говорит в их формате',
      providers: ['OpenAI', 'Anthropic', 'Ollama', 'vLLM', 'LM Studio', 'свой сервер'],
      probe: [
        'temperature — отвергнута',
        'max_tokens — отвергнуто',
        'max_completion_tokens — принято',
        'инструменты — приняты',
      ],
      foot: 'Hark спрашивает модель сам и прячет ручки, которые она не берёт',
    },
    data: {
      title: 'Свои данные, а не догадки',
      lead: 'Запрос пишет модель, пускать его решает Hark',
      query: 'SELECT id, status, eta FROM orders WHERE id = 4127',
      guards: [
        'только SELECT',
        'таблицы по белому списку',
        'второй запрос отклоняется',
        'предел строк обёрткой, не припиской LIMIT',
      ],
      foot: 'Отклонённый запрос виден в чеке, а не заминается',
    },
    widget: {
      title: 'Один тег на страницу',
      lead: '23 КБ, по проводу семь',
      knobs: ['шрифт', 'отступы', 'цвета', 'фон', 'скругления', 'тень'],
      foot: 'В предпросмотре крутится сам виджет, а не макет',
    },
    outro: {
      title: 'Ставится к себе',
      commands: ['go build -o hark .', './hark -manager вы@example.com -password секрет', './hark'],
      repo: 'github.com/dripips/hark',
      foot: 'Один бинарник, база файлом рядом, четыре зависимости',
    },
  },

  en: {
    hook: {
      big: '86%',
      under: 'of the output tokens you pay for',
      note: 'the visitor never sees a single one',
    },
    receipt: {
      title: 'A receipt for every answer',
      lead: 'What was called, how long it took, what it cost',
      steps: [
        {kind: 'model', name: 'gpt-5-nano', detail: 'in 191 · out 96 · reasoning 64 · 1.8 s'},
        {kind: 'connection', name: 'shop_db', detail: 'SELECT on orders · 1 row · 12 ms'},
        {kind: 'model', name: 'gpt-5-nano', detail: 'in 318 · out 422 · reasoning 384 · 3.1 s'},
      ],
      totalLabel: 'output tokens billed',
      total: '518 tokens',
      thinkLabel: 'of which reasoning',
      think: '448 · 86%',
      foot: 'Your provider dashboard does not break this out',
    },
    model: {
      title: 'Bring your own model',
      lead: 'OpenAI, Anthropic, and anything speaking their format',
      providers: ['OpenAI', 'Anthropic', 'Ollama', 'vLLM', 'LM Studio', 'your server'],
      probe: [
        'temperature — refused',
        'max_tokens — refused',
        'max_completion_tokens — accepted',
        'tools — accepted',
      ],
      foot: 'Hark asks the model and hides the knobs it will not take',
    },
    data: {
      title: 'Your data, not guesses',
      lead: 'The model writes the query, Hark decides whether to run it',
      query: 'SELECT id, status, eta FROM orders WHERE id = 4127',
      guards: [
        'SELECT only',
        'tables against an allowlist',
        'a second statement is refused',
        'row cap by wrapping, not by appending LIMIT',
      ],
      foot: 'A refused query shows up in the receipt instead of being swallowed',
    },
    widget: {
      title: 'One script tag',
      lead: '23 KB, seven over the wire',
      knobs: ['font', 'spacing', 'colours', 'background', 'radii', 'shadow'],
      foot: 'The preview runs the actual widget, not a mockup',
    },
    outro: {
      title: 'Self-hosted',
      commands: ['go build -o hark .', './hark -manager you@example.com -password secret', './hark'],
      repo: 'github.com/dripips/hark',
      foot: 'One binary, a SQLite file beside it, four dependencies',
    },
  },
};
