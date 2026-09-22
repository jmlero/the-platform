import assert from 'node:assert/strict';
import test from 'node:test';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

// Loaded from the checked-out candidate, never from the image's baseline copy.
const { createReport, parseReport } = await import(
  pathToFileURL(resolve('shared/report.ts')).href
);
const detail = () => ({
  session: { id: 'PRIVATE_SESSION', project: 'PRIVATE_PROJECT', cwd: '/private/SECRET',
    fileName: 'PRIVATE_FILE', startedAt: 'PRIVATE_TIME', updatedAt: 'PRIVATE_TIME',
    version: null, source: 'PRIVATE_SOURCE', archived: false },
  revision: 'PRIVATE_REVISION',
  points: [{ source: 'request', id: 'PRIVATE_RESPONSE', turnId: 'PRIVATE_TURN' }],
  events: [{ label: 'PRIVATE_COMMAND', detail: 'PRIVATE_OUTPUT' }],
  warnings: ['PRIVATE_WARNING'],
  summary: { startInput: 100, latestInput: 200, peakInput: 300,
    totalInput: 600, totalOutput: 75, totalCached: 150, cacheRate: .25,
    compactions: 2, model: 'PRIVATE_MODEL', contextWindow: 1000, elapsedMs: 4000 },
});
const expected = () => ({
  schema_version: 1,
  metrics: { start_input_tokens: 100, latest_input_tokens: 200, peak_input_tokens: 300,
    total_input_tokens: 600, total_output_tokens: 75, total_cached_input_tokens: 150,
    compaction_count: 2, elapsed_ms: 4000, context_window_tokens: 1000 },
  limitations: ['not-billing', 'sessions-may-share-history', 'usage-may-be-incomplete'],
});
const parse = (value: unknown) => parseReport(JSON.stringify(value));

test('exports only versioned aggregate metrics and fixed limitation codes', () => {
  const input = detail();
  const before = structuredClone(input);
  assert.deepEqual(createReport(input), expected());
  assert.deepEqual(input, before);
  assert(!JSON.stringify(createReport(input)).includes('PRIVATE'));
});

test('valid reports round-trip without recomputing or double-counting usage', () => {
  const output = createReport(detail());
  assert.deepEqual(parse(output), output);
  assert.equal(output.metrics.total_input_tokens, 600);
  assert.equal(output.metrics.total_cached_input_tokens, 150);
  assert.equal(output.metrics.total_output_tokens, 75);
});

test('zero usage and an unknown context window remain valid', () => {
  const input = detail();
  Object.assign(input.summary, { startInput: 0, latestInput: 0, peakInput: 0,
    totalInput: 0, totalOutput: 0, totalCached: 0, cacheRate: 0,
    compactions: 0, contextWindow: null, elapsedMs: 0 });
  const result = createReport(input);
  assert.equal(result.metrics.context_window_tokens, null);
  assert.equal(result.metrics.total_input_tokens, 0);
  assert.deepEqual(parse(result), result);
});

test('rejects invalid numeric metrics both at creation and parsing', () => {
  const keys = ['startInput', 'latestInput', 'peakInput', 'totalInput', 'totalOutput',
    'totalCached', 'compactions', 'elapsedMs'];
  for (const key of keys) {
    for (const value of [-1, 0.5, NaN, Infinity, Number.MAX_SAFE_INTEGER + 1, '1', null]) {
      const input = detail();
      (input.summary as Record<string, unknown>)[key] = value;
      assert.throws(() => createReport(input), `${key}: ${value}`);
    }
  }
  for (const key of Object.keys(expected().metrics)) {
    for (const value of [-1, 0.5, '1', Number.MAX_SAFE_INTEGER + 1]) {
      const report = expected();
      (report.metrics as Record<string, unknown>)[key] = value;
      assert.throws(() => parse(report), `${key}: ${value}`);
    }
  }
});

test('requires a positive safe integer or null context window', () => {
  for (const value of [0, -1, 1.5, NaN, Infinity, '1000']) {
    const input = detail();
    (input.summary as Record<string, unknown>).contextWindow = value;
    assert.throws(() => createReport(input));
    const report = expected();
    (report.metrics as Record<string, unknown>).context_window_tokens = value;
    // JSON serializes NaN/Infinity to null, so test those at creation only.
    if (Number.isFinite(value) || typeof value === 'string') assert.throws(() => parse(report));
  }
});

test('rejects inconsistent aggregates without assuming window size caps input', () => {
  for (const patch of [{ totalCached: 601 }, { latestInput: 301 }, { startInput: 301 }]) {
    const input = detail(); Object.assign(input.summary, patch);
    assert.throws(() => createReport(input));
  }
  for (const patch of [{ total_cached_input_tokens: 601 }, { latest_input_tokens: 301 },
    { start_input_tokens: 301 }]) {
    const report = expected(); Object.assign(report.metrics, patch);
    assert.throws(() => parse(report));
  }
  const input = detail(); input.summary.contextWindow = 100;
  assert.equal(createReport(input).metrics.latest_input_tokens, 200);
});

test('rejects unknown or missing fields and non-object envelopes', () => {
  for (const value of [null, [], 1, 'report', {}, { ...expected(), schema_version: 2 },
    { ...expected(), transcript: 'PRIVATE_TRANSCRIPT' }]) assert.throws(() => parse(value));
  const missing = expected(); delete (missing.metrics as Record<string, unknown>).total_output_tokens;
  assert.throws(() => parse(missing));
  const extra = expected(); (extra.metrics as Record<string, unknown>).prompt = 'PRIVATE_PROMPT';
  assert.throws(() => parse(extra));
  assert.throws(() => parseReport('{"__proto__":{"polluted":true}}'));
});

test('requires the exact fixed limitation codes', () => {
  for (const limitations of [[], ['PRIVATE_WARNING'], ['not-billing'],
    [...expected().limitations, 'not-billing'], null, 'not-billing']) {
    assert.throws(() => parse({ ...expected(), limitations }));
  }
});

test('rejects malformed, oversized and non-string input without disclosing its body', () => {
  for (const input of ['PRIVATE_BROKEN_JSON', ' '.repeat(65537), null, {}]) {
    assert.throws(() => parseReport(input), (error: unknown) => {
      assert(error instanceof Error);
      assert(!error.message.includes('PRIVATE_BROKEN_JSON'));
      return true;
    });
  }
});
