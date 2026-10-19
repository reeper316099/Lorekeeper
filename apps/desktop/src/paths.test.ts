import path from 'node:path';
import { describe, expect, it } from 'vitest';
import { expectedUserData, resolveDataDir } from './paths';

describe('data dir resolution', () => {
  it('prefers env, then the Settings choice, then userData', () => {
    expect(resolveDataDir({ env: '/tmp/a', bootstrapDir: '/tmp/b', userData: '/u' })).toBe(
      '/tmp/a',
    );
    expect(resolveDataDir({ bootstrapDir: '/tmp/b', userData: '/u' })).toBe('/tmp/b');
    expect(resolveDataDir({ userData: '/u' })).toBe('/u');
  });
  it('ignores relative or blank overrides', () => {
    expect(resolveDataDir({ env: 'relative/dir', bootstrapDir: '  ', userData: '/u' })).toBe(
      path.resolve('/u'),
    );
  });
});

describe('default app-data locations', () => {
  it('matches the documented path on each OS', () => {
    expect(
      expectedUserData('win32', 'Lorekeeper', { APPDATA: 'C:\\Users\\me\\AppData\\Roaming' }),
    ).toBe('C:\\Users\\me\\AppData\\Roaming\\Lorekeeper');
    expect(expectedUserData('darwin', 'Lorekeeper', { HOME: '/Users/me' })).toBe(
      '/Users/me/Library/Application Support/Lorekeeper',
    );
    expect(expectedUserData('linux', 'Lorekeeper', { HOME: '/home/me' })).toBe(
      '/home/me/.config/Lorekeeper',
    );
    expect(
      expectedUserData('linux', 'Lorekeeper', { HOME: '/home/me', XDG_CONFIG_HOME: '/x' }),
    ).toBe('/x/Lorekeeper');
  });
});
