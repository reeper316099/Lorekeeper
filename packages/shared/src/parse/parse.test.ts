import { describe, expect, it } from 'vitest';
import {
  parseProgress,
  behindBy,
  progressCell,
  parseLink,
  splitTags,
  suggestTagMerges,
  normalizeTitle,
  titleSimilarity,
  parseStatus,
  parseScore,
  parseSources,
  readableTextColor,
  slugify,
} from '../index.js';

describe('parseProgress', () => {
  it('parses numbers (int and string)', () => {
    expect(parseProgress(46)).toMatchObject({ raw: '46', number: 46, kind: 'number' });
    expect(parseProgress(' 111 ')).toMatchObject({ number: 111, kind: 'number' });
  });
  it('keeps raw text and parses what it can', () => {
    expect(parseProgress('End of S1')).toEqual({
      raw: 'End of S1',
      number: null,
      season: 1,
      kind: 'end',
    });
    expect(parseProgress('Beginning of S2')).toMatchObject({ season: 2, kind: 'start' });
    expect(parseProgress('S2 E1')).toEqual({ raw: 'S2 E1', number: 1, season: 2, kind: 'number' });
    expect(parseProgress('49?')).toEqual({ raw: '49?', number: 49, season: null, kind: 'text' });
    for (const w of ['Done', 'Completed', 'Finished', 'Finished?'])
      expect(parseProgress(w).kind).toBe('complete');
    expect(parseProgress('End').kind).toBe('end');
    const odd = parseProgress('A few chapters after Season 3?');
    expect(odd).toMatchObject({
      raw: 'A few chapters after Season 3?',
      kind: 'text',
      number: null,
    });
    expect(parseProgress('N/A').kind).toBe('text');
  });
  it('empty → null progress', () => {
    expect(parseProgress(null).raw).toBeNull();
    expect(parseProgress('  ').kind).toBeNull();
  });
  it('behindBy and cell round trip', () => {
    expect(behindBy(parseProgress(111), parseProgress(110))).toBe(1);
    expect(behindBy(parseProgress(111), parseProgress('End'))).toBeNull();
    expect(progressCell(parseProgress(46))).toBe(46);
    expect(progressCell(parseProgress('S2 E1'))).toBe('S2 E1');
  });
});

describe('parseLink', () => {
  it('adds https to scheme-less urls', () => {
    const l = parseLink('youtu.be/nQ5pQCTXeB0?si=abc', 'youtube')!;
    expect(l.url).toBe('https://youtu.be/nQ5pQCTXeB0?si=abc');
    expect(l.kind).toBe('youtube');
    expect(l.raw).toBe('youtu.be/nQ5pQCTXeB0?si=abc');
    expect(parseLink(' tapas.io/episode/2', 'other')!.url).toBe('https://tapas.io/episode/2');
  });
  it('keeps descriptive text as label with date and channel', () => {
    const l = parseLink('(S1 #full) Some Title (3/3/26) By: PanelPilot', 'youtube')!;
    expect(l.url).toBeNull();
    expect(l.label).toContain('Some Title');
    expect(l.date).toBe('3/3/26');
    expect(l.channel).toBe('PanelPilot');
  });
  it('extracts embedded urls and ignores empty', () => {
    expect(parseLink('see https://example.com/a.', 'other')!.url).toBe('https://example.com/a');
    expect(parseLink('  ')).toBeNull();
  });
});

describe('tags', () => {
  it('splits, trims, drops empties, dedupes case-insensitively', () => {
    expect(splitTags('Romance,Slapstick')).toEqual(['Romance', 'Slapstick']);
    expect(splitTags('Action, Isekai, ')).toEqual(['Action', 'Isekai']);
    expect(splitTags('Quest-like, Quest-Like')).toEqual(['Quest-like']);
  });
  it('suggests merges for typos and near-dups', () => {
    const s = suggestTagMerges({
      Sorcery: 10,
      Sorcrey: 1,
      Fantasy: 8,
      Fantsy: 1,
      'Rank Board': 5,
      'Rankk Board': 1,
      'Sword Arts': 3,
      'Sword Artss': 1,
      'Quest-like': 4,
      'Quest-Like': 1,
      'Sunny Days': 5,
      'Sunny Days(loud)': 1,
      Romance: 9,
    });
    const pairs = s.map((x) => `${x.from}>${x.to}`);
    expect(pairs).toContain('Sorcrey>Sorcery');
    expect(pairs).toContain('Fantsy>Fantasy');
    expect(pairs).toContain('Rankk Board>Rank Board');
    expect(pairs).toContain('Sword Artss>Sword Arts');
    expect(pairs).toContain('Quest-Like>Quest-like');
    expect(pairs).toContain('Sunny Days(loud)>Sunny Days');
    expect(s.some((x) => x.from === 'Romance' || x.to === 'Romance')).toBe(false);
  });
});

describe('title normalizer', () => {
  it('matches across quotes, punctuation, articles, unicode', () => {
    expect(normalizeTitle('The “Marlow’s” Plan!')).toBe(normalizeTitle('marlows plan'));
    expect(normalizeTitle('Mr. Marlow')).toBe(normalizeTitle('Mr Marlow'));
    expect(normalizeTitle('ＡＢＣ')).toBe('abc');
  });
  it('near-dups are similar but not equal', () => {
    const a = 'Quiet Harbor Without Mr. Pell';
    const b = 'Can You Do Quiet Harbor Without Mr Pelle?';
    expect(normalizeTitle(a)).not.toBe(normalizeTitle(b));
    expect(titleSimilarity(a, b)).toBeGreaterThan(0.6);
  });
});

describe('status/score/sources', () => {
  it('status aliases', () => {
    expect(parseStatus('Finished')).toMatchObject({ status: 'completed' });
    expect(parseStatus('plan to read')).toMatchObject({ status: 'planned', blank: false });
    expect(parseStatus('Re-Reading')).toMatchObject({ status: 'repeating' });
    expect(parseStatus(null)).toMatchObject({ status: 'planned', blank: true });
    expect(parseStatus('???')).toEqual({ ok: false, raw: '???' });
  });
  it('score labels from all sets', () => {
    expect(parseScore('7 - Good')).toBe(7);
    expect(parseScore(' 9 - Amazing')).toBe(9);
    expect(parseScore('10 - Masterpiece')).toBe(10);
    expect(parseScore('Great')).toBeNull();
    expect(parseScore(15)).toBe(10);
  });
  it('sources', () => {
    expect(parseSources('Youtube, Inkreader')).toEqual(['YouTube', 'Inkreader']);
    expect(parseSources('YouTube')).toEqual(['YouTube']);
    expect(parseSources('ClipHub')).toEqual(['ClipHub']);
  });
});

describe('misc', () => {
  it('readable text and slug', () => {
    expect(readableTextColor('#B71C1C')).toBe('#ffffff');
    expect(readableTextColor('#FFEB3B')).toBe('#000000');
    expect(slugify('Misc.')).toBe('misc');
    expect(slugify('Sci-Fi')).toBe('sci-fi');
    expect(slugify('Slice of Life')).toBe('slice-of-life');
  });
});
