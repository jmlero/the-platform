import { describe, expect, it, vi } from 'vitest';
import { loadChannelHistory } from './channel-history';
import type { ChannelMessage } from './types';

const message = (id: string) => ({ id, body: id } as ChannelMessage);
describe('channel history refresh', () => {
  it('reads every cursor page and replaces duplicate deliveries once', async () => {
    const fetchPage = vi.fn()
      .mockResolvedValueOnce({ items: Array.from({ length: 100 }, (_, i) => message(String(i))), next_cursor: 'page2' })
      .mockResolvedValueOnce({ items: Array.from({ length: 100 }, (_, i) => message(String(i + 100))), next_cursor: 'page3' })
      .mockResolvedValueOnce({ items: [message('199'), message('200')] });
    const items = await loadChannelHistory('channel', fetchPage);
    expect(items.map((item) => item.id)).toEqual(Array.from({ length: 201 }, (_, i) => String(i)));
    expect(fetchPage.mock.calls).toEqual([['channel', ''], ['channel', 'page2'], ['channel', 'page3']]);
  });
  it('reports a failed refresh instead of returning truncated history', async () => {
    const fetchPage = vi.fn().mockResolvedValueOnce({ items: [message('1')], next_cursor: 'more' }).mockRejectedValueOnce(new Error('offline'));
    await expect(loadChannelHistory('channel', fetchPage)).rejects.toThrow('offline');
  });
});

import { createChannelHistory } from './channel-history';

describe('open channel refresh', () => {
  it('recovers missed messages on refresh and displays duplicate delivery only once', async () => {
    const first = message('first');
    const second = message('second-client');
    const fetchPage = vi.fn().mockResolvedValueOnce({ items: [first] })
      .mockResolvedValue({ items: [first, second, second] });
    const displayed = vi.fn();
    const history = createChannelHistory(displayed, vi.fn(), vi.fn(), fetchPage);
    await history.refresh('open-channel');
    await history.refresh('open-channel'); // message notification
    await history.refresh('open-channel'); // SSE reconnect after a missed event
    expect(displayed.mock.calls).toEqual([[[first]], [[first, second]], [[first, second]]]);
  });

  it('discards an old channel response after selection changes or the view closes', async () => {
    let finishOld!: (page: { items: ChannelMessage[] }) => void;
    const fetchPage = vi.fn().mockImplementationOnce(() => new Promise((resolve) => { finishOld = resolve; }))
      .mockResolvedValueOnce({ items: [message('new-channel')] });
    const displayed = vi.fn();
    const history = createChannelHistory(displayed, vi.fn(), vi.fn(), fetchPage);
    const pending = history.refresh('old-channel');
    await history.refresh('new-channel');
    history.close();
    finishOld({ items: [message('stale')] });
    await pending;
    expect(displayed.mock.calls).toEqual([[[message('new-channel')]]]);
  });
});
