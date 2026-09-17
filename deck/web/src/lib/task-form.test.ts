import { describe, expect, it, vi } from 'vitest';
import { render } from 'svelte/server';
import CreateTaskModal from './components/CreateTaskModal.svelte';

describe('task form reused for channel promotion', () => {
  it('prefills conversation context with two attempts and explicit opt-in PR authority', () => {
    const { body } = render(CreateTaskModal, { props: {
      initialTitle: 'Fix the agreed regression',
      initialDescription: 'Context from the selected channel message',
      requireCriteria: true,
      onclose: vi.fn(), oncreate: vi.fn()
    } });
    expect(body).toContain('Fix the agreed regression');
    expect(body).toContain('Context from the selected channel message');
    expect(body).toMatch(/Maximum attempts[\s\S]*?value="2"/);
    expect(body).toMatch(/Maximum tokens[\s\S]*?value="200000"/);
    expect(body).toContain('Deadline (optional)');
    expect(body).toContain('Maximum cost (minor units)');
    expect(body).toContain('Maximum concurrency');
    expect(body).toContain('Allow Marina to change the configured repository and open a PR.');
    expect(body.match(/<input[^>]*type="checkbox"[^>]*>/)?.[0]).not.toContain('checked');
  });
});
