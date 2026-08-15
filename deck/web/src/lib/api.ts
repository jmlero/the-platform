import type {
  Agent,
  AppEvent,
  Bootstrap,
  CreateTaskInput,
  Decision,
  Task,
  TaskDetail,
  TaskStatus,
  UpdateTaskInput
} from './types';

export class ApiError extends Error {
  constructor(
    message: string,
    public status: number
  ) {
    super(message);
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
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
    throw new ApiError(message, response.status);
  }
  if (response.status === 204) {
    return undefined as T;
  }
  return (await response.json()) as T;
}

export const api = {
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
    })
};

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
