import { afterEach, describe, expect, it, vi } from 'vitest';

import { api } from './api';

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('operator API requests', () => {
  it('adds the browser CSRF token to human mutations', async () => {
    vi.stubGlobal('document', { cookie: 'appdeck_operator_csrf=csrf-token-123' });
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ id: 'tsk_test' }), {
        status: 201,
        headers: { 'Content-Type': 'application/json' }
      })
    );
    vi.stubGlobal('fetch', fetchMock);

    await api.createTask({
      title: 'Protected task',
      description: '',
      acceptance_criteria: '',
      status: 'backlog',
      priority: 'medium',
      budget_minutes: 30
    });

    expect(fetchMock).toHaveBeenCalledOnce();
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/tasks');
    expect(fetchMock.mock.calls[0][1]).toMatchObject({
      method: 'POST',
      credentials: 'same-origin',
      headers: expect.objectContaining({ 'X-CSRF-Token': 'csrf-token-123' })
    });
  });

  it('does not attach a CSRF token to session reads', async () => {
    vi.stubGlobal('document', { cookie: 'appdeck_operator_csrf=csrf-token-123' });
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ authenticated: true, operator_id: 'operator-primary' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' }
      })
    );
    vi.stubGlobal('fetch', fetchMock);

    await api.session();

    expect(fetchMock.mock.calls[0][1].headers).not.toHaveProperty('X-CSRF-Token');
  });
});
