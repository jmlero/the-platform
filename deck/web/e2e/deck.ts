import { spawn, spawnSync } from 'node:child_process';
import { mkdtemp, rename, rm, writeFile } from 'node:fs/promises';
import { createServer } from 'node:net';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { randomUUID } from 'node:crypto';
import { test as base, expect, request, type APIRequestContext } from '@playwright/test';

export interface Deck {
  url: string;
  call<T>(method: string, path: string, data?: unknown, status?: number): Promise<T>;
  // mapProject binds a Deck project to the sandbox's Marina project.
  mapProject(projectID: string): Promise<void>;
  marina<T>(path: string): Promise<T>;
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

function build(directory: string, cwd: string, output: string, args: string[]) {
  const result = spawnSync('go', ['build', ...args, '-o', join(directory, output), './cmd/' + output], { cwd, encoding: 'utf8', timeout: 180_000 });
  if (result.status !== 0) throw new Error(`Build ${output}: ${result.error ?? result.stderr}`);
  return join(directory, output);
}

function start(binary: string, args: string[], env: NodeJS.ProcessEnv) {
  const server = spawn(binary, args, { env, stdio: ['ignore', 'pipe', 'pipe'] });
  const process = { server, log: '' };
  server.stdout.on('data', chunk => process.log += chunk.toString());
  server.stderr.on('data', chunk => process.log += chunk.toString());
  return process;
}

async function stop(server: ReturnType<typeof spawn>) {
  if (server.exitCode !== null) return;
  server.kill('SIGTERM');
  await new Promise<void>(done => {
    const timeout = setTimeout(() => server.kill('SIGKILL'), 10_000);
    server.once('exit', () => { clearTimeout(timeout); done(); });
  });
}

async function waitReady(client: APIRequestContext, url: string, process: { server: ReturnType<typeof spawn>; log: string }) {
  const deadline = Date.now() + 15_000;
  while (true) {
    try {
      await client.get(url, { timeout: 500 });
      return;
    } catch { /* Wait for the isolated loopback server. */ }
    if (process.server.exitCode !== null || Date.now() > deadline) throw new Error(`Process did not start: ${process.log}`);
    await new Promise(done => setTimeout(done, 50));
  }
}

// Each worker runs a real Deck configured with a real Marina process: the
// Marina API sandbox build, which serves the platform API over an in-memory
// Kubernetes and simulates execution without Git or models.
export const test = base.extend<{}, { deck: Deck }>({
  deck: [async ({}, use) => {
    const directory = await mkdtemp(join(tmpdir(), 'deck-browser-'));
    const deckRoot = resolve(fileURLToPath(new URL('../..', import.meta.url)));
    const marinaRoot = resolve(process.env.MARINA_REPO ?? join(deckRoot, '..', 'marina'));
    let deck: ReturnType<typeof start> | undefined;
    let marina: ReturnType<typeof start> | undefined;
    let client: APIRequestContext | undefined;
    try {
      const deckBinary = build(directory, deckRoot, 'deck', []);
      const marinaBinary = build(directory, marinaRoot, 'marina', ['-tags', 'sandbox']);
      const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.startsWith('DECK_') && !key.startsWith('APPDECK_')));
      const marinaURL = `http://127.0.0.1:${await unusedPort()}`;
      marina = start(marinaBinary, ['api-sandbox', '--addr', marinaURL.slice(7), '--dir', join(directory, 'sandbox')], env);
      const token = join(directory, 'token');
      const projects = join(directory, 'projects.json');
      await writeFile(token, 'user:system:serviceaccount:marina-system:deck');
      await writeFile(projects, '[]');
      const port = await unusedPort();
      const url = `http://127.0.0.1:${port}`;
      client = await request.newContext({ baseURL: url });
      await waitReady(client, `${marinaURL}/apis/marina.dev/v1/capabilities`, marina);
      deck = start(deckBinary, [], { ...env, DECK_ADDR: `127.0.0.1:${port}`, DECK_DATA_DIR: join(directory, 'data'),
        DECK_MARINA_URL: marinaURL, DECK_MARINA_TOKEN_FILE: token, DECK_MARINA_PROJECTS_FILE: projects });
      await waitReady(client, '/healthz', deck);
      const mapped = new Set<string>();
      await use({
        url,
        async call<T>(method: string, path: string, data?: unknown, status = 200): Promise<T> {
          const response = await client!.fetch(`/api/v1${path}`, {
            method, data, headers: { 'Idempotency-Key': `browser-${randomUUID()}` }
          });
          expect(response.status(), `${method} ${path}: ${await response.text()}`).toBe(status);
          return status === 204 ? undefined as T : response.json();
        },
        async mapProject(projectID: string) {
          mapped.add(projectID);
          const mapping = [...mapped].map(id => ({ deck_project: id, namespace: 'factory', project: 'acceptance',
            workloads: { 'repository/v2': 'land-on-main' }, conversation_workload: 'chat' }));
          await writeFile(`${projects}.tmp`, JSON.stringify(mapping));
          await rename(`${projects}.tmp`, projects);
        },
        async marina<T>(path: string): Promise<T> {
          const response = await client!.get(`${marinaURL}/apis/marina.dev/v1/namespaces/factory${path}`, { headers: { Authorization: 'Bearer user:viewer' } });
          expect(response.ok()).toBe(true);
          return response.json();
        }
      });
    } finally {
      await client?.dispose();
      if (deck) await stop(deck.server);
      if (marina) await stop(marina.server);
      await rm(directory, { recursive: true, force: true });
    }
  }, { scope: 'worker', timeout: 240_000 }]
});

export { expect };
