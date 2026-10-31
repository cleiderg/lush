import js from '@eslint/js';
import globals from 'globals';
import tseslint from 'typescript-eslint';

const forbiddenInState = {
  paths: [
    { name: 'three', message: 'src/state must not import three.' },
  ],
  patterns: [
    { group: ['three/*'], message: 'src/state must not import three.' },
    {
      group: ['**/render', '**/render/**', 'render', 'render/**'],
      message: 'src/state must not import src/render.',
    },
  ],
};

export default tseslint.config(
  { ignores: ['dist', 'node_modules', '.shots'] },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ['**/*.ts'],
    languageOptions: { globals: { ...globals.browser } },
  },
  {
    // Node scripts (npm run shots); page.evaluate callbacks run in the browser.
    files: ['scripts/**/*.mjs'],
    languageOptions: { globals: { ...globals.node, ...globals.browser } },
  },
  {
    files: ['src/state/**/*.ts'],
    rules: { 'no-restricted-imports': ['error', forbiddenInState] },
  },
);
