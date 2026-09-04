import React from 'react';
import {interpolate, useCurrentFrame, useVideoConfig} from 'remotion';
import {Mark, Scene, rise} from '../components/Chrome';
import {C, D, F} from '../theme';
import type {Copy} from '../copy';

// Виджет и его настройки.
//
// Окно перекрашивается прямо в кадре: это единственный способ за восемь секунд
// показать, что «настраивается» здесь не про один ползунок цвета.
const THEMES = [
  {surface: '#ffffff', text: '#14161a', accent: '#2f6df6', bubble: '#f1f3f7', radius: 18},
  {surface: '#fffaf3', text: '#2b2318', accent: '#8a5a2b', bubble: '#f3ebdd', radius: 6},
  {surface: '#0d1117', text: '#c9d1d9', accent: '#39d353', bubble: '#161b22', radius: 2},
  {surface: '#ffffff', text: '#15302c', accent: '#12a594', bubble: '#eaf6f4', radius: 26},
];

// Надпись на заливке выбирается по контрасту, а не берётся белой всегда.
// Ровно эта ошибка живёт в чужих конструкторах: салатовая кнопка с белой
// подписью не читается, и Hark у себя её как раз чинит.
const ink = (hex: string) => {
  const channel = (n: number) => {
    const v = n / 255;
    return v <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4);
  };
  const [r, g, b] = [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16));
  const luma = 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b);
  const onWhite = 1.05 / (luma + 0.05);
  const onBlack = (luma + 0.05) / 0.0665;
  return onWhite >= onBlack ? '#ffffff' : '#101215';
};

export const Widget: React.FC<{copy: Copy}> = ({copy}) => {
  const frame = useCurrentFrame();
  const {width} = useVideoConfig();
  const tall = width < 1400;
  const w = copy.widget;

  const step = Math.min(THEMES.length - 1, Math.floor(Math.max(0, frame - 30) / 46));
  const t = THEMES[step];
  const pop = interpolate((frame - 30) % 46, [0, 8], [0.985, 1], {
    extrapolateLeft: 'clamp',
    extrapolateRight: 'clamp',
  });

  return (
    <Scene title={w.title} lead={w.lead} foot={w.foot} duration={D.widget}>
      <Mark />

      <div
        style={{
          display: 'flex',
          flexDirection: tall ? 'column-reverse' : 'row',
          gap: tall ? 30 : 56,
          alignItems: 'center',
          justifyContent: 'flex-start',
        }}
      >
        <div
          style={{
            display: 'flex',
            flexWrap: 'wrap',
            gap: 12,
            maxWidth: tall ? '100%' : 520,
          }}
        >
          {w.knobs.map((knob, i) => (
            <div
              key={knob}
              style={{
                ...rise(frame, 18 + i * 10),
                padding: tall ? '14px 22px' : '13px 20px',
                borderRadius: 999,
                border: `1px solid ${C.lineStrong}`,
                background: C.bgSoft,
                fontSize: tall ? 27 : 25,
              }}
            >
              {knob}
            </div>
          ))}
        </div>

        {/* Окно виджета: те же части, что в настоящем, только нарисованные. */}
        <div
          style={{
            ...rise(frame, 12),
            width: tall ? 380 : 440,
            marginLeft: tall ? 0 : 'auto',
            background: t.surface,
            borderRadius: t.radius + 6,
            overflow: 'hidden',
            boxShadow: '0 30px 80px rgba(0,0,0,0.45)',
            transform: `scale(${pop})`,
            transition: 'none',
            fontFamily: F.sans,
            flex: 'none',
          }}
        >
          <div
            style={{
              background: t.accent,
              color: ink(t.accent),
              padding: '16px 20px',
              fontSize: 21,
              fontWeight: 600,
            }}
          >
            Помощник магазина
          </div>
          <div style={{padding: 20, display: 'flex', flexDirection: 'column', gap: 12}}>
            <div
              style={{
                alignSelf: 'flex-end',
                background: t.accent,
                color: ink(t.accent),
                padding: '12px 16px',
                borderRadius: t.radius,
                fontSize: 19,
                maxWidth: '80%',
              }}
            >
              Где мой заказ 4127?
            </div>
            <div
              style={{
                background: t.bubble,
                color: t.text,
                padding: '12px 16px',
                borderRadius: t.radius,
                fontSize: 19,
                maxWidth: '86%',
              }}
            >
              В пути, будет 12 сентября.
            </div>
            <div
              style={{
                marginTop: 6,
                border: `1px solid ${t.bubble}`,
                borderRadius: t.radius,
                padding: '12px 16px',
                color: t.text,
                opacity: 0.45,
                fontSize: 18,
              }}
            >
              Напишите сообщение…
            </div>
          </div>
        </div>
      </div>
    </Scene>
  );
};
