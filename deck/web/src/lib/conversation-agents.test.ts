import { describe, expect, it } from 'vitest';
import { configuredConversationAgents, mentionedConversationAgent } from './conversation-agents';
import type { Agent } from './types';

const agents: Agent[] = [
  { id: 'agt_codex', installation_id: 'installation-1', name: 'Codex', kind: 'controller', status: 'idle', capabilities: ['conversation.invoke.prj_default'], last_seen_at: 0, created_at: 0 },
  { id: 'agt_foreign', installation_id: 'installation-3', name: 'Foreign', kind: 'controller', status: 'idle', capabilities: ['conversation.invoke.other'], last_seen_at: 0, created_at: 0 },
  { id: 'agt_worker', installation_id: 'installation-2', name: 'Worker', kind: 'controller', status: 'idle', capabilities: ['work-control'], last_seen_at: 0, created_at: 0 }
];

describe('conversation agent selection', () => {
  it('offers only registered conversation agents and resolves an addressed mention', () => {
    expect(configuredConversationAgents(agents, 'prj_default').map(agent => agent.id)).toEqual(['agt_codex']);
    expect(mentionedConversationAgent('@codex, can you investigate?', agents, 'prj_default')?.id).toBe('agt_codex');
    expect(mentionedConversationAgent('The note says @codex should investigate', agents, 'prj_default')).toBeUndefined();
    expect(mentionedConversationAgent('"@codex investigate"', agents, 'prj_default')).toBeUndefined();
    expect(mentionedConversationAgent('@worker investigate', agents, 'prj_default')).toBeUndefined();
    expect(mentionedConversationAgent('@foreign investigate', agents, 'prj_default')).toBeUndefined();
  });
});

it('ignores a registered worker whose capabilities are null or absent', () => {
 const missing = [{...agents[2],capabilities:null},{...agents[2],id:'missing',capabilities:undefined}] as unknown as Agent[];
 expect(configuredConversationAgents([...missing,...agents],'prj_default').map(agent=>agent.id)).toEqual(['agt_codex']);
});
