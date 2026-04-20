export interface ChatMessage {
  id: string;
  role: string;
  content: string;
  timestamp: string;
}

export interface ChatSession {
  id: string;
  messages: ChatMessage[];
  created_at: string;
  updated_at: string;
}

export interface ChatChunk {
  session_id: string;
  delta: string;
  done: boolean;
  error?: string;
}

export interface OllamaStatus {
  available: boolean;
  model_ready: boolean;
  error?: string;
}

export interface PullProgress {
  status: string;
  completed: number;
  total: number;
}

export function chatSessionTitle(session: ChatSession): string {
  const first = session.messages.find(m => m.role === 'user');
  if (!first) return 'New Chat';
  return first.content.length > 40
    ? first.content.slice(0, 40) + '…'
    : first.content;
}

export function relativeTime(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime();
  const mins = Math.floor(diff / 60000);
  if (mins < 1) return 'just now';
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.floor(mins / 60);
  if (hrs < 24) return `${hrs}h ago`;
  return `${Math.floor(hrs / 24)}d ago`;
}
