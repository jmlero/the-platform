<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '$lib/api';
  import type { RepositoryRequest } from '$lib/types';
  export let projectID: string;
  let current: RepositoryRequest | null = null;
  let repository = '';
  let baseBranch = 'main';
  let busy = false;
  let error = '';
  let loaded = false;
  async function load() {
    try {
      current = await api.repository(projectID);
      if (!loaded) { repository = current.repository; baseBranch = current.base_branch || 'main'; }
      loaded = true;
    } catch (caught) { error = caught instanceof Error ? caught.message : 'Could not load repository.'; }
  }
  onMount(() => { void load(); const timer = setInterval(() => { if (current?.status === 'pending') void load(); }, 5000); return () => clearInterval(timer); });
  async function save() {
    if (!current) return;
    busy = true; error = '';
    try { current = await api.requestRepository({expected_revision: current.revision, expected_binding_version: current.binding?.version ?? 0, repository: repository.trim(), base_branch: baseBranch.trim()}, projectID); }
    catch (caught) { error = caught instanceof Error ? caught.message : 'Could not request association.'; }
    finally { busy = false; }
  }
</script>
<section class="surface repository">
  <div><h2>Project repository</h2><p>Associate a GitHub repository using this installation's existing connection. Later changes apply to newly authorized tasks.</p>
    {#if current?.binding}<p class="binding"><strong>{current.binding.repository}</strong> · {current.binding.base_branch} · Version {current.binding.version}</p>{/if}
    {#if current?.status === 'pending'}<p role="status">Marina is checking repository access and the base branch. The current association stays in use until confirmed.</p>{/if}
    {#if current?.status === 'rejected'}<p class="error" role="alert">{current.reason}</p>{/if}
    {#if current?.status === 'connected'}<p role="status">Repository connected. Existing tasks retain their original binding.</p>{/if}
    {#if error}<p class="error" role="alert">{error} <button type="button" on:click={load}>Refresh</button></p>{/if}
  </div>
  <form on:submit|preventDefault={save}>
    <label>Repository<input required pattern={'[A-Za-z0-9_\\-][A-Za-z0-9_.\\-]{0,99}/[A-Za-z0-9_\\-][A-Za-z0-9_.\\-]{0,99}'} bind:value={repository} placeholder="owner/repository" /></label>
    <label>Base branch<input required maxlength="200" bind:value={baseBranch} /></label>
    <button class="primary" type="submit" disabled={busy || !loaded || current?.status === 'pending'}>{busy ? 'Submitting…' : 'Associate repository'}</button>
  </form>
</section>
<style>
  .repository { padding: 24px; display: grid; grid-template-columns: minmax(0,1.15fr) minmax(300px,1fr); gap:24px; }
  h2 { font-size:18px; margin:0 0 12px; } p { font-size:13px; color:var(--muted); line-height:1.6; } .binding { color:var(--text); overflow-wrap:anywhere; }
  form { display:grid; gap:12px; } label { display:grid; gap:7px; font-size:13px; font-weight:500; }
  input { width:100%; padding:10px; border:1px solid var(--line); border-radius:6px; background:var(--panel); color:var(--text); font:inherit; }
  button { min-height:38px; padding:8px 12px; border-radius:6px; border:1px solid var(--line); background:var(--panel); color:var(--text); cursor:pointer; }
  .primary { background:var(--accent); color:white; border-color:var(--accent); justify-self:start; } button:disabled { opacity:.6; cursor:default; } .error { color:var(--danger); }
  @media(max-width:1000px) { .repository { grid-template-columns:minmax(0,1fr); } }
</style>
