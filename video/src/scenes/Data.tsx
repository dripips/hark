import React from 'react';
import {interpolate, useCurrentFrame, useVideoConfig} from 'remotion';
import {Mark, Scene, rise} from '../components/Chrome';
import {C, D, F} from '../theme';
import type {Copy} from '../copy';

// Запрос печатается на глазах, ограничители зажигаются под ним.
//
// Печать здесь не украшение: она показывает, что SQL пишет модель, а не
// человек, — и именно поэтому дальше нужен список того, что Hark не пропустит.
export const Data: React.FC<{copy: Copy}> = ({copy}) => {
  const frame = useCurrentFrame();
  const {width} = useVideoConfig();
  const tall = width < 1400;
  const d = copy.data;

  const chars = Math.round(
    interpolate(frame, [14, 76], [0, d.query.length], {
      extrapolateLeft: 'clamp',
      extrapolateRight: 'clamp',
    })
  );
  const typed = d.query.slice(0, chars);
  const caret = frame < 80 && Math.floor(frame / 8) % 2 === 0;

  return (
    <Scene title={d.title} lead={d.lead} foot={d.foot} duration={D.data}>
      <Mark />

      <div
        style={{
          ...rise(frame, 10),
          padding: tall ? '26px 26px' : '26px 30px',
          borderRadius: 16,
          background: C.bgSoft,
          border: `1px solid ${C.line}`,
          fontFamily: F.mono,
          fontSize: tall ? 24 : 26,
          color: C.t1,
          marginBottom: tall ? 28 : 26,
          minHeight: tall ? 110 : 90,
          lineHeight: 1.5,
          wordBreak: 'break-word',
        }}
      >
        <span style={{color: C.t3, marginRight: 12}}>sql</span>
        {typed}
        <span style={{opacity: caret ? 1 : 0, color: C.accent}}>▌</span>
      </div>

      <div style={{display: 'flex', flexDirection: 'column', gap: 12}}>
        {d.guards.map((line, i) => (
          <div
            key={line}
            style={{
              ...rise(frame, 84 + i * 14),
              display: 'flex',
              alignItems: 'center',
              gap: 16,
              fontSize: tall ? 28 : 27,
              color: C.t1,
            }}
          >
            <span
              style={{
                width: tall ? 34 : 32,
                height: tall ? 34 : 32,
                borderRadius: 999,
                background: C.accentSoft,
                color: C.accent,
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                fontSize: tall ? 20 : 19,
                flex: 'none',
              }}
            >
              ✓
            </span>
            {line}
          </div>
        ))}
      </div>
    </Scene>
  );
};
