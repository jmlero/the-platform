import { test, expect, type Deck } from './deck';
import type { Page } from '@playwright/test';
import type { Agent, Task, TaskDetail, ProjectChannel } from '../src/lib/types';

// Every test drives the browser against a real Deck whose work runs in a real
// Marina process. PRD drafts and ordered PRD task proposals need an agent the
// Marina sandbox does not simulate; store and HTTP tests cover those flows.

const repository = 'example/acceptance';
const landedCommit = '7c1e0f2a9b8d4c3e5f6a7b8c9d0e1f2a3b4c5d6e';

async function openProject(page: Page, deck: Deck): Promise<ProjectChannel> {
  await page.goto(deck.url);
  await page.getByRole('button', { name: 'Add project', exact: true }).first().click();
  const dialog = page.getByRole('dialog', { name: 'Connect a project' });
  await dialog.getByLabel('GitHub repository').fill(repository);
  const created = page.waitForResponse(response => response.url().endsWith('/operator/projects') && response.request().method() === 'POST');
  await dialog.getByRole('button', { name: 'Create project', exact: true }).click();
  const response = await created;
  expect(response.status()).toBe(201);
  const project: ProjectChannel = await response.json();
  await expect(dialog).not.toBeVisible();
  await deck.mapProject(project.project.id);
  await expect.poll(async () => {
    const agents = await deck.call<Agent[]>('GET', '/agents');
    const status = await deck.call<{ status: string }>('GET', `/operator/projects/${project.project.id}/repository`);
    return agents.some(agent => agent.id === 'marina' && agent.capabilities.includes(`conversation.invoke.${project.project.id}`)) && status.status === 'connected';
  }, { timeout: 20_000 }).toBe(true);
  await page.reload();
  await page.getByRole('navigation', { name: 'Projects', exact: true }).getByRole('button', { name: 'acceptance', exact: true }).click();
  return project;
}

async function readyTask(deck: Deck, project: ProjectChannel, title: string): Promise<Task> {
  return deck.call<Task>('POST', '/tasks', {
    project_id: project.project.id, title, acceptance_criteria: 'A unit test covers it', status: 'ready',
    budget: { time_minutes: 30, max_tokens: 10000, max_attempts: 3, authority: ['source.read', 'source.write', 'source.fast_forward'] }
  }, 201);
}

async function waitForStatus(deck: Deck, task: Task, status: string) {
  await expect.poll(async () => (await deck.call<TaskDetail>('GET', `/operator/tasks/${task.id}`)).task.status, { timeout: 30_000 }).toBe(status);
}

async function sendToMarina(page: Page, body: string) {
  await page.getByLabel('Conversation agent', { exact: true }).selectOption('marina');
  await page.getByRole('textbox', { name: 'Message project channel' }).fill(body);
  const invoked = page.waitForResponse(response => response.url().endsWith('/invocations') && response.request().method() === 'POST');
  await page.getByRole('button', { name: 'Send message', exact: true }).click();
  expect((await invoked).status()).toBe(201);
  await expect(page.getByRole('textbox', { name: 'Reply in thread', exact: true })).toBeVisible();
}

test('chats with Marina in a durable thread that survives reload', async ({ page, deck }) => {
  await openProject(page, deck);
  await sendToMarina(page, 'Explore the browser acceptance flow.');
  await expect(page.getByText('Sandbox reply to: Explore the browser acceptance flow.', { exact: true })).toBeVisible({ timeout: 20_000 });
  const threadURL = page.url();
  await page.reload();
  await expect(page.getByText('Sandbox reply to: Explore the browser acceptance flow.', { exact: true })).toBeVisible();
  await expect(page).toHaveURL(threadURL);
});

test('answers Marina\'s clarification question in the thread', async ({ page, deck }) => {
  await openProject(page, deck);
  await sendToMarina(page, 'Please clarify the design');
  await expect(page.getByText('Which part should I explain?', { exact: true }).first()).toBeVisible({ timeout: 20_000 });
  await page.getByRole('button', { name: 'Store', exact: true }).click();
  await page.getByRole('button', { name: 'Send answer', exact: true }).click();
  await expect(page.getByText('Sandbox reply about Store', { exact: false })).toBeVisible({ timeout: 20_000 });
});

test('approves Marina\'s plan in the browser and shows the evidenced landing', async ({ page, deck }) => {
  const project = await openProject(page, deck);
  const task = await readyTask(deck, project, 'Land the browser-tested result');
  await waitForStatus(deck, task, 'waiting_decision');
  await page.reload();
  await page.getByRole('button', { name: new RegExp(task.title) }).first().click();
  const drawer = page.getByRole('dialog', { name: task.title, exact: true });
  await drawer.getByRole('button', { name: 'Approve plan', exact: true }).click();
  await expect(drawer.getByRole('heading', { name: 'Landed on main', exact: true })).toBeVisible({ timeout: 30_000 });
  await expect(drawer.getByRole('button', { name: /Accept result|Confirm code available/ })).toHaveCount(0);
  await expect(drawer.getByRole('link', { name: 'Open landed commit', exact: true })).toHaveAttribute('href', `https://github.com/${repository}/commit/${landedCommit}`);
  await drawer.getByRole('button', { name: /^Evidence/ }).click();
  await expect(drawer.getByText('landing · passed', { exact: true })).toBeVisible();
  await expect(drawer.getByText('semantic_review · passed', { exact: true })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.getByRole('region', { name: 'Recently landed or accepted', exact: true }).getByRole('button', { name: `${task.title} Landed`, exact: true })).toBeVisible();
  const run = await deck.marina<{ status: { phase: string } }>(`/runs/${(await deck.call<TaskDetail>('GET', `/operator/tasks/${task.id}`)).runs[0].id}`);
  expect(run.status.phase).toBe('Succeeded');
});

test('cancels a waiting run from task details through Marina', async ({ page, deck }) => {
  const project = await openProject(page, deck);
  const task = await readyTask(deck, project, 'Cancel this browser run');
  await waitForStatus(deck, task, 'waiting_decision');
  await page.reload();
  await page.getByRole('button', { name: new RegExp(task.title) }).first().click();
  const drawer = page.getByRole('dialog', { name: task.title, exact: true });
  await drawer.getByRole('button', { name: 'Cancel run', exact: true }).click();
  await waitForStatus(deck, task, 'cancelled');
  const detail = await deck.call<TaskDetail>('GET', `/operator/tasks/${task.id}`);
  expect(detail.commands.map(command => command.status)).toEqual(['completed']);
  expect((await deck.marina<{ status: { phase: string } }>(`/runs/${detail.runs[0].id}`)).status.phase).toBe('Cancelled');
});
