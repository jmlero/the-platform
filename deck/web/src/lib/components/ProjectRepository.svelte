<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '$lib/api';
  import type { RepositoryRequest } from '$lib/types';
  export let projectID: string;
  let current: RepositoryRequest | null = null;
  let error = '';
  async function load() {
    error = '';
    try { current = await api.repository(projectID); }
    catch (caught) { error = caught instanceof Error ? caught.message : 'Could not load repository.'; }
  }
  onMount(() => { void load(); });
</script>
<section class="surface repository">
  <h2>Project repository</h2>
  {#if current?.status === 'connected' && current.binding}
    <p class="binding"><strong>{current.binding.repository}</strong> · {current.binding.base_branch} · Version {current.binding.version}</p>
    <p>Marina's project binds this repository. Changes are made in Marina and apply to newly authorized tasks; existing tasks keep their binding.</p>
  {:else if current}
    <p>Marina has not bound a repository for this project yet. An administrator binds it in Marina's project and maps the project in Deck's configuration.</p>
  {/if}
  {#if error}<p class="error" role="alert">{error} <button type="button" on:click={load}>Refresh</button></p>{/if}
</section>
<style>
  .repository { padding: 24px; }
  h2 { font-size:18px; margin:0 0 12px; } p { font-size:13px; color:var(--muted); line-height:1.6; } .binding { color:var(--text); overflow-wrap:anywhere; }
  button { min-height:38px; padding:8px 12px; border-radius:6px; border:1px solid var(--line); background:var(--panel); color:var(--text); cursor:pointer; }
  .error { color:var(--danger); }
</style>
