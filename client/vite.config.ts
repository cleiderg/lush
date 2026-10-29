import { defineConfig } from 'vitest/config';

export default defineConfig({
  build: {
    // Maps are written for Sentry but the bundle never links to them; the deploy
    // workflow uploads them and deletes them before publishing.
    sourcemap: 'hidden',
  },
  test: {
    environment: 'node',
    include: ['src/**/*.test.ts'],
  },
});
