import React from 'react';
import {Composition} from 'remotion';
import {Video} from './Video';
import {totalFrames} from './theme';

// Четыре сборки: два языка на два кадра.
//
// Широкий идёт на Reddit и в YouTube, вертикальный — в Телеграм и в короткие
// форматы, где горизонтальный ролик показывают маркой на пол-экрана.
export const RemotionRoot: React.FC = () => (
  <>
    <Composition
      id="Ru"
      component={Video}
      durationInFrames={totalFrames}
      fps={30}
      width={1920}
      height={1080}
      defaultProps={{lang: 'ru' as const, withMusic: true}}
    />
    <Composition
      id="En"
      component={Video}
      durationInFrames={totalFrames}
      fps={30}
      width={1920}
      height={1080}
      defaultProps={{lang: 'en' as const, withMusic: true}}
    />
    <Composition
      id="RuTall"
      component={Video}
      durationInFrames={totalFrames}
      fps={30}
      width={1080}
      height={1920}
      defaultProps={{lang: 'ru' as const, withMusic: true}}
    />
    <Composition
      id="EnTall"
      component={Video}
      durationInFrames={totalFrames}
      fps={30}
      width={1080}
      height={1920}
      defaultProps={{lang: 'en' as const, withMusic: true}}
    />
  </>
);
