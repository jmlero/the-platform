<script lang="ts">
  export let eyebrow: string;
  export let title: string;
  export let description: string;
</script>

<header class="page-heading">
  <div>
    <span class="eyebrow">{eyebrow}</span>
    <h1>{title}</h1>
    <p>{description}</p>
  </div>
  <div class="heading-detail" aria-hidden="true">
    <svg width="112" height="48" viewBox="0 0 112 48" fill="none">
      <path d="M0 22c14-12 28-12 42 0s28 12 42 0 28-12 42 0M0 32c14-12 28-12 42 0s28 12 42 0 28-12 42 0M0 42c14-12 28-12 42 0s28 12 42 0 28-12 42 0" stroke="currentColor" />
    </svg>
  </div>
</header>

<style>
  .page-heading { display: flex; flex: 0 0 auto; align-items: center; justify-content: space-between; gap: var(--space-6); padding: 0 0 var(--space-6); }
  .eyebrow { color: var(--accent); }
  h1 { margin: 6px 0 8px; font-size: clamp(1.6rem, 2.3vw, 2rem); font-weight: 650; letter-spacing: -.035em; line-height: 1.2; }
  p { margin: 0; max-width: 680px; color: var(--muted); font-size: var(--text-base); }
  .heading-detail { color: var(--accent); opacity: .2; overflow: hidden; flex-shrink: 0; }
  @media (max-width: 760px) { .heading-detail { display: none; } .page-heading { padding-bottom: var(--space-4); } }
</style>
