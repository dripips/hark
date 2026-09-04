import React from 'react';
import {AbsoluteFill, interpolate, useCurrentFrame, useVideoConfig} from 'remotion';
import {Backdrop, fadeOut, rise} from '../components/Chrome';
import {C, D, F} from '../theme';
import type {Copy} from '../copy';

// Первая сцена — одно число.
//
// В ленте у ролика есть примерно секунда, чтобы объяснить, зачем его смотреть.
// Название продукта на это не годится: его никто не знает. А доля оплаченного,
// которую посетитель не видит, годится — она удивляет и без контекста.
export const Hook: React.FC<{copy: Copy}> = ({copy}) => {
  const frame = useCurrentFrame();
  const {width} = useVideoConfig();
  const tall = width < 1400;

  // Число набегает, а не появляется готовым: глаз цепляется за движение.
  const target = 86;
  const shown = Math.round(
    interpolate(frame, [8, 52], [0, target], {
      extrapolateLeft: 'clamp',
      extrapolateRight: 'clamp',
      easing: (x) => 1 - Math.pow(1 - x, 4),
    })
  );

  // Полоса под числом заполняется до той же доли: цифру видно и боковым зрением.
  const fill = interpolate(frame, [8, 52], [0, target / 100], {
    extrapolateLeft: 'clamp',
    extrapolateRight: 'clamp',
    easing: (x) => 1 - Math.pow(1 - x, 4),
  });

  return (
    <AbsoluteFill style={{opacity: fadeOut(frame, D.hook)}}>
      <Backdrop />
      <AbsoluteFill
        style={{
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          justifyContent: 'center',
          fontFamily: F.sans,
          padding: tall ? 72 : 120,
          textAlign: 'center',
        }}
      >
        <div
          style={{
            fontSize: tall ? 230 : 300,
            fontWeight: 800,
            letterSpacing: '-0.05em',
            lineHeight: 1,
            color: C.warm,
            fontVariantNumeric: 'tabular-nums',
          }}
        >
          {shown}%
        </div>

        <div
          style={{
            width: tall ? '86%' : '58%',
            height: 8,
            borderRadius: 999,
            background: 'rgba(255,255,255,0.09)',
            overflow: 'hidden',
            margin: '28px 0 40px',
          }}
        >
          <div
            style={{
              width: `${fill * 100}%`,
              height: '100%',
              background: C.warm,
              borderRadius: 999,
            }}
          />
        </div>

        <div
          style={{
            ...rise(frame, 46),
            fontSize: tall ? 40 : 40,
            color: C.t1,
            fontWeight: 500,
            marginBottom: 12,
          }}
        >
          {copy.hook.under}
        </div>
        <div style={{...rise(frame, 62), fontSize: tall ? 34 : 32, color: C.t2}}>
          {copy.hook.note}
        </div>
      </AbsoluteFill>
    </AbsoluteFill>
  );
};
