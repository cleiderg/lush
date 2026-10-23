/** Byte transport between client and server. WebSocket now; WebTransport later behind the same shape. */
export interface Transport {
  send(bytes: Uint8Array): void;
  close(): void;
  onMessage: ((bytes: Uint8Array) => void) | null;
  onOpen: (() => void) | null;
  onClose: (() => void) | null;
}

export type TransportFactory = () => Transport;

export function serverUrl(): string {
  const env = (import.meta as unknown as { env?: Record<string, string | undefined> }).env;
  const url = env?.VITE_SERVER_URL;
  if (url) return url;
  return 'ws://localhost:8080/ws';
}

export class WebSocketTransport implements Transport {
  onMessage: ((bytes: Uint8Array) => void) | null = null;
  onOpen: (() => void) | null = null;
  onClose: (() => void) | null = null;
  private readonly ws: WebSocket;

  constructor(url: string = serverUrl()) {
    this.ws = new WebSocket(url);
    this.ws.binaryType = 'arraybuffer';
    this.ws.onopen = () => this.onOpen?.();
    this.ws.onclose = () => this.onClose?.();
    this.ws.onmessage = (e) => {
      if (e.data instanceof ArrayBuffer) this.onMessage?.(new Uint8Array(e.data));
    };
  }

  send(bytes: Uint8Array): void {
    if (this.ws.readyState === WebSocket.OPEN) this.ws.send(bytes);
  }

  close(): void {
    this.ws.onclose = null;
    this.ws.close();
  }
}
