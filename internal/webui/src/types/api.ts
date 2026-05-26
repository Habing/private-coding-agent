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

export interface LLMTokenQuota {
  used: number
  cap: number
  enabled: boolean
  resets_at?: string
}

export interface QuotaResponse {
  llm_tokens: LLMTokenQuota
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
  /** Canonical tool name (normalized from tool_name on receive). */
  tool?: string
  tool_call_id?: string
  input?: unknown
  output?: unknown
  error?: string
  /** Backend wire fields (agent.Engine JSON). */
  tool_name?: string
  tool_input?: unknown
  tool_output?: unknown
  tool_error?: string
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

/** SSE step event from GET /admin/workflows/:slug/invoke/stream */
export interface WorkflowInvokeStepEvent {
  kind: 'step_start' | 'step_complete'
  step_id: string
  step_kind?: string
  tool?: string
  status?: 'ok' | 'error'
  error?: string
  output?: unknown
  ts?: string
}

export type WorkflowInvokeLiveStepPhase = 'running' | 'ok' | 'error'

export interface WorkflowInvokeLiveStep {
  stepId: string
  stepKind?: string
  tool?: string
  phase: WorkflowInvokeLiveStepPhase
  error?: string
  output?: unknown
}

export interface WorkflowTriggerRow {
  trigger_id: string
  kind: 'cron' | 'webhook'
  enabled: boolean
  cron_expr?: string
  timezone?: string
  webhook_url?: string
  webhook_token_suffix?: string
  next_run_at?: string | null
  last_run_at?: string | null
  last_status?: string
  last_error?: string
  default_inputs?: Record<string, unknown>
}

export interface WorkflowTriggersResponse {
  triggers: WorkflowTriggerRow[]
  webhook_base_url: string
}

export interface WorkflowTemplateSlot {
  name: string
  type: 'string' | 'object' | 'array'
  required: boolean
  description: string
  default?: unknown
  tool_picker?: 'notify' | 'forward'
  suggested_tools?: string[]
}

export interface WorkflowTemplate {
  id: string
  name: string
  description: string
  slots: WorkflowTemplateSlot[]
}

export interface WorkflowTemplateListResponse {
  templates: WorkflowTemplate[]
}

export interface WorkflowTemplatePreviewResponse {
  template_id: string
  slug: string
  name: string
  dsl_yaml: string
}

export type WorkflowProposalStatus =
  | 'draft'
  | 'pending_approval'
  | 'confirmed'
  | 'published'
  | 'rejected'

export interface WorkflowProposal {
  id: string
  tenant_id: string
  slug: string
  name: string
  description: string
  dsl_yaml: string
  source: string
  template_id?: string
  dry_run_ok: boolean
  dry_run_error?: string
  status: WorkflowProposalStatus
  created_at: string
  updated_at: string
}

export interface WorkflowProposalListResponse {
  proposals: WorkflowProposal[]
}

/** Slice 20 visual editor model */
export interface WorkflowDesign {
  id: string
  name: string
  description?: string
  inputs?: WorkflowDesignInput[]
  steps: WorkflowDesignStep[]
  outputs?: WorkflowDesignOutput[]
}

export interface WorkflowDesignInput {
  name: string
  type: string
  default?: unknown
  label?: string
  description?: string
  widget?: string
  options?: string[]
}

export interface WorkflowDesignOutput {
  name: string
  expr: string
}

export interface WorkflowDesignStep {
  id: string
  kind: 'tool' | 'assign' | 'if' | string
  tool?: string
  args?: WorkflowDesignArg[]
  assignments?: WorkflowDesignAssign[]
  condition?: WorkflowDesignCondition
  then?: WorkflowDesignStep[]
  else?: WorkflowDesignStep[]
}

export interface WorkflowDesignArg {
  name: string
  value: string
  valueKind: 'literal' | 'expr' | string
}

export interface WorkflowDesignAssign {
  var: string
  expr: string
  label?: string
}

export interface WorkflowDesignCondition {
  left: string
  op: string
  right: string
  rightKind?: string
}

export interface DesignCompileResponse {
  dsl_yaml: string
  warnings?: string[]
}

export interface DesignDecompileResponse {
  design: WorkflowDesign
  warnings?: string[]
}

export interface ToolSchemaEntry {
  name: string
  description: string
  parameters: Record<string, unknown>
  mutating: boolean
}

export interface ToolSchemasResponse {
  tools: ToolSchemaEntry[]
}

export type MemoryProposalStatus = 'pending' | 'approved' | 'auto_approved' | 'rejected'

export interface MemoryProposal {
  id: string
  tenant_id: string
  owner_user_id: string
  session_id?: string
  type: MemoryType
  content: string
  tags: string[]
  confidence: number
  status: MemoryProposalStatus
  memory_id?: string
  decided_at?: string
  decided_by?: string
  created_at: string
}

export interface MemoryProposalListResponse {
  proposals: MemoryProposal[]
}

export interface ApproveMemoryProposalRequest {
  type?: MemoryType
  content?: string
  tags?: string[]
}

export interface RejectMemoryProposalRequest {
  reason?: string
}

export interface McpToolSchema {
  name: string
  description?: string
  inputSchema: Record<string, unknown>
  annotations?: Record<string, unknown>
}

export interface McpServer {
  id: string
  tenant_id: string
  slug: string
  name: string
  description: string
  url: string
  transport: string
  auth_type: string
  auth_token?: string
  headers: Record<string, string>
  enabled: boolean
  last_seen_at?: string
  last_error: string
  tools_cache: McpToolSchema[]
  created_at: string
  updated_at: string
}

export interface McpServerListResponse {
  servers: McpServer[]
}

export interface CreateMcpServerRequest {
  slug: string
  name: string
  description?: string
  url: string
  auth_type?: string
  auth_token?: string
  headers?: Record<string, string>
  enabled?: boolean
}

export interface UpdateMcpServerRequest {
  name?: string
  description?: string
  url?: string
  auth_type?: string
  auth_token?: string
  headers?: Record<string, string>
  enabled?: boolean
}

export interface TestMcpConnectionRequest {
  url?: string
  auth_type?: string
  auth_token?: string
  headers?: Record<string, string>
}

export interface McpRefreshResponse {
  tools: McpToolSchema[]
}

export interface ConnectorRecipeStatus {
  id: string
  name: string
  description: string
  kind: 'mcp' | 'http_fetch'
  mcp_slug?: string
  suggest_tools: string[]
  setup_url_hint?: string
  docs_path?: string
  auth_type_default?: string
  installed: boolean
  server_id?: string
  enabled: boolean
  tools: string[]
  allow_hosts?: string[]
}

export interface HTTPFetchSettings {
  enabled: boolean
  allow_hosts: string[]
  block_private_ips: boolean
}

export interface ConnectorCatalogResponse {
  recipes: ConnectorRecipeStatus[]
}
