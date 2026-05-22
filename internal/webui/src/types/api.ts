export type Role = 'user' | 'assistant' | 'tool' | 'system'

export interface User {
  id: string
  tenant_id: string
  email: string
  role: string
  name?: string
}

export interface MeResponse {
  user_id: string
  tenant_id: string
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
  sandbox_id?: string
  skill_ids?: string[]
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

// Backend AgentEventKind plus a frontend-only 'user' marker the WS hook
// pushes locally for optimistic echo of just-sent user messages. The server
// never emits 'user'.
export type AgentEventKind =
  | 'user'
  | 'assistant_delta'
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
  | { type: 'done'; seq?: number }
  | { type: 'error'; message: string; code?: string }
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

export interface AuditEntry {
  occurred_at: string
  tenant_id?: string
  user_id?: string
  action: string
  target: string
  method: string
  path: string
  status: number
  duration_ms: number
  metadata: Record<string, unknown> | null
}

export interface AuditListResponse {
  entries: AuditEntry[]
  total: number
  limit: number
  offset: number
}

export type MemoryType = 'profile' | 'preference' | 'knowledge' | 'lesson'

export interface Memory {
  id: string
  tenant_id: string
  user_id: string
  type: MemoryType
  content: string
  tags: string[]
  created_at: string
  updated_at: string
  last_used_at?: string
}

export interface MemoryListResponse {
  memories: Memory[]
}

export interface CreateMemoryRequest {
  type: MemoryType
  content: string
  tags?: string[]
}

export interface UpdateMemoryRequest {
  content?: string
  tags?: string[]
}

export interface SandboxFileEntry {
  name: string
  type: 'file' | 'dir'
  size?: number
}

export interface SandboxFileListResponse {
  entries: SandboxFileEntry[]
}

export interface SandboxFileReadResponse {
  content_base64: string
  size: number
}

export interface TenantSkill {
  id: string
  tenant_id: string
  skill_key: string
  description: string
  body: string
  content_hash: string
  enabled: boolean
  created_at: string
  updated_at: string
}

export interface TenantSkillListResponse {
  skills: TenantSkill[]
}

export interface CreateTenantSkillRequest {
  skill_key: string
  description?: string
  body: string
  enabled?: boolean
}

export interface UpdateTenantSkillRequest {
  description?: string
  body?: string
  enabled?: boolean
}

export interface ProfileSkillBinding {
  profile: string
  skill_keys: string[]
}

export interface ProfileInfo {
  name: string
  description: string
}

export interface ProfileListResponse {
  profiles: ProfileInfo[]
}

export interface ToolDef {
  name: string
  description: string
  parameters: unknown
  mutating: boolean
}

export interface ToolListResponse {
  tools: ToolDef[]
}

export interface Workflow {
  id: string
  tenant_id: string
  slug: string
  name: string
  description: string
  dsl_yaml?: string
  version: number
  published: boolean
  published_at?: string
  created_at: string
  updated_at: string
}

export interface WorkflowListResponse {
  workflows: Workflow[]
}

export type WorkflowRunStatus =
  | 'ok'
  | 'failed'
  | 'max_steps'
  | 'timeout'
  | 'cancelled'

export interface WorkflowRun {
  id: string
  tenant_id: string
  user_id: string
  workflow_id: string
  version_at_run: number
  dry_run: boolean
  status: WorkflowRunStatus
  inputs_json?: string
  outputs_json?: string
  error_text?: string
  duration_ms: number
  started_at: string
  finished_at?: string
}

export interface WorkflowRunListResponse {
  runs: WorkflowRun[]
}

export interface CreateWorkflowRequest {
  slug: string
  name: string
  description?: string
  dsl_yaml: string
}

export interface UpdateWorkflowRequest {
  name: string
  description?: string
  dsl_yaml: string
}

export interface InvokeWorkflowRequest {
  inputs?: Record<string, unknown>
  dry_run?: boolean
}

export interface WorkflowInvokeResult {
  run_id: string
  status: WorkflowRunStatus
  outputs?: Record<string, unknown>
  error?: string
  steps: number
  dry_run: boolean
  started_at: string
  duration_ms: number
}
