import { api } from './api';
import type { ChannelMessage } from './types';

// Follow the public cursor contract so refresh and reconnect never truncate history.
export async function loadChannelHistory(channelID: string, fetchPage = api.channelMessages): Promise<ChannelMessage[]> {
  const messages = new Map<string, ChannelMessage>();
  const cursors = new Set<string>();
  let cursor = '';
  do {
    if (cursors.has(cursor)) throw new Error('Channel history cursor repeated. Please retry.');
    cursors.add(cursor);
    const page = await fetchPage(channelID, cursor);
    for (const message of page.items) messages.set(message.id, message);
    cursor = page.next_cursor ?? '';
  } while (cursor);
  return [...messages.values()];
}

export function createChannelHistory(
  onMessages: (messages: ChannelMessage[]) => void,
  onError: (message: string) => void,
  onLoading: (loading: boolean) => void,
  fetchPage = api.channelMessages
) {
  let sequence = 0;
  let closed = false;
  return {
    async refresh(channelID: string) {
      if (closed) return;
      const request = ++sequence;
      onLoading(true);
      try {
        const messages = await loadChannelHistory(channelID, fetchPage);
        if (request === sequence && !closed) {
          onMessages(messages);
          onError('');
        }
      } catch (caught) {
        if (request === sequence && !closed) onError(caught instanceof Error ? caught.message : 'Could not load channel messages.');
      } finally {
        if (request === sequence && !closed) onLoading(false);
      }
    },
    close() { closed = true; sequence++; }
  };
}
