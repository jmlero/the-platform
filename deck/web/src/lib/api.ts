import type {
  Agent,
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
    ? readCookie('appdeck_operator_csrf')
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
      window.dispatchEvent(new Event('appdeck:unauthenticated'));
    }
    throw new ApiError(message, response.status);
  }
  if (response.status === 204) {
    return undefined as T;
  }
  return (await response.json()) as T;
}

export const api = {
  session: () => request<OperatorSession>('/api/v1/operator/session'),
  login: (token: string) =>
    request<OperatorSession>('/api/v1/operator/session', {
      method: 'POST',
      body: JSON.stringify({ token })
    }),
  logout: () => request<void>('/api/v1/operator/session', { method: 'DELETE' }),
  bootstrap: () => request<Bootstrap>('/api/v1/bootstrap'),
  task: (id: string) => request<TaskDetail>(`/api/v1/operator/tasks/${id}`),
  createTask: (input: CreateTaskInput) =>
    request<Task>('/api/v1/tasks', { method: 'POST', body: JSON.stringify(input) }),
  updateTask: (id: string, input: UpdateTaskInput) =>
    request<Task>(`/api/v1/tasks/${id}`, { method: 'PATCH', body: JSON.stringify(input) }),
  moveTask: (id: string, status: TaskStatus) =>
    request<Task>(`/api/v1/tasks/${id}/move`, {
      method: 'POST',
      body: JSON.stringify({ status })
    }),
  agents: () => request<Agent[]>('/api/v1/agents'),
  answerDecision: (id: string, answer: string) =>
    request<Decision>(`/api/v1/operator/decisions/${id}/answer`, {
      method: 'POST',
      body: JSON.stringify({ answer })
    }),
  channels: (projectID = 'prj_default') =>
    request<{ items: Channel[] }>(`/api/v1/operator/channels?project_id=${encodeURIComponent(projectID)}`),
  createChannel: (name: string, projectID = 'prj_default') =>
    request<Channel>('/api/v1/operator/channels', {
      method: 'POST',
      body: JSON.stringify({ name, project_id: projectID })
    }),
  channelMessages: (channelID: string) =>
    request<{ items: ChannelMessage[] }>(`/api/v1/operator/channels/${channelID}/messages`),
  postChannelMessage: (channelID: string, body: string, references: CanonicalReferences = {}) =>
    request<ChannelMessage>(`/api/v1/operator/channels/${channelID}/messages`, {
      method: 'POST',
      body: JSON.stringify({ body, references })
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
