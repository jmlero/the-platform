<script lang="ts">
  export let title: string;
</script>

<header class="page-heading">
  <h1>{title}</h1>
</header>

<style>
  .page-heading { flex: 0 0 auto; padding-bottom: var(--space-6); }
  h1 { margin: 0; font-size: 1.5rem; font-weight: 600; letter-spacing: -.025em; line-height: 1.3; }
  @media (max-width: 760px) { .page-heading { padding-bottom: var(--space-4); } }
</style>
