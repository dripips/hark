/* Виджет Hark. Один тег script на чужой странице:
 *
 *   <script src="https://hark.example.com/widget/hark.js" data-bot="shop" defer></script>
 *
 * Зависимостей нет, стили свои и с префиксом, разметка в теневом дереве —
 * чтобы вёрстка чужого сайта не поехала и не поехала от него сама.
 */
(function () {
  'use strict';

  var script = document.currentScript;
  if (!script) return;

  var slug = script.getAttribute('data-bot');
  if (!slug) return;

  var origin = new URL(script.src, location.href).origin;
  var api = origin + '/api/widget';
  var token = null;
  var lastSeen = 0;
  var polling = null;

  function request(path, options) {
    return fetch(api + path, options).then(function (r) {
      if (!r.ok) throw new Error('hark: ' + r.status);
      return r;
    });
  }

  request('/config?bot=' + encodeURIComponent(slug))
    .then(function (r) { return r.json(); })
    .then(build)
    .catch(function () { /* бот выключен или домен не разрешён — молча ничего не показываем */ });

  function build(config) {
    var host = document.createElement('div');
    host.setAttribute('data-hark', slug);
    document.body.appendChild(host);

    var root = host.attachShadow ? host.attachShadow({ mode: 'open' }) : host;
    var accent = config.accent || '#059669';
    var side = config.position === 'left' ? 'left' : 'right';

    var style = document.createElement('style');
    style.textContent = css(accent, side);
    root.appendChild(style);

    var wrap = document.createElement('div');
    wrap.className = 'hk';
    wrap.innerHTML =
      '<button class="hk__launch" type="button" aria-haspopup="dialog">' +
        '<span class="hk__dot"></span>' + escapeHTML(config.launcher || 'Спросить') +
      '</button>' +
      '<section class="hk__panel" role="dialog" aria-label="' + escapeHTML(config.name || 'Чат') + '" hidden>' +
        '<header class="hk__head">' +
          '<b>' + escapeHTML(config.name || 'Чат') + '</b>' +
          '<button class="hk__close" type="button" aria-label="Закрыть">×</button>' +
        '</header>' +
        '<div class="hk__feed" aria-live="polite"></div>' +
        '<form class="hk__form">' +
          '<input class="hk__input" type="text" placeholder="Спросите что-нибудь" autocomplete="off">' +
          '<button class="hk__send" type="submit" aria-label="Отправить">→</button>' +
        '</form>' +
      '</section>';
    root.appendChild(wrap);

    var launch = wrap.querySelector('.hk__launch');
    var panel = wrap.querySelector('.hk__panel');
    var feed = wrap.querySelector('.hk__feed');
    var form = wrap.querySelector('.hk__form');
    var input = wrap.querySelector('.hk__input');

    launch.addEventListener('click', function () { open(); });
    wrap.querySelector('.hk__close').addEventListener('click', function () {
      panel.hidden = true;
      launch.hidden = false;
      stopPolling();
    });

    function open() {
      panel.hidden = false;
      launch.hidden = true;
      input.focus();
      if (token) { startPolling(); return; }

      request('/start?bot=' + encodeURIComponent(slug), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ page_url: location.href })
      })
        .then(function (r) { return r.json(); })
        .then(function (data) {
          token = data.token;
          if (data.greeting) add('assistant', data.greeting);
          startPolling();
        })
        .catch(function () { add('error', 'Не получилось начать разговор.'); });
    }

    form.addEventListener('submit', function (event) {
      event.preventDefault();
      var text = input.value.trim();
      if (!text || !token) return;
      input.value = '';
      add('user', text);
      send(text);
    });

    function send(text) {
      var bubble = add('assistant', '');
      bubble.classList.add('hk__msg--typing');

      fetch(api + '/send', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ token: token, text: text })
      }).then(function (response) {
        if (!response.ok || !response.body) throw new Error('bad');
        var reader = response.body.getReader();
        var decoder = new TextDecoder();
        var buffer = '';

        function pump() {
          return reader.read().then(function (chunk) {
            if (chunk.done) { bubble.classList.remove('hk__msg--typing'); return; }
            buffer += decoder.decode(chunk.value, { stream: true });

            var parts = buffer.split('\n\n');
            buffer = parts.pop();
            parts.forEach(function (block) { handle(block, bubble); });
            return pump();
          });
        }
        return pump();
      }).catch(function () {
        bubble.classList.remove('hk__msg--typing');
        bubble.textContent = 'Связь оборвалась. Попробуйте ещё раз.';
      });
    }

    function handle(block, bubble) {
      var event = '';
      var data = '';
      block.split('\n').forEach(function (line) {
        if (line.indexOf('event: ') === 0) event = line.slice(7).trim();
        if (line.indexOf('data: ') === 0) data += line.slice(6);
      });
      if (!event) return;

      var payload = {};
      try { payload = JSON.parse(data || '{}'); } catch (e) { return; }

      if (event === 'chunk' && payload.text) {
        bubble.classList.remove('hk__msg--typing');
        bubble.textContent += payload.text;
        scroll();
      } else if (event === 'message' && payload.text) {
        // Итоговый текст перекрывает собранный по кускам: если поток оборвался
        // на середине, посетитель всё равно увидит ответ целиком.
        bubble.classList.remove('hk__msg--typing');
        bubble.textContent = payload.text;
        lastSeen = Math.max(lastSeen, payload.id || 0);
        scroll();
      } else if (event === 'waiting' && payload.text) {
        bubble.classList.remove('hk__msg--typing');
        bubble.textContent = payload.text;
      } else if (event === 'error') {
        bubble.classList.remove('hk__msg--typing');
        bubble.textContent = 'Не получилось ответить.';
      }
    }

    // Ответ менеджера приходит не потоком, поэтому лента спрашивает сама.
    function startPolling() {
      stopPolling();
      polling = setInterval(function () {
        if (!token) return;
        request('/poll?token=' + encodeURIComponent(token) + '&after=' + lastSeen)
          .then(function (r) { return r.json(); })
          .then(function (data) {
            (data.messages || []).forEach(function (m) {
              lastSeen = Math.max(lastSeen, m.id);
              if (m.role === 'human') {
                add('human', m.text, m.author);
              }
            });
          })
          .catch(function () { /* сеть моргнула — попробуем в следующий раз */ });
      }, 4000);
    }

    function stopPolling() {
      if (polling) { clearInterval(polling); polling = null; }
    }

    function add(role, text, author) {
      var node = document.createElement('div');
      node.className = 'hk__msg hk__msg--' + role;
      if (role === 'human' && author) {
        var tag = document.createElement('span');
        tag.className = 'hk__who';
        tag.textContent = author;
        node.appendChild(tag);
      }
      node.appendChild(document.createTextNode(text));
      feed.appendChild(node);
      scroll();
      return node;
    }

    function scroll() { feed.scrollTop = feed.scrollHeight; }
  }

  function escapeHTML(s) {
    return String(s).replace(/[&<>"']/g, function (c) {
      return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
    });
  }

  function css(accent, side) {
    return '' +
      '.hk{position:fixed;bottom:20px;' + side + ':20px;z-index:2147483000;' +
      'display:flex;flex-direction:column;align-items:flex-' + (side === 'left' ? 'start' : 'end') + ';' +
      'font:15px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif}' +
      // display в правилах ниже перебивает атрибут hidden, и кнопка
      // оставалась висеть поверх открытой панели.
      '.hk [hidden]{display:none!important}' +
      '.hk__launch{display:inline-flex;align-items:center;gap:8px;padding:12px 18px;border:0;' +
      'border-radius:999px;background:' + accent + ';color:#fff;font:inherit;font-weight:500;' +
      'cursor:pointer;box-shadow:0 6px 24px rgba(0,0,0,.18)}' +
      '.hk__dot{width:8px;height:8px;border-radius:50%;background:#fff;opacity:.9}' +
      '.hk__panel{width:min(380px,calc(100vw - 32px));height:min(560px,calc(100vh - 96px));' +
      'display:flex;flex-direction:column;background:#fff;color:#111;border-radius:18px;' +
      'overflow:hidden;box-shadow:0 12px 48px rgba(0,0,0,.22)}' +
      '.hk__head{display:flex;align-items:center;padding:14px 16px;background:' + accent + ';color:#fff}' +
      '.hk__head b{font-size:15px}' +
      '.hk__close{margin-left:auto;background:none;border:0;color:#fff;font-size:22px;' +
      'line-height:1;cursor:pointer;opacity:.85}' +
      '.hk__feed{flex:1;overflow-y:auto;padding:16px;display:flex;flex-direction:column;gap:10px}' +
      '.hk__msg{max-width:84%;padding:10px 13px;border-radius:14px;background:#f1f2f4;' +
      'white-space:pre-wrap;word-break:break-word}' +
      '.hk__msg--user{align-self:flex-end;background:' + accent + ';color:#fff}' +
      '.hk__msg--human{border:1px solid ' + accent + ';background:#fff}' +
      '.hk__msg--error{background:#fee2e2;color:#991b1b}' +
      '.hk__msg--typing::after{content:"…";opacity:.55}' +
      '.hk__who{display:block;font-size:11px;opacity:.6;margin-bottom:3px}' +
      '.hk__form{display:flex;gap:8px;padding:12px;border-top:1px solid #e6e7ea}' +
      '.hk__input{flex:1;padding:10px 12px;border:1px solid #dcdee2;border-radius:11px;font:inherit}' +
      '.hk__input:focus{outline:2px solid ' + accent + ';outline-offset:1px}' +
      '.hk__send{width:42px;border:0;border-radius:11px;background:' + accent + ';color:#fff;' +
      'font-size:18px;cursor:pointer}' +
      '@media (prefers-color-scheme:dark){' +
      '.hk__panel{background:#16181b;color:#f2f2f3}' +
      '.hk__msg{background:#23262a}' +
      '.hk__msg--human{background:#16181b}' +
      '.hk__form{border-top-color:#2a2d31}' +
      '.hk__input{background:#0f1113;border-color:#2a2d31;color:#f2f2f3}}';
  }
})();
