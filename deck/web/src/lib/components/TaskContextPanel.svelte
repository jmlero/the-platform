<script lang="ts">
  import { api } from '$lib/api';
  import { formatDate } from '$lib/format';
  import type { ContextManifest, ResolvedContext, TaskDetail } from '$lib/types';
  export let detail: TaskDetail;
  let selectedRun = detail.runs[0]?.id ?? '';
  let resolved: ResolvedContext | null = null;
  let error = '';
  let loading = true;
  let requested = '';
  let generation = 0;
  $: key = `${detail.task.id}:${selectedRun}:${detail.task.version}:${detail.events[0]?.id}`;
  $: if (key !== requested) { requested = key; void load(); }
  $: contextEvent = detail.events.find((event) => event.type === 'context.assembled' && event.run_id === selectedRun);
  $: manifest = resolved?.assembly?.context ?? contextEvent?.payload?.context as ContextManifest | undefined;
  $: outcome = resolved?.assembly?.interpreted_outcome ?? contextEvent?.payload?.interpreted_outcome as {Title?: string; Description?: string; AcceptanceCriteria?: string} | undefined;
  async function load() {
    const ticket = ++generation;
    loading = true; error = '';
    try {
      const result = selectedRun ? await api.runContext(selectedRun) : await api.taskContext(detail.task.id);
      if (ticket === generation) resolved = result;
    } catch (caught) { if (ticket === generation) error = caught instanceof Error ? caught.message : 'Could not load selected context.'; }
    finally { if (ticket === generation) loading = false; }
  }
</script>

<section class="context-panel">
  {#if detail.runs.length}
    <label>Execution context<select bind:value={selectedRun}>{#each detail.runs as run}<option value={run.id}>{formatDate(run.started_at)} · {run.status}</option>{/each}<option value="">Current task selection</option></select></label>
  {/if}
  <div class="outcome"><h3>{outcome ? 'Outcome received by the worker' : 'Requested outcome'}</h3><p>{outcome?.Title ?? detail.task.title}</p><h4>Acceptance criteria</h4><p>{outcome?.AcceptanceCriteria ?? detail.task.acceptance_criteria}</p></div>
  {#if error}<p class="error" role="alert">{error} <button type="button" on:click={load}>Retry</button></p>{/if}
  {#if loading}<p role="status">Loading selected context…</p>{/if}
  {#if resolved && !loading}
    {#if resolved.selection?.authorized_at}<p class="meta">Selection authorized {formatDate(resolved.selection.authorized_at)}. Later edits apply to newly authorized tasks.</p>{:else if detail.task.status === 'backlog'}<p class="meta">Project sources will be selected when this draft moves to Ready.</p>{/if}
    {#if resolved.selection?.repository}<h3>Repository selected for this task</h3><p class="reference">{resolved.selection.repository.repository} · {resolved.selection.repository.base_branch} · Binding version {resolved.selection.repository.version}</p>{/if}
    <h3>Selected project knowledge</h3>
    {#each resolved.sources as source}
      <article><div class="source-title"><strong>{source.title}</strong><span>Revision {source.revision}</span></div>
        <p class="meta">{source.kind} · {source.trust === 'guidance' ? 'Approved guidance' : 'Reference data'}{source.revoked_at ? ' · REVOKED' : ''}</p>
        {#if source.kind === 'git'}<p class="reference">{source.path} at {source.source_revision}</p>{:else if source.kind === 'decision'}<p class="reference">Decision {source.reference} · plan {source.source_revision}</p>{/if}
        {#if source.content}<details><summary>Read selected revision</summary><p class="content">{source.content}</p></details>{/if}
        <details><summary>Source identity</summary><code>{source.id} / {source.digest}</code></details>
      </article>
    {:else}<p class="meta">No extra project sources were selected.</p>{/each}
    <h3>Selected conversation</h3>
    {#each resolved.messages as message}<article><strong>{message.author_id}</strong><p class="meta">{formatDate(message.created_at)} · Task data</p><p class="content">{message.content}</p></article>{:else}<p class="meta">No conversation excerpts were selected.</p>{/each}
  {/if}
  {#if manifest}
    <h3>Worker snapshot</h3><p class="meta">Assembled {formatDate(new Date(manifest.created_at).getTime())}. This package survives worker and service restarts.</p>
    {#each manifest.inputs as input}<article><strong>{input.title ?? input.kind}</strong><p class="meta">{input.owner ?? input.kind} · {input.trust === 'trusted' ? 'Guidance within task permissions' : 'Untrusted task data'}</p><p class="reference">{input.uri}</p>{#if input.content}<details><summary>Content received by the worker</summary><p class="content">{input.content}</p></details>{/if}<details><summary>Revision and content digest</summary><code>{input.revision}<br />{input.digest}</code></details></article>{/each}
    {#if manifest.omissions?.length}<h3>Omitted inputs</h3>{#each manifest.omissions as omitted}<article><strong>{omitted.title}</strong><p>{omitted.reason}</p><p class="reference">{omitted.uri}</p></article>{/each}{/if}
    <details><summary>Snapshot identity</summary><code>{manifest.digest}</code><p class="reference">Repository revision: {manifest.source_revision}<br />Policy: {manifest.policy_version}</p></details>
  {:else}<p class="meta">A worker snapshot will appear after Marina assembles the authorized inputs.</p>{/if}
</section>

<style>
  .context-panel { padding: 24px; font-size: 13px; }
  h3 { font-size: 14px; margin: 24px 0 12px; } h4 { margin-bottom: 4px; } p { line-height: 1.6; }
  label { display: grid; gap: 8px; } select { padding: 10px; width: 100%; color: var(--text); background: var(--panel); border: 1px solid var(--line); border-radius: 6px; }
  .outcome { background: var(--panel-soft); border-radius: 8px; padding: 16px; margin-top: 16px; } .outcome h3 { margin-top: 0; }
  article { padding: 14px 0; border-bottom: 1px solid var(--line); } .source-title { display: flex; justify-content: space-between; gap: 12px; }
  .meta { color: var(--muted); } .content { white-space: pre-wrap; overflow-wrap: anywhere; } .reference, code { overflow-wrap: anywhere; font-size: 12px; }
  details { margin-top: 8px; } summary { cursor: pointer; padding: 6px 0; } .error { color: var(--danger); }
</style>
