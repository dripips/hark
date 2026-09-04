/* Виджет Hark. Один тег script на чужой странице:
 *
 *   <script src="https://hark.example.com/widget/hark.js" data-bot="shop" defer></script>
 *
 * Виджет собирается из настроек, а не из зашитого макета. Каждая часть
 * необязательна: пустое значение просто убирает её с экрана. Ничего не
 * заполнено — получается голая лента с полем ввода; заполнено всё — круглая
 * кнопка, приветственный экран с готовыми вопросами и сноска внизу.
 *
 * Зависимостей нет, разметка в теневом дереве: стили не текут ни в одну
 * сторону.
 */
(function () {
  'use strict';

  var script = document.currentScript;
  if (!script) return;

  var slug = script.getAttribute('data-bot');
  if (!slug) return;

  var origin = new URL(script.src, location.href).origin;
  var api = origin + '/api/widget';

  /* Предпросмотр в админке. data-preview — готовая строка запроса с
   * несохранённой темой, data-demo просит показать разговор, не заводя его:
   * иначе каждое движение ползунка сыпало бы пустые разговоры в ленту. */
  var preview = script.getAttribute('data-preview') || '';
  var demo = script.hasAttribute('data-demo');
  var token = null;
  var lastSeen = 0;
  var polling = null;
  var started = false;

  function request(path, options) {
    return fetch(api + path, options).then(function (r) {
      if (!r.ok) throw new Error('hark: ' + r.status);
      return r;
    });
  }

  request('/config?bot=' + encodeURIComponent(slug) + (preview ? '&' + preview : ''))
    .then(function (r) { return r.json(); })
    .then(build)
    .catch(function () { /* бот выключен или домен не разрешён — молчим */ });

  function build(config) {
    var host = document.createElement('div');
    host.setAttribute('data-hark', slug);
    document.body.appendChild(host);

    var root = host.attachShadow ? host.attachShadow({ mode: 'open' }) : host;
    var side = config.position === 'left' ? 'left' : 'right';
    var round = config.launcher_style === 'round';
    var mark = config.avatar || '';
    var quick = config.quick || [];
    var hasWelcome = !!(config.welcome_title || config.welcome_text || quick.length);

    var design = config.design || {};
    if (design.font_url) {
      // Свой шрифт живёт в теневом дереве вместе с виджетом и не трогает
      // страницу, на которую его поставили.
      var link = document.createElement('link');
      link.rel = 'stylesheet';
      link.href = design.font_url;
      root.appendChild(link);
    }

    var style = document.createElement('style');
    style.textContent = css(design, side);
    root.appendChild(style);

    var wrap = document.createElement('div');
    wrap.className = 'hk';
    wrap.appendChild(launcher());
    wrap.appendChild(panel());
    root.appendChild(wrap);

    var launchEl = wrap.querySelector('.hk__launch');
    var panelEl = wrap.querySelector('.hk__panel');
    var feed = wrap.querySelector('.hk__feed');
    var welcome = wrap.querySelector('.hk__welcome');
    var form = wrap.querySelector('.hk__form');
    var input = wrap.querySelector('.hk__input');

    launchEl.addEventListener('click', open);

    /* В предпросмотре сразу видно и кнопку, и открытое окно с примером
     * переписки: настройки цвета реплик иначе нечем оценить. Живой разговор
     * при этом не заводится — лента менеджера остаётся чистой. */
    var sample = [
      config.greeting || 'Здравствуйте! Чем помочь?',
      'Заказ 4412 уехал вчера, курьер привезёт завтра до 14:00.'
    ];
    if (demo) {
      open();
      launchEl.hidden = false;
      if (!hasWelcome) {
        add('assistant', sample[0]);
        add('user', 'Где мой заказ?');
        add('assistant', sample[1]);
      }
    }
    wrap.querySelector('.hk__close').addEventListener('click', function () {
      panelEl.hidden = true;
      launchEl.hidden = false;
      stopPolling();
    });

    wrap.querySelectorAll('.hk__quick button').forEach(function (button) {
      button.addEventListener('click', function () {
        ask(button.textContent);
      });
    });

    form.addEventListener('submit', function (event) {
      event.preventDefault();
      if (demo) { input.value = ''; return; }
      var text = input.value.trim();
      if (!text) return;
      input.value = '';
      ask(text);
    });

    // ── Разметка ────────────────────────────────────────────────────────

    function launcher() {
      var button = document.createElement('button');
      button.type = 'button';
      button.className = 'hk__launch' + (round ? ' hk__launch--round' : '');
      button.setAttribute('aria-haspopup', 'dialog');
      button.setAttribute('aria-label', config.launcher || config.name || 'Чат');
      button.appendChild(icon(round ? 26 : 18));
      if (!round) {
        button.appendChild(document.createTextNode(config.launcher || 'Спросить'));
      }
      return button;
    }

    function panel() {
      var section = document.createElement('section');
      section.className = 'hk__panel';
      section.setAttribute('role', 'dialog');
      section.setAttribute('aria-label', config.name || 'Чат');
      section.hidden = true;

      var head = document.createElement('header');
      head.className = 'hk__head';
      head.appendChild(icon(20, 'hk__head-icon'));

      var title = document.createElement('div');
      var name = document.createElement('b');
      name.textContent = config.name || 'Чат';
      title.appendChild(name);
      if (design.subtitle) {
        var sub = document.createElement('small');
        sub.textContent = design.subtitle;
        title.appendChild(sub);
      }
      head.appendChild(title);
      var close = document.createElement('button');
      close.type = 'button';
      close.className = 'hk__close';
      close.setAttribute('aria-label', 'Закрыть');
      close.textContent = '×';
      head.appendChild(close);
      section.appendChild(head);

      var body = document.createElement('div');
      body.className = 'hk__body';

      if (hasWelcome) {
        var hello = document.createElement('div');
        hello.className = 'hk__welcome';
        hello.appendChild(icon(34, 'hk__welcome-icon'));
        if (config.welcome_title) {
          var h = document.createElement('h2');
          h.textContent = config.welcome_title;
          hello.appendChild(h);
        }
        if (config.welcome_text) {
          var p = document.createElement('p');
          p.textContent = config.welcome_text;
          hello.appendChild(p);
        }
        if (quick.length) {
          var list = document.createElement('div');
          list.className = 'hk__quick';
          quick.forEach(function (text) {
            var button = document.createElement('button');
            button.type = 'button';
            button.textContent = text;
            list.appendChild(button);
          });
          hello.appendChild(list);
        }
        body.appendChild(hello);
      }

      var feedEl = document.createElement('div');
      feedEl.className = 'hk__feed';
      feedEl.setAttribute('aria-live', 'polite');
      body.appendChild(feedEl);
      section.appendChild(body);

      var formEl = document.createElement('form');
      formEl.className = 'hk__form';
      formEl.innerHTML =
        '<input class="hk__input" type="text" autocomplete="off">' +
        '<button class="hk__send" type="submit" aria-label="Отправить">' +
        '<svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">' +
        '<path fill="currentColor" d="M3 20.5 21 12 3 3.5l3.2 7.1L14 12l-7.8 1.4z"/></svg></button>';
      formEl.querySelector('.hk__input').placeholder = config.placeholder || 'Напишите сообщение…';
      section.appendChild(formEl);

      if (config.disclaimer || config.privacy_url) {
        var note = document.createElement('p');
        note.className = 'hk__note';
        if (config.disclaimer) {
          note.appendChild(document.createTextNode(config.disclaimer + ' '));
        }
        if (config.privacy_url) {
          var link = document.createElement('a');
          link.href = config.privacy_url;
          link.target = '_blank';
          link.rel = 'noopener';
          link.textContent = config.privacy_label || 'политика конфиденциальности';
          note.appendChild(link);
        }
        section.appendChild(note);
      }

      return section;
    }

    // Значок: свой символ из настроек или встроенные облачка разговора.
    function icon(size, className) {
      if (mark) {
        var span = document.createElement('span');
        span.className = 'hk__mark' + (className ? ' ' + className : '');
        span.style.fontSize = Math.round(size * 0.9) + 'px';
        span.textContent = mark;
        return span;
      }
      var ns = 'http://www.w3.org/2000/svg';
      var svg = document.createElementNS(ns, 'svg');
      svg.setAttribute('viewBox', '0 0 24 24');
      svg.setAttribute('width', size);
      svg.setAttribute('height', size);
      svg.setAttribute('aria-hidden', 'true');
      if (className) svg.setAttribute('class', className);
      var path = document.createElementNS(ns, 'path');
      path.setAttribute('fill', 'none');
      path.setAttribute('stroke', 'currentColor');
      path.setAttribute('stroke-width', '1.8');
      path.setAttribute('stroke-linejoin', 'round');
      path.setAttribute('d', 'M3.5 5.5h11v8h-7l-4 3v-11zM9.5 15.5h4l4 3v-3h2v-8h-3');
      svg.appendChild(path);
      return svg;
    }

    // ── Поведение ───────────────────────────────────────────────────────

    function open() {
      panelEl.hidden = false;
      launchEl.hidden = true;
      if (demo) return;
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
          // Приветствие показывается репликой, только когда экрана приветствия
          // нет: иначе получится два приветствия подряд.
          if (data.greeting && !hasWelcome) add('assistant', data.greeting);
          startPolling();
        })
        .catch(function () { add('error', 'Не получилось начать разговор.'); });
    }

    function ask(text) {
      if (demo) { hideWelcome(); add('user', text); add('assistant', sample[1]); return; }
      if (!token) return;
      hideWelcome();
      add('user', text);
      send(text);
    }

    // Приветственный экран уходит с первым вопросом: дальше он только мешает.
    function hideWelcome() {
      if (welcome && !welcome.hidden) {
        welcome.hidden = true;
        started = true;
      }
    }

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

    // Ответ менеджера не идёт потоком, поэтому лента спрашивает сама.
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
                hideWelcome();
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
      hideWelcome();
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

    function scroll() {
      var body = feed.parentNode;
      body.scrollTop = body.scrollHeight;
    }
  }

  /* Стили целиком собираются из темы: в правилах ниже нет ни одного зашитого
   * цвета или размера. Значения приходят посчитанными с сервера, поэтому
   * предпросмотр в админке и виджет на сайте рисуют одно и то же.
   *
   * Тёмная схема подключается только при scheme=auto: выбрав светлую или
   * тёмную явно, владелец получает её на любой системе. */
  function css(d, side) {
    var edge = side === 'left' ? 'start' : 'end';
    var pad = function (n) { return (d.step * n) + 'px'; };
    var size = function (delta) { return (d.font_size + delta) + 'px'; };

    var base = '' +
      '.hk{position:fixed;bottom:' + d.offset + 'px;' + side + ':' + d.offset + 'px;z-index:2147483000;' +
      'display:flex;flex-direction:column;align-items:flex-' + edge + ';gap:' + pad(3) + ';' +
      'font:' + d.font_size + 'px/' + d.line_height + ' ' + d.font + '}' +
      // display в правилах ниже перебивает атрибут hidden, поэтому нужен вес.
      '.hk [hidden]{display:none!important}' +
      '.hk *{box-sizing:border-box}' +

      '.hk__launch{display:inline-flex;align-items:center;gap:' + pad(2) + ';' +
      'padding:' + pad(3) + ' ' + pad(5) + ';border:0;border-radius:999px;' +
      'background:' + d.accent + ';color:' + d.on_accent + ';font:inherit;font-weight:500;' +
      'cursor:pointer;box-shadow:' + d.shadow + ';' +
      'transition:transform .25s cubic-bezier(.32,.72,0,1)}' +
      '.hk__launch:hover{transform:translateY(-2px)}' +
      '.hk__launch--round{width:58px;height:58px;padding:0;justify-content:center;gap:0}' +

      '.hk__panel{width:min(' + d.width + 'px,calc(100vw - 32px));' +
      'height:min(' + d.height + 'px,calc(100vh - 110px));' +
      'display:flex;flex-direction:column;background:' + d.surface + ';color:' + d.text + ';' +
      'border-radius:' + d.radius + 'px;overflow:hidden;box-shadow:' + d.shadow + ';' +
      'border:' + d.panel_border + '}' +

      '.hk__head{display:flex;align-items:center;gap:' + pad(2.5) + ';' +
      'padding:' + pad(3.5) + ' ' + pad(4) + ';background:' + d.header + ';' +
      'border-bottom:1px solid ' + d.border + ';color:' + d.head_text + '}' +
      '.hk__head b{display:block;font-size:' + size(0.5) + ';color:inherit}' +
      '.hk__head small{display:block;font-size:' + size(-3) + ';opacity:.72;' +
      'font-weight:400;margin-top:1px}' +
      '.hk__close{margin-left:auto;flex:none;width:30px;height:30px;border:0;border-radius:50%;' +
      'background:rgba(127,127,127,.16);color:inherit;font-size:19px;line-height:1;cursor:pointer}' +
      '.hk__close:hover{background:rgba(127,127,127,.28)}' +

      '.hk__body{flex:1;overflow-y:auto;display:flex;flex-direction:column;' +
      'background:' + d.backdrop + '}' +

      '.hk__welcome{padding:' + pad(7) + ' ' + pad(5) + ' ' + pad(5) + ';text-align:center;' +
      'color:' + d.ink + '}' +
      '.hk__welcome h2{margin:' + pad(3.5) + ' 0 ' + pad(1.5) + ';font-size:' + size(2) + ';' +
      'color:' + d.text + '}' +
      '.hk__welcome p{margin:0 0 ' + pad(4.5) + ';font-size:' + size(-1) + ';' +
      'line-height:1.5;color:' + d.muted + '}' +
      '.hk__quick{display:flex;flex-direction:column;gap:' + pad(2) + ';text-align:left}' +
      '.hk__quick button{padding:' + pad(2.75) + ' ' + pad(3.5) + ';' +
      'border:1px solid ' + d.border + ';border-radius:' + d.bubble + 'px;' +
      'background:' + d.surface + ';color:' + d.text + ';font:inherit;font-size:' + size(-1) + ';' +
      'text-align:left;cursor:pointer;transition:border-color .2s,transform .2s}' +
      '.hk__quick button:hover{border-color:' + d.accent + ';transform:translateX(2px)}' +

      '.hk__feed{display:flex;flex-direction:column;gap:' + pad(2.5) + ';padding:' + pad(4) + '}' +
      '.hk__feed:empty{padding:0}' +
      '.hk__msg{max-width:84%;padding:' + pad(2.5) + ' ' + pad(3.25) + ';' +
      'border-radius:' + d.bubble + 'px;background:' + d.bot_bubble + ';color:' + d.bot_text + ';' +
      'white-space:pre-wrap;word-break:break-word}' +
      '.hk__msg--user{align-self:flex-end;background:' + d.user_bubble + ';color:' + d.user_text + '}' +
      '.hk__msg--human{border:1px solid ' + d.accent + ';background:' + d.surface + '}' +
      '.hk__msg--error{background:#fee2e2;color:#991b1b}' +
      '.hk__msg--typing::after{content:"…";opacity:.55}' +
      '.hk__who{display:block;font-size:' + size(-4) + ';opacity:.6;margin-bottom:3px}' +

      '.hk__form{display:flex;gap:' + pad(2) + ';padding:' + pad(3) + ';' +
      'background:' + d.surface + ';border-top:1px solid ' + d.border + '}' +
      '.hk__input{flex:1;min-width:0;padding:' + pad(2.75) + ' ' + pad(3.25) + ';' +
      'border:1px solid ' + d.border + ';border-radius:' + d.bubble + 'px;' +
      'background:' + d.surface + ';color:' + d.text + ';font:inherit}' +
      '.hk__input::placeholder{color:' + d.muted + '}' +
      '.hk__input:focus{outline:2px solid ' + d.accent + ';outline-offset:1px}' +
      '.hk__send{flex:none;width:44px;border:0;border-radius:' + d.bubble + 'px;' +
      'background:' + d.accent + ';color:' + d.on_accent + ';' +
      'display:flex;align-items:center;justify-content:center;cursor:pointer}' +

      '.hk__note{margin:0;padding:0 ' + pad(3.5) + ' ' + pad(3) + ';background:' + d.surface + ';' +
      'font-size:' + size(-3.5) + ';line-height:1.45;color:' + d.muted + ';text-align:center}' +
      '.hk__note a{color:' + d.ink + '}' +

      '@media (prefers-reduced-motion:reduce){' +
      '.hk__launch:hover,.hk__quick button:hover{transform:none}}';

    // Схема auto: те же части, перекрашенные цветами владельца. Раньше здесь
    // стоял зашитый набор хексов, и бежевая панель в тёмной системе
    // становилась чужой серой.
    if (d.scheme === 'auto' && d.dark) {
      var k = d.dark;
      base += '@media (prefers-color-scheme:dark){' +
        '.hk__panel{background:' + k.surface + ';color:' + k.text + '}' +
        '.hk__body{background:' + k.backdrop + '}' +
        '.hk__head{background:' + k.header + ';color:' + k.head_text + ';' +
        'border-bottom-color:' + k.border + '}' +
        '.hk__welcome{color:' + k.ink + '}' +
        '.hk__welcome h2{color:' + k.text + '}.hk__welcome p{color:' + k.muted + '}' +
        '.hk__quick button{background:' + k.bot_bubble + ';border-color:' + k.border + ';' +
        'color:' + k.text + '}' +
        '.hk__msg{background:' + k.bot_bubble + ';color:' + k.bot_text + '}' +
        '.hk__msg--human{background:' + k.surface + '}' +
        '.hk__form{background:' + k.surface + ';border-top-color:' + k.border + '}' +
        '.hk__input{background:' + k.backdrop + ';border-color:' + k.border + ';color:' + k.text + '}' +
        '.hk__note{background:' + k.surface + ';color:' + k.muted + '}' +
        '.hk__note a{color:' + k.ink + '}}';
    }
    return base;
  }

})();
