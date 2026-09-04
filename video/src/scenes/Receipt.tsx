import React from 'react';
import {interpolate, useCurrentFrame, useVideoConfig} from 'remotion';
import {Mark, Scene, rise} from '../components/Chrome';
import {C, D, F} from '../theme';
import type {Copy} from '../copy';

// Чек: шаги появляются по одному, будто ответ собирается на глазах.
export const Receipt: React.FC<{copy: Copy}> = ({copy}) => {
  const frame = useCurrentFrame();
  const {width} = useVideoConfig();
  const tall = width < 1400;
  const r = copy.receipt;

  // Доля рассуждения в итоге закрашивается тёплым: ровно то число из первой
  // сцены, но теперь видно, откуда оно взялось.
  const share = interpolate(frame, [120, 165], [0, 0.86], {
    extrapolateLeft: 'clamp',
    extrapolateRight: 'clamp',
    easing: (x) => 1 - Math.pow(1 - x, 3),
  });

  return (
    <Scene title={r.title} lead={r.lead} foot={r.foot} duration={D.receipt}>
      <Mark />

      <div style={{display: 'flex', flexDirection: 'column', gap: tall ? 14 : 12}}>
        {r.steps.map((step, i) => (
          <div
            key={step.name + i}
            style={{
              ...rise(frame, 18 + i * 18),
              display: 'flex',
              alignItems: 'center',
              gap: tall ? 18 : 22,
              padding: tall ? '20px 24px' : '18px 26px',
              borderRadius: 16,
              background: C.bgSoft,
              border: `1px solid ${step.kind === 'model' || step.kind === 'модель' ? C.line : C.accentSoft}`,
            }}
          >
            <div
              style={{
                fontSize: tall ? 22 : 20,
                color: step.kind === 'model' || step.kind === 'модель' ? C.t3 : C.accent,
                minWidth: tall ? 150 : 170,
                textTransform: 'lowercase',
              }}
            >
              {step.kind}
            </div>
            <div style={{flex: 1, minWidth: 0}}>
              <div
                style={{
                  fontFamily: F.mono,
                  fontSize: tall ? 26 : 25,
                  color: C.t1,
                  marginBottom: 4,
                }}
              >
                {step.name}
              </div>
              <div style={{fontSize: tall ? 22 : 21, color: C.t2}}>{step.detail}</div>
            </div>
          </div>
        ))}
      </div>

      {/* Итог: сколько вывода оплачено и какая его часть невидима. */}
      <div
        style={{
          ...rise(frame, 108),
          marginTop: tall ? 28 : 24,
          padding: tall ? '24px 26px' : '22px 28px',
          borderRadius: 16,
          background: C.bgSoft,
          border: `1px solid ${C.lineStrong}`,
        }}
      >
        <div
          style={{
            display: 'flex',
            alignItems: 'baseline',
            gap: 16,
            flexWrap: 'wrap',
            marginBottom: 16,
          }}
        >
          <span style={{fontSize: tall ? 24 : 22, color: C.t2}}>{r.totalLabel}</span>
          <b style={{fontSize: tall ? 34 : 32, fontFamily: F.mono}}>{r.total}</b>
          <span style={{marginLeft: 'auto', fontSize: tall ? 24 : 22, color: C.warm}}>
            {r.thinkLabel} <b style={{fontFamily: F.mono}}>{r.think}</b>
          </span>
        </div>

        <div
          style={{
            height: 14,
            borderRadius: 999,
            background: 'rgba(255,255,255,0.07)',
            overflow: 'hidden',
          }}
        >
          <div style={{width: `${share * 100}%`, height: '100%', background: C.warm}} />
        </div>
      </div>
    </Scene>
  );
};
