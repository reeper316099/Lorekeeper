import { describe, expect, it } from 'vitest';
import { activeFilterCount, defaultFilters, filtersToParams } from './store';

describe('filtersToParams', () => {
  it('sends only non-default filters', () => {
    expect(filtersToParams(defaultFilters).toString()).toBe('sort=title&order=asc');
  });
  it('serializes lists, tag mode and flags', () => {
    const p = filtersToParams({
      ...defaultFilters,
      q: ' magic ',
      library: 'reading',
      status: ['in_progress', 'planned'],
      tags: ['A', 'B'],
      tagMode: 'any',
      excludeTags: ['C'],
      behind: true,
      minScore: '7',
    });
    expect(p.get('q')).toBe('magic');
    expect(p.get('library')).toBe('reading');
    expect(p.get('status')).toBe('in_progress,planned');
    expect(p.get('tags')).toBe('A,B');
    expect(p.get('tagMode')).toBe('any');
    expect(p.get('excludeTags')).toBe('C');
    expect(p.get('behind')).toBe('true');
    expect(p.get('minScore')).toBe('7');
    // a search term leaves ordering to relevance unless a sort was chosen
    expect(p.has('sort')).toBe(false);
  });
  it('counts active filters', () => {
    expect(activeFilterCount(defaultFilters)).toBe(0);
    expect(
      activeFilterCount({
        ...defaultFilters,
        status: ['a', 'b'],
        favorite: true,
        library: 'watching',
      }),
    ).toBe(4);
  });
});
