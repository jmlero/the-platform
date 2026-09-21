# Harbor — Deck's design system

Deck is the place to steer work; Marina carries it out. Their shared visual
language borrows the clarity of a harbor: deep navy navigation, ocean teal
controls, open light surfaces, and restrained wave linework. Keep domain language
plain: tasks, decisions, results, and evidence. Maritime decoration belongs in the
brand and page header, never in operational status or control labels.

## Master page template

The application shell in `src/routes/+page.svelte` owns the persistent navigation,
utility bar, workspace scroll area, and overlays. New views use this structure:

1. **Navigation:** 232 px navy sidebar, product mark, workspace identity, grouped
   navigation, factual provider status, Deck & Marina signature. At 980 px and
   below it becomes a slide-out menu.
2. **Utility bar:** 72 px high (64 px on phones), location, task search, project
   import/export, session controls, and the primary “New task” action. Never show
   a shortcut or dropdown affordance without working behavior.
3. **Page heading:** reuse `PageHeading.svelte` with an eyebrow, one clear h1, and
   one sentence explaining the view. The wave motif is decorative and hidden
   from assistive technology.
4. **Content:** 32 px workspace padding, 24 px on tablets, 16 px on phones.
   Overview has three summary cards followed by attention, active work, and
   results. The task board scrolls horizontally; other views reflow vertically.
5. **Detail:** task inspection opens in a right drawer. Creation opens in a
   centered form, becoming a bottom sheet on phones. Both use native modal
   dialogs through `src/lib/dialog.ts` for an inert background, keyboard focus
   containment, Escape dismissal, and focus restoration.

```svelte
<PageHeading
  eyebrow="Human attention"
  title="Decision inbox"
  description="Review the context and choose how work should proceed."
/>
<section class="surface">...</section>
```

Use `BrandMark.svelte` for the Deck mark. `TaskCard.svelte` is shared by overview
and board; `CreateTaskModal.svelte` is shared by task creation and channel
promotion. Add to these patterns instead of creating a new visual treatment for
each screen. This is a Deck implementation and a visual reference for future
Marina surfaces, not a runtime dependency between the products.

## Color palette

`src/app.css` is the source of truth. Use semantic CSS variables in components.
The sidebar overrides foreground and accent tokens for its dark surface.

| Role | Token | Color | Use |
| --- | --- | --- | --- |
| Deep navy | `--sidebar` | `#102D3B` | Navigation and product identity |
| Ocean teal | `--accent` | `#126C77` | Primary actions, selection, focus, active work |
| Teal wash | `--accent-soft` | `#E5F2F2` | Selected controls and supporting icon fields |
| Warm mist | `--bg` | `#F4F6F5` | Workspace background |
| White | `--panel` | `#FFFFFF` | Cards, forms, utility bar, drawers |
| Sea glass | `--panel-soft` | `#EDF2F3` | Grouped fields and quiet supporting surfaces |
| Ink | `--text` | `#183440` | Headings and primary text |
| Slate | `--muted` | `#506772` | Body copy |
| Muted slate | `--subtle` | `#586e79` | Secondary copy and metadata |
| Sand | `--warning` | `#92601A` | Decisions and waiting states |
| Sea green | `--success` | `#267054` | Completed results |
| Coastal blue | `--info` | `#356E99` | Verification and informational status |
| Dusk | `--planning` | `#635C91` | Planning and high priority |
| Coral | `--danger` | `#B04342` | Errors, failures, urgent work |

Primary controls use white text on teal. Status colors use their corresponding
soft backgrounds and always include a text label. A green result means ready for
review, not merged or deployed. Stale evidence and synchronization retain their
own explicit labels. Provider status comes from actual data, never branding.

## Typography and spacing

Use the installed system sans-serif stack (Inter if locally available). No remote
font request is required. Monospace is reserved for record IDs and technical
values. Page titles are 26–32 px; component titles 15–17 px; body text 14 px;
secondary text 13 px; operational metadata 12 px. Compact decorative brand labels
and priority tags may use 10–11 px. Use sentence case, 500–650 weights, and limited
uppercase for eyebrows and compact metadata.

Use the 4 px spacing scale: 4, 8, 12, 16, 24, 32. Controls use a 6 px radius,
cards 10 px, dialogs 14–18 px. Borders are subtle; shadows are shallow on cards
and stronger on overlays. Avoid glowing accents and translucent content panels.

## Interaction and accessibility

- Keep visible keyboard focus, including hidden file and radio inputs via their
  labels. Use accessible names for icon-only controls.
- Keep ordinary controls at least 36–44 px high; navigation items are 44 px.
- Preserve contrast on both the light workspace and dark navigation surface.
- Keep long task titles and evidence readable with wrapping and scrolling.
- At 760 px, overview columns stack and summaries remain visible. Keep import,
  export, creation, and session controls available on narrow screens.
- Empty views explain what will appear there. Search emptiness says “No matching
  work” rather than implying the entire workspace is empty.
- Respect reduced-motion preferences. Never animate status without real data.
- Do not hide consequential authority, approval, budget, or verification details
  to simplify the appearance.

## Checking a change

Run `npm test`, `npm run check`, and `npm run build` from `deck/web`. Product-wide
checks are `make test check` from `deck`. The build refreshes the tracked frontend
embedded in the Go binary.

In a browser, review overview, board, backlog, decisions, channels, task creation,
and task details. Desktop browsers are the acceptance target (operator decision, 2026-09-21).
Existing narrow layouts may remain, but phone support is not required. Verify Tab,
Escape, focus restoration, scrolling, and a task-creation cycle in an isolated
preview database. The design does not change the Deck–Marina API contract.
