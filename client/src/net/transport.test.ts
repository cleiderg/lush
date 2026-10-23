import { describe, expect, it } from 'vitest';
import { serverUrl } from './transport';

describe('serverUrl', () => {
  it('defaults to the local dev server when VITE_SERVER_URL is unset', () => {
    expect(serverUrl()).toBe('ws://localhost:8080/ws');
  });
});
