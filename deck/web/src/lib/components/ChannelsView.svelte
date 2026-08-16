<script lang="ts">
  import { ArrowRight, CheckCircle2, MessageSquare, Plus, Send } from '@lucide/svelte';
  import { api } from '$lib/api';
  import { formatDate } from '$lib/format';
  import type { Channel, ChannelMessage, CreateTaskInput, Task } from '$lib/types';

  export let channels: Channel[] = [];
  export let onpromoted: (task: Task) => Promise<void>;

  let selectedID = '';
  let messages: ChannelMessage[] = [];
  let body = '';
  let channelName = '';
  let busy = false;
  let error = '';
  let promoting = '';
  let promotionTitle = '';
  let acceptanceCriteria = '';
  let timeMinutes = 30;
  let authority = 'source.read';

  $: if (!selectedID && channels.length) {
    void selectChannel(channels[0]);
  }

  async function selectChannel(channel: Channel) {
    selectedID = channel.id;
    error = '';
    try {
      messages = (await api.channelMessages(channel.id)).items;
    } catch (caught) {
      error = caught instanceof Error ? caught.message : 'Could not load channel messages.';
    }
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
      const message = await api.postChannelMessage(selectedID, body.trim());
      messages = [...messages, message];
      body = '';
    } catch (caught) {
      error = caught instanceof Error ? caught.message : 'Could not post the message.';
    } finally {
      busy = false;
    }
  }

  function startPromotion(message: ChannelMessage) {
    promoting = message.id;
    promotionTitle = message.body.length > 80 ? `${message.body.slice(0, 77)}…` : message.body;
    acceptanceCriteria = '';
  }

  async function promote(message: ChannelMessage) {
    if (!promotionTitle.trim() || !acceptanceCriteria.trim()) {
      error = 'A promoted task needs a clear outcome and observable acceptance criteria.';
      return;
    }
    busy = true;
    error = '';
    const input: CreateTaskInput = {
      title: promotionTitle.trim(),
      description: '',
      acceptance_criteria: acceptanceCriteria.trim(),
      status: 'backlog',
      priority: 'medium',
      budget_minutes: Number(timeMinutes),
      budget: {
        time_minutes: Number(timeMinutes), max_attempts: 1, max_tokens: 0,
        max_cost_minor: 0, currency: 'USD', max_concurrency: 1,
        authority: authority.split(',').map((item) => item.trim()).filter(Boolean)
      }
    };
    try {
      const task = await api.promoteMessage(message.id, input);
      messages = messages.map((item) => item.id === message.id ? { ...item, promoted_task_id: task.id } : item);
      promoting = '';
      await onpromoted(task);
    } catch (caught) {
      error = caught instanceof Error ? caught.message : 'Could not promote the message.';
    } finally {
      busy = false;
    }
  }

  function referenceCount(message: ChannelMessage): number {
    return Object.values(message.references ?? {}).reduce((total, values) => total + (values?.length ?? 0), 0);
  }
</script>

<section class="channels-layout">
  <aside class="channel-list glass">
    <header><span class="eyebrow">Project channels</span><h2>Agent-native conversation</h2></header>
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

  <div class="conversation glass">
    <header>
      <div><span class="eyebrow">Conversation, not authority</span><h2>{channels.find((channel) => channel.id === selectedID)?.name ?? 'Select a channel'}</h2></div>
      <p>Messages may reference canonical records. Execution starts only after promotion into a bounded, versioned task.</p>
    </header>
    {#if error}<div class="channel-error">{error}</div>{/if}
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
              <button type="button" on:click={() => startPromotion(message)}>Promote to bounded task <ArrowRight size={13} /></button>
            {/if}
          </footer>
          {#if promoting === message.id}
            <div class="promotion-form">
              <label>Outcome<input bind:value={promotionTitle} /></label>
              <label>Acceptance criteria<textarea bind:value={acceptanceCriteria} rows="2"></textarea></label>
              <div><label>Minutes<input type="number" min="1" bind:value={timeMinutes} /></label><label>Authority<input bind:value={authority} /></label></div>
              <button type="button" disabled={busy} on:click={() => promote(message)}>Create versioned task</button>
            </div>
          {/if}
        </article>
      {:else}
        <div class="empty"><MessageSquare size={25} /><p>No messages in this channel.</p></div>
      {/each}
    </div>
    {#if selectedID}
      <form class="composer" on:submit|preventDefault={postMessage}>
        <textarea bind:value={body} rows="2" placeholder="Write a project-scoped message…"></textarea>
        <button type="submit" disabled={busy || !body.trim()}><Send size={14} /> Send</button>
      </form>
    {/if}
  </div>
</section>

<style>
  .channels-layout { display: grid; min-height: 0; flex: 1; grid-template-columns: 240px 1fr; gap: 12px; }
  .channel-list, .conversation { min-height: 0; border-radius: 15px; }
  .channel-list { display: flex; flex-direction: column; padding: 15px; }
  h2 { margin: 4px 0 0; font-size: 0.9rem; }
  .channel-list nav { display: grid; gap: 5px; margin-top: 16px; }
  .channel-list nav button { display: flex; align-items: center; gap: 8px; padding: 9px; color: var(--muted); background: transparent; border: 0; border-radius: 8px; cursor: pointer; }
  .channel-list nav button.active { color: var(--text); background: rgba(184, 243, 107, 0.08); }
  .channel-list nav p { color: var(--subtle); font-size: 0.66rem; }
  .channel-list > form { display: flex; gap: 6px; margin-top: auto; }
  input, textarea { width: 100%; color: var(--text); background: rgba(255,255,255,.035); border: 1px solid var(--line); border-radius: 8px; }
  input { height: 36px; padding: 0 9px; } textarea { padding: 9px; resize: vertical; }
  .channel-list form button, .composer button { display: inline-flex; align-items: center; justify-content: center; gap: 6px; color: #10140c; background: var(--acid); border: 0; border-radius: 8px; cursor: pointer; }
  .channel-list form button { width: 36px; }
  .conversation { display: flex; flex-direction: column; overflow: hidden; }
  .conversation > header { padding: 15px 17px; border-bottom: 1px solid var(--line); }
  .conversation > header p { max-width: 620px; margin: 6px 0 0; color: var(--subtle); font-size: 0.63rem; line-height: 1.45; }
  .messages { display: flex; min-height: 220px; flex: 1; flex-direction: column; gap: 9px; padding: 16px; overflow-y: auto; }
  .messages article { align-self: flex-start; width: min(680px, 92%); padding: 11px; background: rgba(255,255,255,.025); border: 1px solid var(--line); border-radius: 11px; }
  .messages article.agent { align-self: flex-end; border-color: rgba(108, 216, 232, .16); }
  .messages article > header, .messages article > footer { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
  .messages strong { font-size: 0.64rem; } .messages time, .messages footer span { color: var(--subtle); font-size: 0.56rem; }
  .messages article > p { margin: 8px 0; color: var(--muted); font-size: 0.71rem; line-height: 1.5; white-space: pre-wrap; }
  .messages footer button { display: inline-flex; align-items: center; gap: 5px; color: var(--acid); font-size: 0.6rem; background: transparent; border: 0; cursor: pointer; }
  .promoted { display: inline-flex; align-items: center; gap: 5px; color: var(--acid) !important; }
  .promotion-form { display: grid; gap: 8px; margin-top: 10px; padding: 10px; background: rgba(0,0,0,.16); border-radius: 8px; }
  .promotion-form label { display: grid; gap: 4px; color: var(--subtle); font-size: 0.57rem; }
  .promotion-form > div { display: grid; grid-template-columns: 120px 1fr; gap: 8px; }
  .promotion-form > button { height: 34px; color: #10140c; font-weight: 700; background: var(--acid); border: 0; border-radius: 8px; cursor: pointer; }
  .composer { display: grid; grid-template-columns: 1fr 90px; gap: 8px; padding: 12px; border-top: 1px solid var(--line); }
  .channel-error { margin: 10px 16px 0; padding: 8px; color: var(--red); font-size: .64rem; background: var(--red-soft); border-radius: 8px; }
  .empty { display: grid; flex: 1; color: var(--subtle); place-content: center; justify-items: center; font-size: .68rem; }
  @media (max-width: 760px) { .channels-layout { grid-template-columns: 1fr; } .channel-list { min-height: 180px; } }
</style>
