import { Call } from '@wailsio/runtime';

// The browser uses the daemon's same-origin API; the desktop build uses the
// Wails service so the daemon's Host and Origin checks remain unchanged.
export async function apiFetch(path: string, options: RequestInit = {}): Promise<Response> {
  if (import.meta.env.VITE_NUDGE_DESKTOP !== '1') return fetch(path, options);

  const headers = new Headers(options.headers);
  const result: unknown = await Call.ByName('nudge/desktop/bridge.Client.Request', options.method ?? 'GET', path, typeof options.body === 'string' ? options.body : '', headers.get('If-Match') ?? '');
  if (typeof result !== 'object' || result === null || !('status' in result) || !('etag' in result) || !('body' in result) || typeof result.status !== 'number' || typeof result.etag !== 'string' || typeof result.body !== 'string') {
    throw new Error('Invalid desktop bridge response');
  }
  const responseHeaders = new Headers({ 'Content-Type': 'application/json' });
  if (result.etag) responseHeaders.set('ETag', result.etag);
  return new Response(result.body, { status: result.status, headers: responseHeaders });
}
