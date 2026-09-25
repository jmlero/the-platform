import type {
  ProjectChannel, ConversationThread, ConversationInvocation, ConversationQuestion, ContextSource, ContextSourceInput, ResolvedContext, RepositoryRequest, RepositoryRequestInput,
  AppEvent,
  Bootstrap,
  Channel,
  ChannelMessage,
  CanonicalReferences,
  CreateTaskInput,
  Decision,
  Task,
  TaskDetail,
  TaskStatus,
  UpdateTaskInput
} from './types';

export interface OperatorSession {
  authenticated: boolean;
  operator_id: string;
  expires_at?: number;
}

export class ApiError extends Error {
  constructor(
    message: string,
    public status: number
  ) {
    super(message);
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const method = (init?.method ?? 'GET').toUpperCase();
  const csrfToken = !['GET', 'HEAD', 'OPTIONS'].includes(method)
    ? readCookie('deck_operator_csrf')
    : '';
  const response = await fetch(path, {
    ...init,
    credentials: 'same-origin',
    headers: {
      'Content-Type': 'application/json',
      ...(csrfToken ? { 'X-CSRF-Token': csrfToken } : {}),
      ...init?.headers
    }
  });

  if (!response.ok) {
    let message = `Request failed (${response.status})`;
    try {
      const body = (await response.json()) as { error?: string };
      message = body.error ?? message;
    } catch {
      // The status still provides a useful error when the response is not JSON.
    }
    if (response.status === 401 && typeof window !== 'undefined') {
      window.dispatchEvent(new Event('deck:unauthenticated'));
    }
    throw new ApiError(message, response.status);
  }
  if (response.status === 204) {
    return undefined as T;
  }
  return (await response.json()) as T;
}

export const api = {
 projects: () => request<ProjectChannel[]>('/api/v1/operator/projects'),
 createProject: (repository:string, base_branch:string) => request<ProjectChannel>('/api/v1/operator/projects', {method:'POST',body:JSON.stringify({repository,base_branch})}),
 acceptTask: (task:Task, source_available:boolean) => request<Task>(`/api/v1/operator/tasks/${task.id}/accept`, {method:'POST',body:JSON.stringify({version:task.version,source_available})}),
 dependencies: (task:Task, depends_on:string[]) => request<Task>(`/api/v1/operator/tasks/${task.id}/dependencies`,{method:'PUT',body:JSON.stringify({version:task.version,depends_on})}),
 createThread: (channel:string,message:string) => request<ConversationThread>(`/api/v1/operator/channels/${channel}/threads`,{method:'POST',body:JSON.stringify({root_message_id:message})}),
 getThread: (id:string) => request<ConversationThread>(`/api/v1/operator/threads/${encodeURIComponent(id)}`),
 invokeConversation: (channel:string, input:{agent_id:string;request_id:string;body?:string;thread_id?:string;message_id?:string}) => request<ConversationInvocation>(`/api/v1/operator/channels/${encodeURIComponent(channel)}/invocations`,{method:'POST',body:JSON.stringify(input)}),
 conversationQuestions: (thread:string) => request<{items:ConversationQuestion[]}>(`/api/v1/operator/threads/${encodeURIComponent(thread)}/questions`),
 answerConversationQuestion: (id:string,answer:string,request_id:string) => request<ConversationQuestion>(`/api/v1/operator/conversation/questions/${encodeURIComponent(id)}/answer`,{method:'POST',body:JSON.stringify({answer,request_id})}),

 repository: (projectID = 'prj_default') => request<RepositoryRequest>(`/api/v1/operator/projects/${encodeURIComponent(projectID)}/repository`),
 requestRepository: (input: RepositoryRequestInput, projectID = 'prj_default') => request<RepositoryRequest>(`/api/v1/operator/projects/${encodeURIComponent(projectID)}/repository`, {method: 'PUT', body: JSON.stringify(input)}),
  contextSources: (project = 'prj_default') => request<ContextSource[]>(`/api/v1/operator/projects/${encodeURIComponent(project)}/context/sources`),
  putContextSource: (input: ContextSourceInput, project = 'prj_default') => request<ContextSource>(`/api/v1/operator/projects/${encodeURIComponent(project)}/context/sources`, {method: 'PUT', body: JSON.stringify(input)}),
  revokeContextSource: (id: string, revision: number, project = 'prj_default') => request<unknown>(`/api/v1/operator/projects/${encodeURIComponent(project)}/context/sources/${encodeURIComponent(id)}/${revision}/revoke`, {method: 'POST'}),
  taskContext: (id: string) => request<ResolvedContext>(`/api/v1/operator/tasks/${encodeURIComponent(id)}/context`),
  runContext: (id: string) => request<ResolvedContext>(`/api/v1/operator/runs/${encodeURIComponent(id)}/context`),
  session: () => request<OperatorSession>('/api/v1/operator/session'),
  login: (token: string) =>
    request<OperatorSession>('/api/v1/operator/session', {
      method: 'POST',
      body: JSON.stringify({ token })
    }),
  logout: () => request<void>('/api/v1/operator/session', { method: 'DELETE' }),
  bootstrap: () => request<Bootstrap>('/api/v1/bootstrap'),
  task: (id: string) => request<TaskDetail>(`/api/v1/operator/tasks/${id}`),
  cancelRun: (id: string) => request<unknown>(`/api/v1/operator/runs/${id}/commands`, {method:'POST', body:JSON.stringify({type:'cancel',reason:'Operator cancelled from task details'})}),
  deleteDraft: (task: Task) => request<void>(`/api/v1/operator/tasks/${task.id}`, {method: 'DELETE', body: JSON.stringify({version: task.version})}),
  archiveTask: (task: Task, archived: boolean) => request<Task>(`/api/v1/operator/tasks/${task.id}/archive`, {method: 'PUT', body: JSON.stringify({version: task.version, archived})}),
  createTask: (input: CreateTaskInput) =>
    request<Task>('/api/v1/tasks', { method: 'POST', body: JSON.stringify(input) }),
  updateTask: (id: string, input: UpdateTaskInput) =>
    request<Task>(`/api/v1/tasks/${id}`, { method: 'PATCH', body: JSON.stringify(input) }),
  moveTask: (id: string, status: TaskStatus) =>
    request<Task>(`/api/v1/tasks/${id}/move`, {
      method: 'POST',
      body: JSON.stringify({ status })
    }),
  answerDecision: (id: string, answer: string) =>
    request<Decision>(`/api/v1/operator/decisions/${id}/answer`, {
      method: 'POST',
      body: JSON.stringify({ answer })
    }),
  createChannel: (name: string, projectID = 'prj_default') =>
    request<Channel>('/api/v1/operator/channels', {
      method: 'POST',
      body: JSON.stringify({ name, project_id: projectID })
    }),
  channelMessages: (channelID: string, cursor = '') =>
    request<{ items: ChannelMessage[]; next_cursor?: string }>(`/api/v1/operator/channels/${channelID}/messages${cursor ? `?cursor=${encodeURIComponent(cursor)}` : ''}`),
  postChannelMessage: (channelID: string, body: string, references: CanonicalReferences = {}, thread_id = '') =>
    request<ChannelMessage>(`/api/v1/operator/channels/${channelID}/messages`, {
      method: 'POST',
      body: JSON.stringify({ body, references, thread_id })
    }),
  promoteMessage: (messageID: string, input: CreateTaskInput) =>
    request<Task>(`/api/v1/operator/messages/${messageID}/promote`, {
      method: 'POST',
      body: JSON.stringify(input)
    }),
  exportURL: (projectID = 'prj_default') =>
    `/api/v1/operator/export?project_id=${encodeURIComponent(projectID)}`,
  importProject: (encoded: string) =>
    request<void>('/api/v1/operator/import', { method: 'POST', body: encoded })
};

function readCookie(name: string): string {
  if (typeof document === 'undefined') return '';
  const prefix = `${encodeURIComponent(name)}=`;
  for (const value of document.cookie.split(';')) {
    const candidate = value.trim();
    if (candidate.startsWith(prefix)) {
      return decodeURIComponent(candidate.slice(prefix.length));
    }
  }
  return '';
}

export function subscribeToEvents(
  onEvent: (event: AppEvent) => void,
  onConnection: (connected: boolean) => void
): () => void {
  const source = new EventSource('/api/v1/operator/events/stream');
  source.onopen = () => onConnection(true);
  source.onerror = () => onConnection(false);
  source.onmessage = (message) => {
    try {
      onEvent(JSON.parse(message.data) as AppEvent);
    } catch {
      // A malformed event should not break subsequent live updates.
    }
  };
  return () => source.close();
}
