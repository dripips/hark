import React from 'react';
import {useCurrentFrame, useVideoConfig} from 'remotion';
import {Mark, Scene, rise} from '../components/Chrome';
import {C, D, F} from '../theme';
import type {Copy} from '../copy';

// Финал: три команды и адрес. Ничего больше — призывов в кадре не будет.
export const Outro: React.FC<{copy: Copy}> = ({copy}) => {
  const frame = useCurrentFrame();
  const {width} = useVideoConfig();
  const tall = width < 1400;
  const o = copy.outro;

  return (
    <Scene title={o.title} foot={o.foot} duration={D.outro}>
      <Mark />

      <div
        style={{
          ...rise(frame, 10),
          padding: tall ? '26px 26px' : '28px 32px',
          borderRadius: 16,
          background: C.bgSoft,
          border: `1px solid ${C.line}`,
          fontFamily: F.mono,
          fontSize: tall ? 23 : 26,
          lineHeight: 1.85,
          marginBottom: tall ? 34 : 32,
          wordBreak: 'break-word',
        }}
      >
        {o.commands.map((line, i) => (
          <div key={line} style={{...rise(frame, 16 + i * 12), color: C.t1}}>
            <span style={{color: C.t3, marginRight: 14}}>$</span>
            {line}
          </div>
        ))}
      </div>

      <div
        style={{
          ...rise(frame, 58),
          fontFamily: F.mono,
          fontSize: tall ? 32 : 36,
          color: C.accent,
          letterSpacing: '-0.01em',
        }}
      >
        {o.repo}
      </div>
    </Scene>
  );
};
