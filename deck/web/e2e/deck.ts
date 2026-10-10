import { spawn, spawnSync } from 'node:child_process';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:net';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { randomUUID } from 'node:crypto';
import { test as base, expect, request, type APIRequestContext } from '@playwright/test';

export interface Deck {
  url: string;
  call<T>(method: string, path: string, data?: unknown, status?: number): Promise<T>;
}

async function unusedPort(): Promise<number> {
  const listener = createServer();
  await new Promise<void>((done, reject) => {
    listener.once('error', reject);
    listener.listen(0, '127.0.0.1', done);
  });
  const address = listener.address();
  if (!address || typeof address === 'string') throw new Error('No loopback address');
  await new Promise<void>((done, reject) => listener.close(error => error ? reject(error) : done()));
  return address.port;
}

export const test = base.extend<{}, { deck: Deck }>({
  deck: [async ({}, use) => {
    const directory = await mkdtemp(join(tmpdir(), 'deck-browser-'));
    const binary = join(directory, 'deck');
    const result = spawnSync('go', ['build', '-o', binary, './cmd/deck'], {
      cwd: resolve(fileURLToPath(new URL('../..', import.meta.url))),
      encoding: 'utf8', timeout: 180_000
    });
    if (result.status !== 0) {
      await rm(directory, { recursive: true, force: true });
      throw new Error(`Build Deck: ${result.error ?? result.stderr}`);
    }
    const port = await unusedPort();
    const url = `http://127.0.0.1:${port}`;
    const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.startsWith('DECK_') && !key.startsWith('APPDECK_')));
    const server = spawn(binary, [], {
      env: { ...env, DECK_ADDR: `127.0.0.1:${port}`, DECK_DATA_DIR: join(directory, 'data'), DECK_DEMO_AGENT: 'false' },
      stdio: ['ignore', 'pipe', 'pipe']
    });
    let log = '';
    server.stdout.on('data', chunk => log += chunk.toString());
    server.stderr.on('data', chunk => log += chunk.toString());
    let client: APIRequestContext | undefined;
    try {
      client = await request.newContext({ baseURL: url });
      const deadline = Date.now() + 10_000;
      while (true) {
        try {
          const response = await client.get('/healthz', { timeout: 500 });
          if (response.ok()) break;
        } catch { /* Wait for the isolated loopback server. */ }
        if (server.exitCode !== null || Date.now() > deadline) throw new Error(`Deck did not start: ${log}`);
        await new Promise(done => setTimeout(done, 50));
      }
      await use({
        url,
        async call<T>(method: string, path: string, data?: unknown, status = 200): Promise<T> {
          const response = await client!.fetch(`/api/v1${path}`, {
            method, data, headers: { 'Idempotency-Key': `browser-${randomUUID()}` }
          });
          expect(response.status(), `${method} ${path}: ${await response.text()}`).toBe(status);
          return status === 204 ? undefined as T : response.json();
        }
      });
    } finally {
      await client?.dispose();
      server.kill('SIGTERM');
      if (server.exitCode === null) {
        await new Promise<void>(done => {
          const timeout = setTimeout(() => server.kill('SIGKILL'), 10_000);
          server.once('exit', () => { clearTimeout(timeout); done(); });
        });
      }
      await rm(directory, { recursive: true, force: true });
    }
  }, { scope: 'worker', timeout: 180_000 }]
});

export { expect };
