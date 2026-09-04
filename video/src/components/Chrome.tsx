import React from 'react';
import {AbsoluteFill, interpolate, useCurrentFrame, useVideoConfig} from 'remotion';
import {C, EASE, F} from '../theme';

// Общие детали кадра: фон, заголовок сцены, подпись внизу и мягкое появление.
//
// Всё собрано здесь, потому что ролик смотрят беззвучно и на телефоне: одна
// сетка на все сцены помогает глазу не искать заново, куда смотреть.

export const Backdrop: React.FC = () => {
  const frame = useCurrentFrame();
  // Медленное дыхание пятна: кадр не выглядит замершим, но и не отвлекает.
  const drift = interpolate(frame % 300, [0, 150, 300], [0, 26, 0]);
  return (
    <AbsoluteFill style={{background: C.bg}}>
      <AbsoluteFill
        style={{
          background: `radial-gradient(120% 90% at ${50 + drift / 6}% ${8 + drift / 10}%, ${C.accentSoft}, transparent 62%)`,
        }}
      />
      <AbsoluteFill
        style={{
          background: `radial-gradient(90% 70% at ${18 - drift / 8}% 96%, ${C.warmSoft}, transparent 58%)`,
        }}
      />
    </AbsoluteFill>
  );
};

// rise — появление снизу с затуханием. delay в кадрах.
export const rise = (frame: number, delay: number, distance = 26) => {
  const t = interpolate(frame - delay, [0, 22], [0, 1], {
    extrapolateLeft: 'clamp',
    extrapolateRight: 'clamp',
    easing: (x) => 1 - Math.pow(1 - x, 3),
  });
  return {opacity: t, transform: `translateY(${(1 - t) * distance}px)`};
};

// Уход сцены: последние двенадцать кадров гаснут, чтобы стык не резал глаз.
export const fadeOut = (frame: number, duration: number) =>
  interpolate(frame, [duration - 12, duration], [1, 0], {
    extrapolateLeft: 'clamp',
    extrapolateRight: 'clamp',
  });

export const Scene: React.FC<{
  title?: string;
  lead?: string;
  foot?: string;
  duration: number;
  children: React.ReactNode;
}> = ({title, lead, foot, duration, children}) => {
  const frame = useCurrentFrame();
  const {width} = useVideoConfig();
  // Вертикальный кадр: те же сцены, но крупнее и с большим полем.
  const tall = width < 1400;
  const pad = tall ? 72 : 120;

  return (
    <AbsoluteFill style={{opacity: fadeOut(frame, duration)}}>
      <Backdrop />
      <AbsoluteFill
        style={{
          padding: pad,
          display: 'flex',
          flexDirection: 'column',
          justifyContent: 'center',
          fontFamily: F.sans,
          color: C.t1,
        }}
      >
        {title ? (
          <div
            style={{
              ...rise(frame, 0),
              fontSize: tall ? 62 : 58,
              fontWeight: 700,
              letterSpacing: '-0.03em',
              marginBottom: 10,
            }}
          >
            {title}
          </div>
        ) : null}
        {lead ? (
          <div
            style={{
              ...rise(frame, 6),
              fontSize: tall ? 32 : 28,
              color: C.t2,
              marginBottom: tall ? 48 : 40,
              maxWidth: tall ? '100%' : '78%',
            }}
          >
            {lead}
          </div>
        ) : null}

        {children}

        {foot ? (
          <div
            style={{
              ...rise(frame, 60),
              marginTop: tall ? 46 : 38,
              fontSize: tall ? 26 : 23,
              color: C.t3,
              maxWidth: tall ? '100%' : '78%',
            }}
          >
            {foot}
          </div>
        ) : null}
      </AbsoluteFill>
    </AbsoluteFill>
  );
};

// Марка в углу: ролик расходится репостами, и без неё через два пересыла
// уже непонятно, о чём он был.
export const Mark: React.FC = () => {
  const {width, height} = useVideoConfig();
  const tall = width < 1400;
  return (
    <div
      style={{
        position: 'absolute',
        left: tall ? 72 : 120,
        top: height - (tall ? 96 : 84),
        fontFamily: F.sans,
        fontSize: tall ? 26 : 23,
        fontWeight: 700,
        color: C.t2,
        letterSpacing: '-0.02em',
      }}
    >
      hark<span style={{color: C.accent}}>.</span>
    </div>
  );
};

export const easing = EASE;
