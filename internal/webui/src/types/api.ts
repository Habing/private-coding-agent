export type Role = 'user' | 'assistant' | 'tool' | 'system'

export interface User {
  id: string
  tenant_id: string
  email: string
  name: string
  role: string
}

export interface Session {
  id: string
  tenant_id: string
  owner_user_id: string
  title: string
  model: string
  profile: string
  status: 'active' | 'archived'
  created_at: string
  updated_at: string
}

export interface ToolCall {
  id: string
  type: 'function'
  function: { name: string; arguments: string }
}

export interface Message {
  id: string
  session_id: string
  seq: number
  role: Role
  content: string
  tool_call_id?: string
  tool_calls?: ToolCall[]
  metadata?: Record<string, unknown>
  created_at: string
}

export type AgentEventKind =
  | 'assistant_message'
  | 'tool_call'
  | 'tool_result'
  | 'final'
  | 'error'

export interface AgentEvent {
  kind: AgentEventKind
  step?: number
  text?: string
  tool?: string
  tool_call_id?: string
  input?: unknown
  output?: unknown
  error?: string
}

export type ServerFrame =
  | { type: 'event'; event: AgentEvent }
  | { type: 'done'; seq: number }
  | { type: 'error'; message: string }
  | { type: 'pong' }

export type ClientFrame =
  | { type: 'user_message'; content: string }
  | { type: 'ping' }

export interface LoginRequest {
  tenant: string
  email: string
  password: string
}

export interface LoginResponse {
  token: string
  user: User
}

export interface CreateSessionRequest {
  model: string
  profile?: string
  title?: string
}

export interface SessionListResponse {
  sessions: Session[]
}

export interface MessageListResponse {
  messages: Message[]
}
