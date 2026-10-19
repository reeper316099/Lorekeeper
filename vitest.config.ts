import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    globalSetup: ['scripts/vitest-global-setup.ts'],
    include: [
      'packages/*/src/**/*.test.ts',
      'packages/*/test/**/*.test.ts',
      'scripts/**/*.test.ts',
    ],
    testTimeout: 20000,
  },
});
