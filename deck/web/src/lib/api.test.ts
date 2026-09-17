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

// Exercise the browser transport contract without requiring a native browser.
describe('live channel notifications', () => {
  it('delivers message events and every reconnect so an open history can refresh', async () => {
    const { subscribeToEvents } = await import('./api');
    let source: FakeSource;
    class FakeSource {
      onopen?: () => void;
      onerror?: () => void;
      onmessage?: (message: { data: string }) => void;
      close = vi.fn();
      constructor(public url: string) { source = this; }
    }
    vi.stubGlobal('EventSource', FakeSource);
    const events = vi.fn();
    const connections = vi.fn();
    const stop = subscribeToEvents(events, connections);
    expect(source!.url).toBe('/api/v1/operator/events/stream');
    source!.onopen!();
    const message = { id: 42, type: 'channel.message.created', payload: { channel_id: 'channel', message_id: 'message' } };
    source!.onmessage!({ data: JSON.stringify(message) });
    source!.onmessage!({ data: 'broken delivery' });
    source!.onerror!();
    source!.onopen!();
    source!.onmessage!({ data: JSON.stringify(message) });
    expect(connections.mock.calls).toEqual([[true], [false], [true]]);
    expect(events.mock.calls).toEqual([[message], [message]]);
    stop();
    expect(source!.close).toHaveBeenCalledOnce();
  });

  it('encodes the opaque channel cursor without interpreting it', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ items: [], next_cursor: 'next' })));
    vi.stubGlobal('fetch', fetchMock);
    expect(await api.channelMessages('channel', 'cursor+/=')).toEqual({ items: [], next_cursor: 'next' });
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/operator/channels/channel/messages?cursor=cursor%2B%2F%3D');
  });
});
