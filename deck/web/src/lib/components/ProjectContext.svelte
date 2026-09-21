<script lang="ts">
  import { onMount } from 'svelte';
 import ProjectRepository from './ProjectRepository.svelte';
  import { api } from '$lib/api';
  import { formatDate } from '$lib/format';
  import type { ContextSource, ContextSourceInput } from '$lib/types';
  export let projectID = 'prj_default';
  let sources: ContextSource[] = [];
  let loading = true;
  let busy = false;
  let error = '';
  let notice = '';
  let editID = '';
  let editRevision = 0;
  let title = '';
  let kind: ContextSourceInput['kind'] = 'instruction';
  let content = '';
  let reference = '';
  let sourceRevision = '';
  let path = '';
  let guidance = false;
  let active = true;
  let expires = '';
  let conflicts: string[] = [];
  $: current = sources.filter((source) => !source.superseded);
  onMount(() => { void load(); });
  async function load() {
    error = '';
    try { sources = await api.contextSources(projectID); }
    catch (caught) { error = caught instanceof Error ? caught.message : 'Could not load project context.'; }
    finally { loading = false; }
  }
  function edit(source?: ContextSource) {
    editID = source?.id ?? ''; editRevision = source?.revision ?? 0;
    title = source?.title ?? ''; kind = source?.kind ?? 'instruction'; content = source?.content ?? '';
    reference = source?.reference ?? ''; sourceRevision = source?.source_revision ?? ''; path = source?.path ?? '';
    guidance = source?.trust === 'guidance'; active = source?.state !== 'inactive';
    conflicts = source?.conflicts_with ?? [];
    expires = source?.expires_at ? new Date(source.expires_at - new Date(source.expires_at).getTimezoneOffset() * 60000).toISOString().slice(0, 16) : '';
    notice = '';
  }
  async function save() {
    busy = true; error = ''; notice = '';
    try {
      await api.putContextSource({id: editID || crypto.randomUUID(), expected_revision: editRevision, title: title.trim(), kind,
        content: ['instruction', 'goal', 'document'].includes(kind) ? content : undefined,
        reference: kind === 'decision' ? reference.trim() : undefined,
        source_revision: ['git', 'decision'].includes(kind) ? sourceRevision.trim() : undefined,
        path: kind === 'git' ? path.trim() : undefined,
        trust: guidance ? 'guidance' : 'data', state: active ? 'active' : 'inactive',
        expires_at: expires ? new Date(expires).getTime() : undefined, conflicts_with: conflicts}, projectID);
      edit(); await load(); notice = 'Saved. Newly authorized tasks will use this revision.';
    } catch (caught) { error = caught instanceof Error ? caught.message : 'Could not save context.'; }
    finally { busy = false; }
  }
  async function revoke(source: ContextSource) {
    busy = true; error = '';
    try { await api.revokeContextSource(source.id, source.revision, projectID); await load(); notice = 'Revision revoked. Runs using it must stop for clarification.'; }
    catch (caught) { error = caught instanceof Error ? caught.message : 'Could not revoke source.'; }
    finally { busy = false; }
  }
</script>

<div class="project-context">
 <ProjectRepository {projectID} />
  {#if error}<p class="error" role="alert">{error} <button type="button" on:click={load}>Reload sources</button></p>{/if}
  {#if notice}<p class="notice" role="status">{notice}</p>{/if}
  <div class="context-grid">
    <section class="surface sources">
      <header><h2>Project sources</h2><p>Active sources are captured when a task moves to Ready. Existing tasks keep their selected revisions.</p></header>
      {#if loading}<p role="status">Loading sources…</p>{/if}
      {#each current as source (source.id)}
        <article>
          <div class="source-heading"><h3>{source.title}</h3><span class:revoked={source.revoked_at}>{source.revoked_at ? 'Revoked' : source.state === 'active' ? 'Active' : 'Inactive'}</span></div>
          <p class="meta">{source.kind} · Revision {source.revision} · {source.trust === 'guidance' ? 'Approved guidance' : 'Reference data'}</p>
          {#if source.kind === 'git'}<p class="reference">{source.path} at {source.source_revision}</p>{:else if source.kind === 'decision'}<p class="reference">Decision {source.reference}</p>{:else}<p class="preview">{source.content}</p>{/if}
          {#if source.expires_at}<p class="meta">Expires {formatDate(source.expires_at)}</p>{/if}
          <div class="actions"><button type="button" disabled={busy} on:click={() => edit(source)}>Edit as new revision</button>{#if !source.revoked_at}<button type="button" disabled={busy} on:click={() => revoke(source)}>Revoke revision</button>{/if}</div>
          <details><summary>Revision history</summary>
            {#each sources.filter((item) => item.id === source.id) as revision}
              <div class="revision"><p>Revision {revision.revision} · {formatDate(revision.created_at)} · {revision.revoked_at ? 'Revoked' : revision.superseded ? 'Superseded' : revision.state}</p><code>{revision.digest}</code>{#if revision.superseded && !revision.revoked_at}<button type="button" disabled={busy} on:click={() => revoke(revision)}>Revoke revision {revision.revision}</button>{/if}</div>
            {/each}
          </details>
        </article>
      {:else}
        {#if !loading}<p class="empty">Add instructions, goals, documents, or decision references to reuse them across tasks.</p>{/if}
      {/each}
    </section>
    <section class="surface editor">
      <h2>{editID ? 'Revise source' : 'Add project knowledge'}</h2>
      <form on:submit|preventDefault={save}>
        <label>Title<input required maxlength="200" bind:value={title} placeholder="For example, preserve the public API" /></label>
        <label>Source type<select bind:value={kind}><option value="instruction">Custom instructions</option><option value="goal">Project goal</option><option value="document">Project document</option><option value="git">Document in Git</option><option value="decision">Approved Deck decision</option></select></label>
        {#if ['instruction', 'goal', 'document'].includes(kind)}
          <label>Content<textarea required rows="8" maxlength="16384" bind:value={content}></textarea></label>
        {:else if kind === 'git'}
          <p class="meta">The document stays in the project's repository. Select an exact commit so its content cannot change during execution.</p>
          <label>File path<input required bind:value={path} placeholder="docs/decisions/api.md" /></label>
          <label>Full commit SHA<input required bind:value={sourceRevision} pattern={'[a-f0-9]{40}([a-f0-9]{24})?'} /></label>
        {:else}
          <label>Approved decision ID<input required bind:value={reference} /></label>
          <label>Decision's plan revision ID<input required bind:value={sourceRevision} /></label>
        {/if}
        <label class="check"><input type="checkbox" bind:checked={guidance} /><span>I reviewed this source and approve it as guidance within task permissions.</span></label>
        <label class="check"><input type="checkbox" bind:checked={active} /><span>Include in newly authorized tasks</span></label>
        <details><summary>Expiry and known conflicts</summary>
          <label>Expires (optional)<input type="datetime-local" bind:value={expires} /></label>
          <p class="meta">Select any source that conflicts with this one. Tasks using both will stop for clarification.</p>
          {#each current.filter((item) => item.id !== editID && !item.revoked_at) as other}<label class="check"><input type="checkbox" bind:group={conflicts} value={other.id} />{other.title}</label>{/each}
        </details>
        <div class="actions"><button class="primary" type="submit" disabled={busy || !title.trim()}>{busy ? 'Saving…' : 'Save source'}</button>{#if editID}<button type="button" disabled={busy} on:click={() => edit()}>Cancel edit</button>{/if}</div>
      </form>
    </section>
  </div>
</div>

<style>
  .project-context { display: grid; gap: 20px; }
  .context-grid { display: grid; grid-template-columns: minmax(0, 1.15fr) minmax(300px, 1fr); gap: 24px; align-items: start; }
  .sources, .editor { padding: 24px; min-width: 0; }
  h2 { font-size: 18px; margin: 0 0 12px; } h3 { margin: 0; font-size: 15px; }
  p { line-height: 1.6; } header p, .meta, .empty { color: var(--muted); font-size: 13px; }
  article { border-top: 1px solid var(--line); padding: 20px 0; }
  .source-heading { display: flex; justify-content: space-between; gap: 12px; align-items: center; }
  .source-heading span { font-size: 12px; padding: 4px 8px; border-radius: 5px; color: var(--accent); background: var(--accent-soft); }
  .source-heading .revoked { color: var(--danger); }
  .preview { white-space: pre-wrap; max-height: 130px; overflow: auto; font-size: 13px; }
  .reference, code { overflow-wrap: anywhere; font-size: 12px; }
  form { display: grid; gap: 16px; } label { display: grid; gap: 7px; font-size: 13px; font-weight: 500; }
  input, select, textarea { width: 100%; padding: 10px; border: 1px solid var(--line); border-radius: 6px; background: var(--panel); color: var(--text); font: inherit; }
  textarea { resize: vertical; } .check { display: flex; align-items: flex-start; line-height: 1.5; } .check input { width: auto; margin-top: 3px; }
  .actions { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 12px; }
  button { min-height: 38px; padding: 8px 12px; border-radius: 6px; border: 1px solid var(--line); background: var(--panel); color: var(--text); cursor: pointer; }
  button.primary { background: var(--accent); color: white; border-color: var(--accent); }
  button:disabled { opacity: .6; cursor: default; } details { margin-top: 14px; font-size: 13px; } summary { cursor: pointer; padding: 8px 0; }
  .revision { padding: 12px 0; border-top: 1px solid var(--line); } .revision button { margin-top: 8px; }
  .error { color: var(--danger); } .notice { color: var(--success); }
  @media(max-width: 1000px) { .context-grid { grid-template-columns: minmax(0, 1fr); } }
</style>
