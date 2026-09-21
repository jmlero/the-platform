import { afterEach, describe, expect, it, vi } from 'vitest';
import { deadlineFromInput, deadlineForInput } from './format';

describe('task deadline editing', () => {
  afterEach(() => vi.unstubAllEnvs());

  it.each([
    ['Europe/Vienna', '2026-09-21T18:00:00+02:00', '2026-09-21T18:00'],
    ['Europe/Vienna', '2026-01-21T18:00:00+01:00', '2026-01-21T18:00'],
    ['America/New_York', '2026-09-21T18:00:00-04:00', '2026-09-21T18:00'],
    ['Asia/Kolkata', '2026-09-21T18:00:00+05:30', '2026-09-21T18:00']
  ])('preserves a deadline in %s when saving another field', (zone, timestamp, field) => {
    vi.stubEnv('TZ', zone);
    const original = Date.parse(timestamp);
    expect(deadlineForInput(original)).toBe(field);
    expect(deadlineFromInput(field, original)).toBe(original);
  });

  it('preserves the original instant during the repeated daylight-saving hour', () => {
    vi.stubEnv('TZ', 'Europe/Vienna');
    const original = Date.parse('2026-10-25T02:30:15.123+01:00');
    expect(deadlineFromInput(deadlineForInput(original), original)).toBe(original);
  });

  it('interprets an edited deadline locally and allows clearing it', () => {
    vi.stubEnv('TZ', 'Europe/Vienna');
    const original = Date.parse('2026-09-21T18:00:00+02:00');
    expect(deadlineFromInput('2026-09-21T19:00', original)).toBe(Date.parse('2026-09-21T19:00:00+02:00'));
    expect(deadlineFromInput('', original)).toBeUndefined();
    expect(deadlineForInput()).toBe('');
  });
});
