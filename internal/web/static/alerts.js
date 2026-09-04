/* Живая очередь ожидающих в админке.
 *
 * Пока этого не было, о том, что бот сдался и позвал человека, узнавали
 * единственным способом: перезагрузив страницу руками. Посетитель в это
 * время сидел и ждал.
 *
 * Сервер присылает не «стало на одного больше», а полный снимок очереди,
 * поэтому потерянное событие, уснувший ноутбук и перезапуск сервера чинятся
 * сами — следующим снимком. Здесь только показ: цифра в шапке, заголовок
 * вкладки, короткий звук и уведомление браузера, если его разрешили.
 *
 * Без этого файла админка работает как раньше: счётчик рисует сервер при
 * каждом показе страницы, и он верен на момент загрузки.
 */
(function () {
  'use strict';

  var badge = document.querySelector('[data-queue]');
  if (!badge) return;

  var baseTitle = document.title;
  var shown = Number(badge.getAttribute('data-queue') || 0);
  var lastSeq = 0;
  var stream = null;
  var polling = null;

  /* Звук делается на месте, файла нет: восемьдесят байт кода вместо запроса
   * за мелодией. Браузер не даёт играть до первого щелчка по странице,
   * поэтому контекст заводится по нему и переживает остальные события. */
  var audio = null;
  document.addEventListener('click', function () {
    if (audio || !window.AudioContext) return;
    try { audio = new AudioContext(); } catch (e) { audio = null; }
  }, { once: true });

  function beep() {
    if (!audio || audio.state !== 'running') return;
    var osc = audio.createOscillator();
    var gain = audio.createGain();
    osc.type = 'sine';
    osc.frequency.value = 660;
    gain.gain.setValueAtTime(0.0001, audio.currentTime);
    gain.gain.exponentialRampToValueAtTime(0.09, audio.currentTime + 0.02);
    gain.gain.exponentialRampToValueAtTime(0.0001, audio.currentTime + 0.28);
    osc.connect(gain).connect(audio.destination);
    osc.start();
    osc.stop(audio.currentTime + 0.3);
  }

  function notify(snapshot) {
    if (!window.Notification || Notification.permission !== 'granted') return;
    var top = (snapshot.top && snapshot.top[0]) || {};
    var body = top.reason || 'Посетитель ждёт человека.';
    try {
      var note = new Notification('Бот зовёт человека', { body: body, tag: 'hark-queue' });
      note.onclick = function () { window.focus(); location.href = '/inbox?state=waiting'; };
    } catch (e) { /* уведомления запрещены страницей — не беда, есть звук */ }
  }

  function paint(count) {
    badge.textContent = count > 0 ? String(count) : '';
    badge.hidden = count === 0;
    // Заголовок вкладки — единственное, что видно, когда админка на фоне.
    document.title = count > 0 ? '(' + count + ') ' + baseTitle : baseTitle;
  }

  function apply(snapshot) {
    // Снимок несёт полное значение и свой номер: пришедший с опозданием
    // старый снимок не должен возвращать уже показанную цифру назад.
    if (snapshot.seq && snapshot.seq < lastSeq) return;
    if (snapshot.seq) lastSeq = snapshot.seq;

    var count = Number(snapshot.count || 0);
    var grew = count > shown;
    shown = count;
    paint(count);

    if (grew) {
      beep();
      notify(snapshot);
    }
  }

  function connect() {
    if (!window.EventSource) { poll(); return; }

    stream = new EventSource('/events');
    stream.addEventListener('queue', function (event) {
      try { apply(JSON.parse(event.data)); } catch (e) { /* мусор в потоке — ждём следующий */ }
    });
    stream.addEventListener('bye', function () {
      // Сервер уходит на перезапуск. Молчим и ждём: EventSource вернётся сам.
      stream.close();
      stream = null;
      setTimeout(connect, 3000);
    });
    stream.onerror = function () {
      // Мест в потоке может не быть вовсе — тогда переходим на опрос и больше
      // не долбимся: браузер иначе будет переподключаться вечно.
      if (stream && stream.readyState === EventSource.CLOSED) {
        stream = null;
        poll();
      }
    };
  }

  // Запасной путь: опрос раз в полминуты. Нужен там, где потока нет — старый
  // браузер, прокси со склейкой, упёршийся потолок подписчиков.
  function poll() {
    if (polling) return;
    var ask = function () {
      fetch('/queue.json', { credentials: 'same-origin' })
        .then(function (r) { return r.ok ? r.json() : null; })
        .then(function (data) { if (data) apply(data); })
        .catch(function () { /* сеть моргнула — попробуем в следующий раз */ });
    };
    ask();
    polling = setInterval(ask, 30000);
  }

  // Разрешение на уведомления спрашивается по щелчку на счётчике, а не при
  // загрузке: непрошеный запрос браузеры прячут, и человек его не увидит.
  badge.addEventListener('click', function (event) {
    if (!window.Notification || Notification.permission !== 'default') return;
    event.preventDefault();
    Notification.requestPermission().then(function () {
      location.href = '/inbox?state=waiting';
    });
  });

  paint(shown);
  connect();
})();
