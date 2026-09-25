import type { Agent } from './types';

export function configuredConversationAgents(agents: Agent[], projectID: string): Agent[] {
  return agents.filter(agent => !!agent.installation_id && agent.capabilities.includes(`conversation.invoke.${projectID}`));
}

export function mentionedConversationAgent(body: string, agents: Agent[], projectID: string): Agent | undefined {
  const mention = /^@([a-z0-9_.-]+)(?=$|[\s,:!?])/i.exec(body.trimStart())?.[1]?.toLowerCase();
  if (!mention) return undefined;
  const matches = configuredConversationAgents(agents, projectID).filter(agent =>
    agent.id.toLowerCase() === mention || agent.name.toLowerCase().replace(/\s+/g, '-') === mention
  );
  return matches.length === 1 ? matches[0] : undefined;
}
