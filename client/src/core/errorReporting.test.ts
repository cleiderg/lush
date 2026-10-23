import { describe, expect, it, vi } from 'vitest';
import { initErrorReporting, REDACTED, scrubBreadcrumb, scrubEvent, type SentrySdk } from './errorReporting';

function mockSdk() {
  return { init: vi.fn(), captureException: vi.fn() } satisfies SentrySdk;
}

function target() {
  const handlers: Record<string, (e: unknown) => void> = {};
  return { handlers, addEventListener: (t: string, h: (e: unknown) => void) => void (handlers[t] = h) };
}

describe('initErrorReporting', () => {
  it('does not init without a DSN', () => {
    const sdk = mockSdk();
    const t = target();
    expect(initErrorReporting({ VITE_RELEASE: 'abc' }, sdk, t as never)).toBe(false);
    expect(sdk.init).not.toHaveBeenCalled();
    expect(Object.keys(t.handlers)).toHaveLength(0);
  });

  it('inits with release and environment, default development', () => {
    const sdk = mockSdk();
    initErrorReporting({ VITE_SENTRY_DSN: 'https://k@o.ingest.sentry.io/1', VITE_RELEASE: 'sha1' }, sdk, target() as never);
    expect(sdk.init).toHaveBeenCalledWith(expect.objectContaining({ release: 'sha1', environment: 'development' }));
    initErrorReporting({ VITE_SENTRY_DSN: 'x', VITE_SENTRY_ENVIRONMENT: 'staging' }, sdk, target() as never);
    expect(sdk.init).toHaveBeenLastCalledWith(expect.objectContaining({ environment: 'staging' }));
  });

  it('captures uncaught errors and unhandled rejections', () => {
    const sdk = mockSdk();
    const t = target();
    initErrorReporting({ VITE_SENTRY_DSN: 'x' }, sdk, t as never);
    const err = new Error('boom');
    t.handlers.error({ error: err });
    const reason = new Error('rejected');
    t.handlers.unhandledrejection({ reason });
    expect(sdk.captureException).toHaveBeenNthCalledWith(1, err);
    expect(sdk.captureException).toHaveBeenNthCalledWith(2, reason);
  });
});

describe('scrubber', () => {
  it('removes guest token and chat text from events and breadcrumbs', () => {
    const token = 'secret-guest-token';
    const event = scrubEvent(
      {
        message: `failed with ${token}`,
        request: { url: `https://x/?t=${token}` },
        extra: { chatText: 'hello world', ok: 1 },
        breadcrumbs: [{ category: 'console', message: 'hi', data: { text: 'yo', n: 2 } }],
      },
      token,
    );
    const json = JSON.stringify(event);
    expect(json).not.toContain(token);
    expect(json).not.toContain('hello world');
    expect(json).not.toContain('"yo"');
    expect(event.extra.ok).toBe(1);
    const crumb = scrubBreadcrumb({ message: `tok ${token}`, data: { text: 'secret chat' } }, token);
    expect(JSON.stringify(crumb)).not.toContain('secret chat');
    expect(JSON.stringify(crumb)).not.toContain(token);
    expect(crumb.message).toBe(`tok ${REDACTED}`);
  });
});
