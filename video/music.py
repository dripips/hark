# -*- coding: utf-8 -*-
"""Дорожка к ролику.

Написана здесь, а не взята со стока, по одной причине: со стоком приходится
держать в голове лицензию, а со своим — нет. Побочная выгода важнее: сцены
ролика меняются на 5, 14, 22, 30 и 38 секундах, и звук меняется ровно там же.
Готовый трек так не умеет.

Темп 120 ударов: доля равна половине секунды, и каждый стык сцены попадает
на долю. Тональность ля минор, ничего сложнее и не нужно — это подложка под
текст, а не музыка, которую слушают.

    python music.py           собрать public/music.mp3
    python music.py --wav     оставить и wav рядом

Дорожка необязательна: ролик собирается и без неё, см. README.
"""
import argparse
import io
import os
import shutil
import struct
import subprocess
import sys
import wave

import numpy as np

SR = 44100
BPM = 120
BEAT = 60.0 / BPM          # 0,5 с
BAR = BEAT * 4             # 2 с
LENGTH = 45.0

# Стыки сцен ролика. Ровно здесь музыка должна меняться, иначе монтаж и звук
# живут отдельными жизнями и ролик разваливается на слух.
CUTS = [0.0, 5.0, 14.0, 22.0, 30.0, 38.0, LENGTH]

A = 110.0  # ля большой октавы
SEMITONE = 2.0 ** (1.0 / 12.0)


def note(steps, base=A):
    return base * (SEMITONE ** steps)


def envelope(n, attack, decay, curve=3.0):
    """Огибающая: быстрый подъём, степенное затухание."""
    t = np.arange(n) / SR
    up = np.clip(t / max(attack, 1e-6), 0, 1)
    down = np.exp(-t / max(decay, 1e-6)) ** 1.0
    return up * down ** (1.0 / curve) * np.exp(-t / max(decay, 1e-6))


def kick(dur=0.42):
    """Бочка: синус, падающий со 120 к 45 герцам, плюс щелчок в атаке."""
    n = int(SR * dur)
    t = np.arange(n) / SR
    freq = 45 + 75 * np.exp(-t / 0.035)
    body = np.sin(2 * np.pi * np.cumsum(freq) / SR)
    body *= np.exp(-t / 0.16)
    click = np.random.default_rng(1).standard_normal(n) * np.exp(-t / 0.004) * 0.25
    return (body + click) * 0.9


def hat(dur=0.06, seed=2):
    """Закрытый хай-хэт: шум с быстрым спадом и подавленным низом.

    Громкость нарочно скромная. Первая сборка отдавала хай-хэту две трети
    всей энергии выше четырёх килогерц — под текстом это слышно как шипение,
    а не как ритм. Подложка не должна перетягивать внимание.
    """
    n = int(SR * dur)
    t = np.arange(n) / SR
    noise = np.random.default_rng(seed).standard_normal(n)
    # Верхний срез мягче, чем разность соседних отсчётов: та звенит.
    filtered = np.zeros(n)
    prev_in = prev_out = 0.0
    for i, x in enumerate(noise):
        prev_out = 0.72 * (prev_out + x - prev_in)
        prev_in = x
        filtered[i] = prev_out
    return filtered * np.exp(-t / 0.014) * 0.11


def sub(freq, dur):
    """Бас: синус с лёгкой второй гармоникой, чтобы не пропал на телефоне."""
    n = int(SR * dur)
    t = np.arange(n) / SR
    wave_ = np.sin(2 * np.pi * freq * t) + 0.18 * np.sin(4 * np.pi * freq * t)
    return wave_ * envelope(n, 0.008, dur * 0.7) * 0.55


def pluck(freq, dur=0.28):
    """Щипок для арпеджио: три гармоники и короткий хвост."""
    n = int(SR * dur)
    t = np.arange(n) / SR
    wave_ = (np.sin(2 * np.pi * freq * t)
             + 0.4 * np.sin(4 * np.pi * freq * t)
             + 0.18 * np.sin(6 * np.pi * freq * t))
    return wave_ * np.exp(-t / 0.09) * 0.22


def pad(freqs, dur, detune=0.004):
    """Подложка: расстроенные синусы, медленный вход и выход."""
    n = int(SR * dur)
    t = np.arange(n) / SR
    out = np.zeros(n)
    for i, f in enumerate(freqs):
        for d in (-detune, 0.0, detune):
            out += np.sin(2 * np.pi * f * (1 + d) * t + i * 0.7)
    out /= (len(freqs) * 3)
    fade = int(SR * 1.2)
    shape = np.ones(n)
    shape[:fade] = np.linspace(0, 1, fade) ** 2
    shape[-fade:] = np.linspace(1, 0, fade) ** 2
    return out * shape * 0.30


def place(track, sound, at):
    """Кладёт звук в дорожку, обрезая по её концу."""
    start = int(at * SR)
    if start >= len(track):
        return
    end = min(len(track), start + len(sound))
    track[start:end] += sound[:end - start]


def section(t):
    """Номер сцены для момента времени."""
    for i in range(len(CUTS) - 1):
        if CUTS[i] <= t < CUTS[i + 1]:
            return i
    return len(CUTS) - 2


def build():
    n = int(SR * LENGTH)
    drums = np.zeros(n)
    bassline = np.zeros(n)
    arp = np.zeros(n)
    pads = np.zeros(n)

    # ── подложка: аккорды на всю длину, меняются раз в четыре такта ──────
    chords = [
        [note(0), note(3), note(7)],    # Am
        [note(0), note(3), note(7)],
        [note(-4), note(0), note(3)],   # F
        [note(-2), note(2), note(5)],   # G
        [note(0), note(3), note(7)],    # Am
        [note(0), note(3), note(7)],
    ]
    span = LENGTH / len(chords)
    for i, ch in enumerate(chords):
        place(pads, pad([f * 2 for f in ch], span + 0.8), i * span)

    # ── ритм и бас по долям ──────────────────────────────────────────────
    # Бас ходит по той же гармонии, что и подложка: тоника, потом квинта.
    steps = [0, 0, 3, -2]
    beat = 0
    t = 0.0
    while t < LENGTH:
        s = section(t)

        # Бочка появляется со второй сцены и уходит на последней.
        if 1 <= s <= 4:
            place(drums, kick(), t)
        elif s == 5 and beat % 8 == 0:
            place(drums, kick(), t)

        # Хай-хэт — с третьей сцены, на слабые доли.
        if s >= 2 and s < 5:
            place(drums, hat(seed=beat % 7 + 2), t + BEAT / 2)

        # Бас — со второй, на первую и третью долю такта.
        if s >= 1 and beat % 2 == 0:
            place(bassline, sub(note(steps[(beat // 2) % len(steps)]), BEAT * 1.6), t)

        # Арпеджио — с четвёртой сцены, по восьмым.
        if 3 <= s <= 4:
            chord = chords[min(int(t / span), len(chords) - 1)]
            for k in range(4):
                place(arp, pluck(chord[k % len(chord)] * 4), t + k * BEAT / 4)

        t += BEAT
        beat += 1

    # ── бас пригибается под бочкой ──────────────────────────────────────
    # Иначе низ бочки и низ баса складываются в кашу: этот приём и есть то,
    # за что минимал-техно звучит собранно, а не мутно.
    duck = np.ones(n)
    t = 0.0
    while t < LENGTH:
        s = section(t)
        if 1 <= s <= 4:
            start = int(t * SR)
            hold = int(SR * 0.22)
            shape = np.linspace(0.35, 1.0, hold) ** 0.6
            end = min(n, start + hold)
            duck[start:end] = np.minimum(duck[start:end], shape[:end - start])
        t += BEAT
    bassline *= duck
    pads *= 0.55 + 0.45 * duck

    mix = drums * 0.9 + bassline * 1.0 + arp * 0.8 + pads * 1.0

    # ── мягкое ограничение и нормализация ───────────────────────────────
    mix = np.tanh(mix * 1.15) * 0.9
    peak = np.max(np.abs(mix))
    if peak > 0:
        mix = mix / peak * 0.89     # запас до перегруза

    # Вход и выход, чтобы не щёлкало на стыке с тишиной.
    edge = int(SR * 0.35)
    mix[:edge] *= np.linspace(0, 1, edge) ** 2
    mix[-int(SR * 1.6):] *= np.linspace(1, 0, int(SR * 1.6)) ** 1.5

    # Лёгкое расширение по сторонам: та же дорожка с задержкой в 8 мс.
    delay = int(SR * 0.008)
    left = mix.copy()
    right = np.concatenate([np.zeros(delay), mix[:-delay]])
    return np.stack([left, right * 0.98], axis=1)


def write_wav(path, stereo):
    data = np.clip(stereo, -1.0, 1.0)
    pcm = (data * 32767).astype('<i2')
    with wave.open(path, 'wb') as f:
        f.setnchannels(2)
        f.setsampwidth(2)
        f.setframerate(SR)
        f.writeframes(pcm.tobytes())


def find_ffmpeg():
    local = os.path.join('node_modules', '@remotion',
                         'compositor-win32-x64-msvc', 'ffmpeg.exe')
    if os.path.exists(local):
        return local
    return shutil.which('ffmpeg')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--wav', action='store_true', help='оставить wav рядом с mp3')
    args = parser.parse_args()

    os.makedirs('public', exist_ok=True)
    stereo = build()

    wav_path = os.path.join('public', 'music.wav')
    write_wav(wav_path, stereo)

    ffmpeg = find_ffmpeg()
    if not ffmpeg:
        print('ffmpeg не найден: оставляю только', wav_path)
        return

    mp3_path = os.path.join('public', 'music.mp3')
    subprocess.run([ffmpeg, '-y', '-loglevel', 'error', '-i', wav_path,
                    '-codec:a', 'libmp3lame', '-b:a', '192k', mp3_path], check=True)
    if not args.wav:
        os.remove(wav_path)

    size = os.path.getsize(mp3_path)
    peak = float(np.max(np.abs(stereo)))
    rms = float(np.sqrt(np.mean(stereo ** 2)))
    print('собрано:', mp3_path, f'{size / 1024:.0f} КБ, {LENGTH:.0f} с')
    print(f'пик {20 * np.log10(peak):.1f} дБ, средняя громкость {20 * np.log10(rms):.1f} дБ')


if __name__ == '__main__':
    main()
