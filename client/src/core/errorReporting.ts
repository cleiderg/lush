/** Sentry error reporting. Runs only when a DSN is provided at build time. */
import * as Sentry from '@sentry/browser';

export interface ReportingEnv {
  VITE_SENTRY_DSN?: string;
  VITE_SENTRY_ENVIRONMENT?: string;
  VITE_RELEASE?: string;
}

/** The slice of the Sentry SDK we use; tests pass a mock. */
export interface SentrySdk {
  init(options: Record<string, unknown>): void;
  captureException(error: unknown): void;
}

export const REDACTED = '[redacted]';
const TOKEN_KEY = 'lush.guestToken';

type Obj = Record<string, unknown>;

function isObj(v: unknown): v is Obj {
  return typeof v === 'object' && v !== null;
}

/** Keys whose values are chat text or credentials. */
function sensitiveKey(key: string): boolean {
  return /token|chat|message|text/i.test(key);
}

function scrubString(s: string, token: string): string {
  return token ? s.split(token).join(REDACTED) : s;
}

function scrubValue(v: unknown, token: string, depth = 0): unknown {
  if (typeof v === 'string') return scrubString(v, token);
  if (depth > 8 || !isObj(v)) return v;
  if (Array.isArray(v)) return v.map((x) => scrubValue(x, token, depth + 1));
  const out: Obj = {};
  for (const [k, val] of Object.entries(v)) {
    out[k] = sensitiveKey(k) && val != null && typeof val !== 'boolean' ? REDACTED : scrubValue(val, token, depth + 1);
  }
  return out;
}

/** Removes the guest token and chat text from an event (including breadcrumbs). */
export function scrubEvent<T extends object>(event: T, token = readToken()): T {
  return scrubValue(event, token) as T;
}

export function scrubBreadcrumb<T extends object>(crumb: T, token = readToken()): T {
  return scrubValue(crumb, token) as T;
}

function readToken(): string {
  try {
    return localStorage.getItem(TOKEN_KEY) ?? '';
  } catch {
    return '';
  }
}

function viteEnv(): ReportingEnv {
  return ((import.meta as unknown as { env?: ReportingEnv }).env ?? {});
}

/** Initialises Sentry and installs global handlers. Returns false (and does nothing) when no DSN is set. */
export function initErrorReporting(
  env: ReportingEnv = viteEnv(),
  sdk: SentrySdk = Sentry as unknown as SentrySdk,
  target: Pick<Window, 'addEventListener'> = window,
): boolean {
  if (!env.VITE_SENTRY_DSN) return false;
  sdk.init({
    dsn: env.VITE_SENTRY_DSN,
    release: env.VITE_RELEASE,
    environment: env.VITE_SENTRY_ENVIRONMENT || 'development',
    sendDefaultPii: false,
    // We install the global handlers ourselves below.
    integrations: (defaults: { name: string }[]) => defaults.filter((i) => i.name !== 'GlobalHandlers'),
    beforeSend: (event: object) => scrubEvent(event),
    beforeBreadcrumb: (crumb: object) => scrubBreadcrumb(crumb),
  });
  target.addEventListener('error', (e) => sdk.captureException((e as ErrorEvent).error ?? (e as ErrorEvent).message));
  target.addEventListener('unhandledrejection', (e) => sdk.captureException((e as PromiseRejectionEvent).reason));
  return true;
}
