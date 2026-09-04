import React from 'react';
import {useCurrentFrame, useVideoConfig} from 'remotion';
import {Mark, Scene, rise} from '../components/Chrome';
import {C, D, F} from '../theme';
import type {Copy} from '../copy';

// Поставщики и проба.
//
// Слева — список того, к чему можно подключиться, справа — что ответила
// конкретная модель. Правая колонка и есть суть: единого протокола нет, и
// узнать это можно только спросив.
export const Model: React.FC<{copy: Copy}> = ({copy}) => {
  const frame = useCurrentFrame();
  const {width} = useVideoConfig();
  const tall = width < 1400;
  const m = copy.model;

  const refused = (line: string) =>
    line.includes('отвергн') || line.includes('refused');

  return (
    <Scene title={m.title} lead={m.lead} foot={m.foot} duration={D.model}>
      <Mark />

      <div
        style={{
          display: 'flex',
          flexDirection: tall ? 'column' : 'row',
          gap: tall ? 34 : 46,
          alignItems: 'flex-start',
        }}
      >
        <div style={{flex: 1, display: 'flex', flexWrap: 'wrap', gap: 12}}>
          {m.providers.map((name, i) => (
            <div
              key={name}
              style={{
                ...rise(frame, 16 + i * 9),
                padding: tall ? '14px 22px' : '13px 20px',
                borderRadius: 999,
                border: `1px solid ${C.lineStrong}`,
                background: C.bgSoft,
                fontSize: tall ? 27 : 25,
                color: C.t1,
              }}
            >
              {name}
            </div>
          ))}
        </div>

        {/* Что ответила проба. Отвергнутое зачёркнуто: видно, что ручку не
            спрятали от лени, а именно попробовали. */}
        <div
          style={{
            ...rise(frame, 74),
            flex: 1,
            padding: tall ? '26px 28px' : '24px 28px',
            borderRadius: 18,
            background: C.bgSoft,
            border: `1px solid ${C.line}`,
            width: tall ? '100%' : undefined,
          }}
        >
          {m.probe.map((line, i) => {
            const off = refused(line);
            return (
              <div
                key={line}
                style={{
                  ...rise(frame, 82 + i * 12),
                  display: 'flex',
                  alignItems: 'center',
                  gap: 14,
                  padding: '10px 0',
                  borderBottom:
                    i === m.probe.length - 1 ? 'none' : `1px solid ${C.line}`,
                  fontFamily: F.mono,
                  fontSize: tall ? 24 : 23,
                  color: off ? C.t3 : C.accent,
                  textDecoration: off ? 'line-through' : 'none',
                }}
              >
                <span style={{fontFamily: F.sans, opacity: off ? 0.5 : 1}}>
                  {off ? '×' : '✓'}
                </span>
                {line}
              </div>
            );
          })}
        </div>
      </div>
    </Scene>
  );
};
