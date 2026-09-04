import React from 'react';
import {AbsoluteFill, Audio, Sequence, staticFile} from 'remotion';
import {COPY, type Lang} from './copy';
import {D} from './theme';
import {Hook} from './scenes/Hook';
import {Receipt} from './scenes/Receipt';
import {Model} from './scenes/Model';
import {Data} from './scenes/Data';
import {Widget} from './scenes/Widget';
import {Outro} from './scenes/Outro';

// Дорожка необязательна.
//
// И Телеграм, и Reddit проигрывают ролик беззвучно, пока в него не ткнут.
// Значит, весь смысл несёт текст, а музыка — приятная добавка для площадок,
// где звук включён. Положите файл в public/music.mp3, и он подхватится сам;
// без него ролик собирается ровно так же.
const MUSIC = 'music.mp3';

export const Video: React.FC<{lang: Lang; withMusic?: boolean}> = ({lang, withMusic}) => {
  const copy = COPY[lang];

  let at = 0;
  const next = (length: number) => {
    const from = at;
    at += length;
    return from;
  };

  return (
    <AbsoluteFill>
      <Sequence from={next(D.hook)} durationInFrames={D.hook}>
        <Hook copy={copy} />
      </Sequence>
      <Sequence from={next(D.receipt)} durationInFrames={D.receipt}>
        <Receipt copy={copy} />
      </Sequence>
      <Sequence from={next(D.model)} durationInFrames={D.model}>
        <Model copy={copy} />
      </Sequence>
      <Sequence from={next(D.data)} durationInFrames={D.data}>
        <Data copy={copy} />
      </Sequence>
      <Sequence from={next(D.widget)} durationInFrames={D.widget}>
        <Widget copy={copy} />
      </Sequence>
      <Sequence from={next(D.outro)} durationInFrames={D.outro}>
        <Outro copy={copy} />
      </Sequence>

      {withMusic ? <Audio src={staticFile(MUSIC)} volume={0.45} /> : null}
    </AbsoluteFill>
  );
};
