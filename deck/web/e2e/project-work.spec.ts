import { createHash } from 'node:crypto';
import { test, expect, type Deck } from './deck';
import type { Page } from '@playwright/test';
import type { Task, TaskDetail, Run, ProjectChannel, ConversationInvocation, EvidenceRecord, PlanRevision } from '../src/lib/types';

const commit = 'a'.repeat(40);

async function openProject(page: Page, deck: Deck, repository: string): Promise<ProjectChannel> {
  await page.goto(deck.url);
  await page.getByRole('button', { name: 'Add project', exact: true }).first().click();
  const dialog = page.getByRole('dialog', { name: 'Connect a project' });
  await dialog.getByLabel('GitHub repository').fill(repository);
  const created = page.waitForResponse(response => response.url().endsWith('/operator/projects') && response.request().method() === 'POST');
  await dialog.getByRole('button', { name: 'Create project', exact: true }).click();
  const response = await created;
  expect(response.status()).toBe(201);
  const project: ProjectChannel = await response.json();
  await expect(page.getByRole('heading', { name: repository.split('/')[1], exact: true })).toBeVisible();
  await expect(dialog).not.toBeVisible();
  await page.getByRole('navigation', { name: 'Projects', exact: true }).getByRole('button', { name: repository.split('/')[1], exact: true }).click();
  return project;
}

async function execute(deck: Deck, task: Task, landed: boolean, afterClaim?: () => Promise<void>): Promise<void> {
  const agent = `browser-${task.id}`;
  await deck.call('POST', '/agents', { id: agent, installation_id: agent, name: 'Deterministic browser fixture' }, 201);
  const run = await deck.call<Run>('POST', `/tasks/${task.id}/claim`, { agent_id: agent, expected_task_version: task.version, lease_seconds: 300 }, 201);
  await afterClaim?.();
  const guard = async () => {
    const detail = await deck.call<TaskDetail>('GET', `/operator/tasks/${task.id}`);
    const current = detail.runs.find(item => item.id === run.id)!;
    return { agent_id: agent, expected_version: current.version, fence_token: current.fence_token };
  };
  const plan = await deck.call<{ plan_revision: PlanRevision }>('POST', `/runs/${run.id}/plan`, { ...await guard(), plan: 'Record deterministic browser fixture evidence', require_approval: false });
  await deck.call('POST', `/runs/${run.id}/verify`, await guard(), 204);
  const evidence: string[] = [];
  for (const kind of landed ? ['test', 'semantic_review', 'landing'] : ['acceptance']) {
    const item = await deck.call<EvidenceRecord>('POST', `/runs/${run.id}/evidence`, {
      ...await guard(), id: `${task.id}-${kind}`, plan_revision_id: plan.plan_revision.id,
      type: kind, producer: 'browser-builder-fixture', verifier: `browser-independent-${kind}`,
      subject_digest: createHash('sha256').update(task.acceptance_criteria).digest('hex'),
      source_revision: landed ? commit : 'browser-fixture-v1', command: `deterministic browser fixture ${kind}`,
      outcome: 'passed', provenance_uri: `test://browser/${kind}`, provenance_digest: `sha256:${kind}`,
      artifact_uri: `test://browser/${kind}`, artifact_digest: `sha256:${kind}`, observed_at: Date.now()
    }, 201);
    evidence.push(item.id);
  }
  await deck.call('POST', `/runs/${run.id}/complete`, {
    ...await guard(), summary: 'Browser fixture result; no Git or model operation', evidence_ids: evidence,
    ...(landed ? { landing: { repository: task.context!.repository!.repository, base_branch: 'main', commit, observed_at: Date.now() } } : {})
  }, 204);
}

test('opens a project, exchanges chat in a durable thread, and retains the selected project', async ({ page, deck }) => {
  const project = await openProject(page, deck, 'example/browser-chat');
  const agentID = 'browser-chat-fixture';
  await deck.call('POST', '/agents', {
    id: agentID, installation_id: 'browser-fixture', name: 'Browser chat fixture', capabilities: [`conversation.invoke.${project.project.id}`]
  }, 201);
  await page.getByLabel('Conversation agent', { exact: true }).selectOption(agentID);
  await page.getByRole('textbox', { name: 'Message project channel' }).fill('Explore the browser acceptance flow.');
  const invoked = page.waitForResponse(response => response.url().endsWith('/invocations') && response.request().method() === 'POST');
  await page.getByRole('button', { name: 'Send message', exact: true }).click();
  const invocation: ConversationInvocation = await (await invoked).json();
  await expect(page.getByText('Explore the browser acceptance flow.', { exact: true })).toBeVisible();
  await expect(page.getByRole('textbox', { name: 'Reply in thread', exact: true })).toBeVisible();
  const reply = 'The browser fixture saved your request and this reply.';
  await deck.call('POST', '/conversation/outcomes', {
    invocation_message_id: invocation.message.id, agent_id: agentID, status: 'completed', body: reply,
    result_digest: `sha256:${createHash('sha256').update(reply).digest('hex')}`
  });
  await expect(page.getByText(reply, { exact: true })).toBeVisible();
  const threadURL = page.url();
  await page.reload();
  await expect(page.getByText(reply, { exact: true })).toBeVisible();
  await expect(page).toHaveURL(threadURL);
  await expect(page.getByRole('navigation', { name: 'Projects', exact: true }).getByRole('button', { name: 'browser-chat', exact: true })).toHaveAttribute('aria-current', 'page');
});

test('moves an evidenced landing from to do through in progress to recently landed and opens its result', async ({ page, deck }) => {
  const project = await openProject(page, deck, 'example/browser-landing');
  await deck.call('PUT', `/projects/${project.project.id}/repository/result`, {
    request_revision: project.repository.revision, status: 'connected',
    binding: { version: 1, repository: 'example/browser-landing', clone_url: 'https://github.com/example/browser-landing.git', base_branch: 'main' }
  });
  const task = await deck.call<Task>('POST', '/tasks', {
    project_id: project.project.id, title: 'Land the browser-tested result', acceptance_criteria: 'The tested result is on main',
    status: 'ready', budget: { time_minutes: 30, max_attempts: 3, authority: ['source.read', 'source.write', 'source.fast_forward'] }
  }, 201);
  const todo = page.getByRole('region', { name: 'To do', exact: true });
  const progress = page.getByRole('region', { name: 'In progress', exact: true });
  const recent = page.getByRole('region', { name: 'Recently landed or accepted', exact: true });
  await expect(todo.getByRole('button', { name: `${task.title} Ready`, exact: true })).toBeVisible();
  await execute(deck, task, true, async () => {
    await expect(progress.getByRole('button', { name: new RegExp(task.title) })).toBeVisible();
    await expect(todo.getByRole('button', { name: new RegExp(task.title) })).toHaveCount(0);
  });
  await expect(todo.getByRole('button', { name: new RegExp(task.title) })).toHaveCount(0);
  await expect(progress.getByRole('button', { name: new RegExp(task.title) })).toHaveCount(0);
  await expect(recent.getByRole('heading', { name: 'Recently landed', exact: true })).toBeVisible();
  const result = recent.getByRole('button', { name: `${task.title} Landed`, exact: true });
  await expect(result).toBeVisible();
  await result.click();
  const drawer = page.getByRole('dialog', { name: task.title, exact: true });
  await expect(drawer.getByRole('heading', { name: 'Landed on main', exact: true })).toBeVisible();
  await expect(drawer.getByRole('button', { name: /Accept result|Confirm code available/ })).toHaveCount(0);
  await expect(drawer.getByRole('link', { name: 'Open landed commit', exact: true })).toHaveAttribute('href', `https://github.com/example/browser-landing/commit/${commit}`);
  await drawer.getByRole('button', { name: /^Evidence/ }).click();
  await expect(drawer.getByText('landing · passed', { exact: true })).toBeVisible();
  await expect(drawer.getByText('semantic_review · passed', { exact: true })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(drawer).not.toBeVisible();
  await expect(result).toBeFocused();
  await page.getByRole('button', { name: /^Needs attention/ }).click();
  await expect(page.getByText('Nothing needs your attention.', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: new RegExp(task.title) })).toHaveCount(0);
});

test('keeps a legacy result awaiting operator review until it is accepted', async ({ page, deck }) => {
  await openProject(page, deck, 'example/browser-acceptance');
  await page.getByRole('button', { name: 'Add task', exact: true }).click();
  const create = page.getByRole('dialog', { name: 'Create a task', exact: true });
  await expect(create.getByLabel('Outcome', { exact: true })).toBeFocused();
  await create.getByLabel('Outcome', { exact: true }).fill('Accept a legacy verification result');
  await create.getByLabel('Acceptance criteria', { exact: true }).fill('The fixture is verified');
  await create.getByText('Priority and execution limits', { exact: true }).click();
  await create.getByRole('checkbox', { name: /^Allow Marina to change the configured repository/ }).check();
  await create.getByRole('radio', { name: /^Ready queue/ }).check();
  const created = page.waitForResponse(response => response.url().endsWith('/api/v1/tasks') && response.request().method() === 'POST');
  await create.getByRole('button', { name: 'Create task', exact: true }).click();
  const response = await created;
  expect(response.status()).toBe(201);
  const task: Task = await response.json();
  await expect(create).not.toBeVisible();
  await page.getByRole('dialog', { name: task.title, exact: true }).getByRole('button', { name: 'Close task', exact: true }).click();
  await execute(deck, task, false);
  const progress = page.getByRole('region', { name: 'In progress', exact: true });
  await progress.getByRole('button', { name: `${task.title} Awaiting review`, exact: true }).click();
  const drawer = page.getByRole('dialog', { name: task.title, exact: true });
  await drawer.getByRole('button', { name: 'Accept result · Done', exact: true }).click();
  await expect(drawer.getByRole('heading', { name: 'Result accepted', exact: true })).toBeVisible();
  await drawer.getByRole('button', { name: 'Close task', exact: true }).click();
  await expect(page.getByRole('region', { name: 'Recently landed or accepted', exact: true }).getByRole('button', { name: `${task.title} Done`, exact: true })).toBeVisible();
  await expect(progress.getByRole('button', { name: new RegExp(task.title) })).toHaveCount(0);
});

test('approves the exact PRD in its thread and retains one publication task after reload', async ({page,deck}) => {
 const project=await openProject(page,deck,'example/browser-prd');
 await deck.call('PUT',`/projects/${project.project.id}/repository/result`,{request_revision:project.repository.revision,status:'connected',binding:{version:1,repository:'example/browser-prd',clone_url:'https://github.com/example/browser-prd.git',base_branch:'main'}});
 const agentID='prd-chat';
 await deck.call('POST','/agents',{id:agentID,installation_id:'prd-fixture',name:'PRD agent',capabilities:[`conversation.invoke.${project.project.id}`]},201);
 await deck.call('PUT',`/projects/${project.project.id}/workloads`,{expires_at:Date.now()+240000,profiles:[{id:'repository/v2',digest:'b'.repeat(64),harness:'codex/v1',authority:['source.read','source.write','source.fast_forward'],max_tokens:30000,budget_minutes:30,min_attempts:3}]});
 await page.getByLabel('Conversation agent',{exact:true}).selectOption(agentID);
 await page.getByRole('textbox',{name:'Message project channel'}).fill('Draft a PRD for saved filters');
 const invoked=page.waitForResponse(r=>r.url().endsWith('/invocations')&&r.request().method()==='POST');
 await page.getByRole('button',{name:'Send message',exact:true}).click();
 const inv:ConversationInvocation=await(await invoked).json();
 const content='# Saved filters\n\nUsers can save and restore filters.\n';
 const outcome=await deck.call<{message_id:string}>('POST','/conversation/outcomes',{invocation_message_id:inv.message.id,agent_id:agentID,status:'completed',body:'Review this PRD.',result_digest:'prd-result',prd:{title:'Saved filters',path:'docs/prd/saved-filters.md',content}});
 const card=page.getByRole('region',{name:'PRD draft: Saved filters',exact:true});
 await expect(card.getByText('docs/prd/saved-filters.md',{exact:true})).toBeVisible();
 await expect(card.locator('pre')).toHaveText(content);
 await card.getByLabel('Token limit',{exact:true}).fill('20000');
 const observer=await page.context().newPage();await observer.goto(page.url());
 await expect(observer.getByRole('button',{name:'Approve PRD and publish',exact:true})).toBeVisible();
 const approved=page.waitForResponse(r=>r.url().endsWith(`/messages/${outcome.message_id}/prd/approve`));
 await card.getByRole('button',{name:'Approve PRD and publish',exact:true}).click();
 const response=await approved;expect(response.status()).toBe(201);const task:Task=await response.json();
 expect(task.prd?.content).toBe(content);expect(task.status).toBe('ready');
 await expect(observer.getByText('Publication authorized',{exact:true})).toBeVisible();await observer.close();
 await page.reload();await expect(card.getByText('Publication authorized',{exact:true})).toBeVisible();
 await expect(card.getByRole('button',{name:'Approve PRD and publish',exact:true})).toHaveCount(0);
 const reply=await deck.call<Task>('POST',`/operator/messages/${outcome.message_id}/prd/approve`,{digest:task.prd!.digest,profile_digest:'b'.repeat(64),budget:task.budget},201);
 expect(reply.id).toBe(task.id);
});

test('approves ordered PRD tasks, shows landed results, and records PRD acceptance in chat', async ({page,deck}) => {
 const project=await openProject(page,deck,'example/browser-prd-tasks');
 await deck.call('PUT',`/projects/${project.project.id}/repository/result`,{request_revision:project.repository.revision,status:'connected',binding:{version:1,repository:'example/browser-prd-tasks',clone_url:'https://github.com/example/browser-prd-tasks.git',base_branch:'main'}});
 const agent='prd-tasks-chat';
 await deck.call('POST','/agents',{id:agent,installation_id:'prd-tasks-fixture',name:'PRD tasks agent',capabilities:[`conversation.invoke.${project.project.id}`]},201);
 await deck.call('PUT',`/projects/${project.project.id}/workloads`,{expires_at:Date.now()+240000,profiles:[{id:'repository/v2',digest:'c'.repeat(64),harness:'codex/v1',authority:['source.read','source.write','source.fast_forward'],max_tokens:30000,budget_minutes:30,min_attempts:3}]});
 const inv=await deck.call<ConversationInvocation>('POST',`/operator/channels/${project.channels[0].id}/invocations`,{agent_id:agent,request_id:'prd-draft',body:'Draft the filters PRD'},201);
 const out=await deck.call<{message_id:string}>('POST','/conversation/outcomes',{invocation_message_id:inv.message.id,agent_id:agent,status:'completed',body:'Review the requirements',result_digest:'filters-draft',prd:{title:'Filters',path:'docs/prd/filters.md',content:'# Filters\n\nSave and restore filters.\n'}});
 const messages=await deck.call<{items:{id:string;prd?:{digest:string}}[]}>('GET',`/operator/channels/${project.channels[0].id}/messages`);
 const source=messages.items.find(message=>message.id===out.message_id)!;
 const publication=await deck.call<Task>('POST',`/operator/messages/${out.message_id}/prd/approve`,{digest:source.prd!.digest,profile_digest:'c'.repeat(64),budget:{time_minutes:30,max_tokens:20000,max_attempts:3,max_cost_minor:0,currency:'USD',max_concurrency:1,authority:['source.read','source.write','source.fast_forward']}},201);
 await execute(deck,publication,true);
 await page.goto(`${deck.url}/?thread=${inv.thread.id}`);
 const draft=page.getByRole('region',{name:'PRD draft: Filters',exact:true});
 await expect(draft.getByText('PRD published',{exact:false})).toBeVisible();
 const proposed=page.waitForResponse(response=>response.url().endsWith('/invocations')&&response.request().method()==='POST');
 await draft.getByRole('button',{name:'Propose implementation tasks',exact:true}).click();
 const turn:ConversationInvocation=await(await proposed).json();
 const proposal=await deck.call<{message_id:string}>('POST','/conversation/outcomes',{invocation_message_id:turn.message.id,agent_id:agent,status:'completed',body:'Review the task order',result_digest:'ordered-tasks',prd_tasks:{publication_task_id:publication.id,revision:commit,tasks:[{title:'Save filters',description:'Persist saved filters',acceptance_criteria:'Unit and browser checks verify persistence after reload'},{title:'Restore filters',description:'Restore a saved filter',acceptance_criteria:'Unit and browser checks verify restored filter values'}]}});
 const card=page.getByRole('region',{name:'PRD task proposal',exact:true});
 await expect(card.getByText('Save filters',{exact:true})).toBeVisible();
 await expect(card.getByText('Restore filters',{exact:true})).toBeVisible();
 const approved=page.waitForResponse(response=>response.url().endsWith(`/messages/${proposal.message_id}/prd/tasks/approve`));
 await card.getByRole('button',{name:'Approve ordered tasks',exact:true}).click();
 const tasks:Task[]=await(await approved).json();expect(tasks).toHaveLength(2);expect(tasks[1].depends_on).toEqual([tasks[0].id]);expect(tasks[0].prd_reference?.revision).toBe(commit);
 await page.reload();await expect(card.getByText('Ordered tasks authorized',{exact:true})).toBeVisible();
 await expect(draft.getByRole('button',{name:'Accept PRD',exact:true})).toHaveCount(0);
 const observer=await page.context().newPage();await observer.goto(page.url());
 await execute(deck,tasks[0],true);await execute(deck,tasks[1],true);
 await expect(draft.getByText('2 of 2 tasks landed',{exact:true})).toBeVisible();
 await draft.getByRole('button',{name:'Accept PRD',exact:true}).click();
 await expect(draft.getByText('PRD accepted',{exact:true})).toBeVisible();
 await expect(observer.getByText('PRD accepted',{exact:true})).toBeVisible();await observer.close();
 await page.reload();await expect(draft.getByText('PRD accepted',{exact:true})).toBeVisible();
 await draft.getByLabel('Requested PRD follow-ups',{exact:true}).fill('Add keyboard navigation');
 const followup=page.waitForResponse(response=>response.url().endsWith('/invocations')&&response.request().method()==='POST');
 await draft.getByRole('button',{name:'Request follow-up proposal',exact:true}).click();
 const reply:ConversationInvocation=await(await followup).json();expect(reply.thread.id).toBe(inv.thread.id);expect(reply.message.body).toContain('Add keyboard navigation');
 await deck.call('POST','/conversation/outcomes',{invocation_message_id:reply.message.id,agent_id:agent,status:'completed',body:'Review the keyboard follow-up',result_digest:'follow-up-tasks',prd_tasks:{publication_task_id:publication.id,revision:commit,tasks:[{title:'Keyboard navigation',description:'Make saved filters keyboard accessible',acceptance_criteria:'Browser checks verify keyboard navigation'}]}});
 const followupCard=page.getByRole('region',{name:'PRD task proposal',exact:true}).last();
 await expect(followupCard.getByText('Keyboard navigation',{exact:true})).toBeVisible();
 const followupApproved=page.waitForResponse(response=>response.url().endsWith('/prd/tasks/approve'));
 await followupCard.getByRole('button',{name:'Approve ordered tasks',exact:true}).click();
 const followupTasks:Task[]=await(await followupApproved).json();expect(followupTasks).toHaveLength(1);
 await page.getByRole('button',{name:'Close task',exact:true}).click();
 await expect(draft.getByText('PRD accepted',{exact:true})).toHaveCount(0);
 await execute(deck,followupTasks[0],true);
 await expect(draft.getByText('3 of 3 tasks landed',{exact:true})).toBeVisible();
 await draft.getByRole('button',{name:'Accept PRD',exact:true}).click();
 await expect(draft.getByText('PRD accepted',{exact:true})).toBeVisible();
 const all=await deck.call<TaskDetail>('GET',`/operator/tasks/${publication.id}`);expect(all.task.prd_acceptance).toBeDefined();
});
