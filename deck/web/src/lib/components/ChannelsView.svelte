<script lang="ts">
  import { ArrowRight, CheckCircle2, MessageSquare, Plus, Send } from '@lucide/svelte';
  import { onDestroy } from 'svelte';
  import { api } from '$lib/api';
  import { createChannelHistory } from '$lib/channel-history';
  import CreateTaskModal from './CreateTaskModal.svelte';
  import { formatDate } from '$lib/format';
  import type { Channel, ChannelMessage, CreateTaskInput, Task } from '$lib/types';

  export let channels: Channel[] = [];
  export let onpromoted: (task: Task) => Promise<void>;

  export let refreshVersion = 0;

  let loading = false;
  let selectedID = '';
  let messages: ChannelMessage[] = [];
  let body = '';
  let channelName = '';
  let busy = false;
  let error = '';
  let promoting: ChannelMessage | null = null;

  const history = createChannelHistory(
    (items) => { messages = items; },
    (message) => { error = message; },
    (state) => { loading = state; }
  );

  $: if (!selectedID && channels.length) {
    void selectChannel(channels[0]);
  }

  $: if (selectedID) { refreshVersion; void refreshMessages(selectedID); }
  onDestroy(() => history.close());

  function selectChannel(channel: Channel) {
    selectedID = channel.id;
    messages = [];
    promoting = null;
    error = '';
  }

  function refreshMessages(channelID: string) {
    return history.refresh(channelID);
  }

  async function createChannel() {
    if (!channelName.trim()) return;
    busy = true;
    error = '';
    try {
      const created = await api.createChannel(channelName.trim());
      channels = [...channels.filter((item) => item.id !== created.id), created];
      channelName = '';
      await selectChannel(created);
    } catch (caught) {
      error = caught instanceof Error ? caught.message : 'Could not create the channel.';
    } finally {
      busy = false;
    }
  }

  async function postMessage() {
    if (!selectedID || !body.trim()) return;
    busy = true;
    error = '';
    try {
      const channelID = selectedID;
      await api.postChannelMessage(channelID, body.trim());
      if (selectedID === channelID) {
        body = '';
        await refreshMessages(channelID);
      }
    } catch (caught) {
      error = caught instanceof Error ? caught.message : 'Could not post the message.';
    } finally {
      busy = false;
    }
  }

  async function promote(input: CreateTaskInput) {
    if (!promoting) return;
    const task = await api.promoteMessage(promoting.id, input);
    promoting = null;
    await refreshMessages(selectedID);
    await onpromoted(task);
  }

  function referenceCount(message: ChannelMessage): number {
    return Object.values(message.references ?? {}).reduce((total, values) => total + (values?.length ?? 0), 0);
  }
</script>

<section class="channels-layout">
  <aside class="channel-list surface">
    <header><span class="eyebrow">Project channels</span><h2>Project conversations</h2></header>
    <nav>
      {#each channels as channel (channel.id)}
        <button class:active={selectedID === channel.id} type="button" on:click={() => selectChannel(channel)}>
          <MessageSquare size={14} /><span>#{channel.name}</span>
        </button>
      {:else}
        <p>No channels yet.</p>
      {/each}
    </nav>
    <form on:submit|preventDefault={createChannel}>
      <input bind:value={channelName} placeholder="new-channel" aria-label="New channel name" />
      <button type="submit" disabled={busy || !channelName.trim()} aria-label="Create channel"><Plus size={14} /></button>
    </form>
  </aside>

  <div class="conversation surface">
    <header>
      <div><span class="eyebrow">Shared context</span><h2>{channels.find((channel) => channel.id === selectedID)?.name ?? 'Select a channel'}</h2></div>
      <p>Discuss ideas here. To authorize work, promote a message into a task with clear criteria and limits.</p>
    </header>
    {#if error}<div class="channel-error" role="alert">{error} <button type="button" on:click={() => refreshMessages(selectedID)}>Retry</button></div>{/if}
    {#if loading}<p class="loading-history" role="status">Loading channel history…</p>{/if}
    <div class="messages">
      {#each messages as message (message.id)}
        <article class:agent={message.author_type === 'agent'}>
          <header><strong>{message.author_id}</strong><time>{formatDate(message.created_at)}</time></header>
          <p>{message.body}</p>
          <footer>
            {#if referenceCount(message)}<span>{referenceCount(message)} canonical references</span>{/if}
            {#if message.promoted_task_id}
              <span class="promoted"><CheckCircle2 size={13} /> Task {message.promoted_task_id.slice(-6)}</span>
            {:else}
              <button type="button" on:click={() => (promoting = message)}>Create task <ArrowRight size={13} /></button>
            {/if}
          </footer>

        </article>
      {:else}
        <div class="empty"><MessageSquare size={25} /><p>No messages in this channel.</p></div>
      {/each}
    </div>
    {#if selectedID}
      <form class="composer" on:submit|preventDefault={postMessage}>
        <textarea aria-label="Message" bind:value={body} rows="2" placeholder="Write a message…"></textarea>
        <button type="submit" disabled={busy || !body.trim()}><Send size={14} /> Send</button>
      </form>
    {/if}
  </div>
</section>

{#if promoting}
  <CreateTaskModal
    initialTitle={promoting.body.length > 80 ? `${promoting.body.slice(0, 77)}…` : promoting.body}
    initialDescription={promoting.body}
    requireCriteria={true}
    onclose={() => (promoting = null)}
    oncreate={promote}
  />
{/if}

<style>
  .loading-history { margin: 8px 16px; color: var(--subtle); font-size: var(--text-sm); }
  .channels-layout { display: grid; min-height: 0; flex: 1; grid-template-columns: 230px minmax(0, 1fr); gap: 20px; min-height: 500px; }
  .channel-list, .conversation { min-height: 0; border-radius: 15px; }
  .channel-list { display: flex; flex-direction: column; padding: 20px; }
  h2 { margin: 4px 0 0; font-size: 0.9rem; }
  .channel-list nav { display: grid; gap: 5px; margin-top: 16px; }
  .channel-list nav button { display: flex; align-items: center; gap: 8px; padding: 9px; color: var(--muted); background: transparent; border: 0; border-radius: 8px; cursor: pointer; }
  .channel-list nav button.active { color: var(--text); background: var(--accent-soft); }
  .channel-list nav p { color: var(--subtle); font-size: var(--text-sm); }
  .channel-list > form { display: flex; gap: 6px; margin-top: 24px; }
  input, textarea { width: 100%; color: var(--text); background: var(--panel-soft); border: 1px solid var(--line); border-radius: 8px; }
  input { height: 36px; padding: 0 9px; } textarea { padding: 9px; resize: vertical; }
  .channel-list form button, .composer button { display: inline-flex; align-items: center; justify-content: center; gap: 6px; color: var(--on-accent); background: var(--accent); border: 0; border-radius: 8px; cursor: pointer; }
  .channel-list form button { width: 36px; }
  .conversation { display: flex; flex-direction: column; overflow: hidden; }
  .conversation > header { padding: 15px 17px; border-bottom: 1px solid var(--line); }
  .conversation > header p { max-width: 620px; margin: 6px 0 0; color: var(--subtle); font-size: var(--text-xs); line-height: 1.45; }
  .messages { display: flex; min-height: 220px; flex: 1; flex-direction: column; gap: 9px; padding: 16px; overflow-y: auto; }
  .messages article { align-self: flex-start; width: min(680px, 92%); padding: 11px; background: var(--panel-soft); border: 1px solid var(--line); border-radius: 11px; }
  .messages article.agent { align-self: flex-end; border-color: color-mix(in srgb, var(--info) 16%, transparent); }
  .messages article > header, .messages article > footer { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
  .messages strong { font-size: var(--text-xs); } .messages time, .messages footer span { color: var(--subtle); font-size: var(--text-xs); }
  .messages article > p { overflow-wrap: anywhere; margin: 8px 0; color: var(--muted); font-size: var(--text-sm); line-height: 1.5; white-space: pre-wrap; }
  .messages footer button { min-height: 32px; display: inline-flex; align-items: center; gap: 5px; color: var(--accent); font-size: var(--text-xs); background: transparent; border: 0; cursor: pointer; }
  .promoted { display: inline-flex; align-items: center; gap: 5px; color: var(--accent) !important; }
  .composer { display: grid; grid-template-columns: 1fr 90px; gap: 8px; padding: 12px; border-top: 1px solid var(--line); }
  .channel-error { margin: 10px 16px 0; padding: 8px; color: var(--danger); font-size: var(--text-xs); background: var(--danger-soft); border-radius: 8px; }
  .empty { display: grid; flex: 1; color: var(--subtle); place-content: center; justify-items: center; font-size: var(--text-sm); }
  @media (max-width: 760px) { .channels-layout { grid-template-columns: 1fr; } .channel-list { min-height: 180px; } }
</style>
