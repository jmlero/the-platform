import { afterEach, describe, expect, it, vi } from 'vitest';
import { eventLabel, initials, statusLabels, timeAgo } from './format';

describe('format helpers', () => {
  afterEach(() => vi.useRealTimers());

  it('uses operator-friendly status labels', () => {
    expect(statusLabels.waiting_decision).toBe('Decision');
    expect(statusLabels.verifying).toBe('Verifying');
  });

  it('formats recent activity relative to now', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-07-16T12:00:00Z'));
    expect(timeAgo(Date.parse('2026-07-16T11:58:00Z'))).toBe('2m ago');
  });

  it('formats identifiers for compact UI surfaces', () => {
    expect(initials('Atlas Demo')).toBe('AD');
    expect(eventLabel('decision.requested')).toBe('Decision · Requested');
  });
});
