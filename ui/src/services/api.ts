import type { KnowledgeGraph, RepoInfo, SearchResult } from '../types/graph';
import { getBaseURL } from '../lib/config';

export interface SemanticSearchResult {
  id: string;
  name: string;
  kind: string;
  file: string;
  line: number;
  end_line?: number;
  score: number;
  qualified_name?: string;
  content?: string;
}

export interface ChatResponse {
  response: string;
  context?: SemanticSearchResult[];
}

// LLM Settings types
export type LLMProvider = 'openai' | 'anthropic' | 'ollama' | 'openrouter' | 'gemini';

export interface LLMSettings {
  provider: LLMProvider;
  apiKey?: string;
  baseUrl?: string;
  model: string;
  maxTokens: number;
  enableSemantic: boolean;
}

export interface ModelInfo {
  id: string;
  name: string;
  provider: LLMProvider;
}

// LLM Settings API
export async function fetchSettings(): Promise<LLMSettings> {
  const response = await fetch(`${getBaseURL()}/api/settings`);
  if (!response.ok) throw new Error('Failed to fetch settings');
  return response.json();
}

export async function updateSettings(settings: Partial<LLMSettings>): Promise<LLMSettings> {
  const response = await fetch(`${getBaseURL()}/api/settings`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(settings),
  });
  if (!response.ok) throw new Error('Failed to update settings');
  return response.json();
}

export async function fetchAvailableModels(): Promise<ModelInfo[]> {
  const response = await fetch(`${getBaseURL()}/api/settings/models`);
  if (!response.ok) throw new Error('Failed to fetch models');
  return response.json();
}

// Stream chat with callback
export async function chatStream(
  message: string,
  onChunk: (chunk: string) => void,
  options?: {
    context?: string;
    graphData?: KnowledgeGraph;
    useSemantic?: boolean;
    repo?: string;
  }
): Promise<void> {
  const body: {
    message: string;
    context?: string;
    graphData?: { nodes: unknown[]; relationships: unknown[] };
    useSemantic?: boolean;
    repo?: string;
  } = { message };

  if (options?.context) body.context = options.context;
  if (options?.graphData) {
    body.graphData = {
      nodes: options.graphData.nodes.slice(0, 50),
      relationships: options.graphData.relationships.slice(0, 50),
    };
  }
  if (options?.useSemantic) body.useSemantic = true;
  if (options?.repo) body.repo = options.repo;

  const response = await fetch(`${getBaseURL()}/api/chat/stream`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });

  if (!response.ok) throw new Error('Stream chat failed');

  const reader = response.body?.getReader();
  if (!reader) throw new Error('No reader available');

  const decoder = new TextDecoder();

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;

    const chunk = decoder.decode(value, { stream: true });
    const lines = chunk.split('\n');

    for (const line of lines) {
      if (line.startsWith('data: ')) {
        const data = line.slice(6);
        if (data === '[DONE]') return;
        try {
          const parsed = JSON.parse(data);
          if (parsed.content) {
            onChunk(parsed.content);
          }
        } catch {
          // Ignore parse errors for incomplete chunks
        }
      }
    }
  }
}

export async function fetchRepos(): Promise<RepoInfo[]> {
  const response = await fetch(`${getBaseURL()}/api/repos`);
  if (!response.ok) throw new Error('Failed to fetch repos');
  return response.json();
}

export interface FetchGraphOptions {
  repo?: string;
  projectId?: string;
  limit?: number;
  filterConnected?: boolean;
  includeStats?: boolean;
  // Level presets for hierarchical loading
  level?: 'structure' | 'class' | 'function' | 'full';
  // Custom node type filters
  nodeTypes?: string[];
  excludeNodeTypes?: string[];
  // Custom edge type filters
  edgeTypes?: string[];
  excludeEdgeTypes?: string[];
}

export async function fetchGraph(options?: FetchGraphOptions): Promise<KnowledgeGraph> {
  let url = `${getBaseURL()}/api/graph`;
  const params = new URLSearchParams();

  if (options?.repo) params.append('repo', options.repo);
  if (options?.projectId) params.append('project_id', String(options.projectId));
  if (options?.limit) params.append('limit', String(options.limit));
  if (options?.filterConnected !== undefined) {
    params.append('filter_connected', options.filterConnected ? 'true' : 'false');
  }
  if (options?.includeStats) params.append('include_stats', 'true');
  if (options?.level) params.append('level', options.level);
  if (options?.nodeTypes?.length) params.append('node_types', options.nodeTypes.join(','));
  if (options?.excludeNodeTypes?.length) params.append('exclude_node_types', options.excludeNodeTypes.join(','));
  if (options?.edgeTypes?.length) params.append('edge_types', options.edgeTypes.join(','));
  if (options?.excludeEdgeTypes?.length) params.append('exclude_edge_types', options.excludeEdgeTypes.join(','));

  if (params.toString()) url += '?' + params.toString();
  const response = await fetch(url);
  if (!response.ok) throw new Error('Failed to fetch graph');
  return response.json();
}

// Node neighbor API for on-demand edge loading
export interface FetchNodeNeighborsOptions {
  projectId?: string;
  depth?: number; // 1-3, default 1
  edgeTypes?: string[];
  excludeEdgeTypes?: string[];
  direction?: 'incoming' | 'outgoing' | 'both'; // default 'both'
}

export async function fetchNodeNeighbors(nodeId: string, options?: FetchNodeNeighborsOptions): Promise<KnowledgeGraph> {
  let url = `${getBaseURL()}/api/nodes/${nodeId}/neighbors`;
  const params = new URLSearchParams();

  if (options?.projectId) params.append('project_id', String(options.projectId));
  if (options?.depth) params.append('depth', String(options.depth));
  if (options?.edgeTypes?.length) params.append('edge_types', options.edgeTypes.join(','));
  if (options?.excludeEdgeTypes?.length) params.append('exclude_edge_types', options.excludeEdgeTypes.join(','));
  if (options?.direction) params.append('direction', options.direction);

  if (params.toString()) url += '?' + params.toString();
  const response = await fetch(url);
  if (!response.ok) throw new Error('Failed to fetch node neighbors');
  return response.json();
}

// Graph delta types for incremental updates
export interface GraphDeltaResponse {
  added_nodes: KnowledgeGraph['nodes'];
  added_edges: KnowledgeGraph['relationships'];
  removed_node_ids: string[];
  removed_edge_ids: string[];
  is_full_rebuild: boolean;
}

export interface FetchGraphDeltaOptions {
  projectId: string;
  changedFiles: string[];
  removedFiles: string[];
}

export async function fetchGraphDelta(options: FetchGraphDeltaOptions): Promise<GraphDeltaResponse> {
  const response = await fetch(`${getBaseURL()}/api/graph/delta`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      project_id: options.projectId,
      changed_files: options.changedFiles,
      removed_files: options.removedFiles,
    }),
  });
  if (!response.ok) throw new Error('Failed to fetch graph delta');
  return response.json();
}

// Drill-down API — fetch a node's sub-graph on demand
export interface FetchGraphDrilldownOptions {
  projectId: string;
  nodeId?: string;
  qualifiedName?: string;
  hops?: number;
  level?: string; // 'structure' | 'class' | 'function' | 'full'
}

export async function fetchGraphDrilldown(options: FetchGraphDrilldownOptions): Promise<KnowledgeGraph> {
  const params = new URLSearchParams();
  if (options.projectId) params.set('project_id', options.projectId);
  if (options.nodeId) params.set('node_id', options.nodeId);
  if (options.qualifiedName) params.set('qualified_name', options.qualifiedName);
  if (options.hops) params.set('hops', String(options.hops));
  if (options.level) params.set('level', options.level);

  const response = await fetch(`${getBaseURL()}/api/graph/drilldown?${params}`);
  if (!response.ok) throw new Error('Failed to fetch drilldown graph');
  return response.json();
}

// Project types
export interface Project {
  id: string;
  name: string;
  root_path: string;
  watch_enabled: boolean;
  watch_status: string;
  language_stack: string[];
  created_at: string;
  updated_at: string;
}

export interface ProjectStats {
  node_count: number;
  edge_count: number;
  file_count: number;
}

// Project API
export async function fetchProjects(): Promise<{ projects: Project[]; count: number }> {
  const response = await fetch(`${getBaseURL()}/v1/projects`);
  if (!response.ok) throw new Error('Failed to fetch projects');
  return response.json();
}

export async function createProject(name: string, rootPath: string): Promise<Project> {
  const response = await fetch(`${getBaseURL()}/v1/projects`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, root_path: rootPath }),
  });
  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.message || 'Failed to create project');
  }
  return response.json();
}

export async function deleteProject(id: string): Promise<void> {
  const response = await fetch(`${getBaseURL()}/v1/projects/${id}`, {
    method: 'DELETE',
  });
  if (!response.ok) throw new Error('Failed to delete project');
}

export async function fetchProjectStats(id: string): Promise<ProjectStats> {
  const response = await fetch(`${getBaseURL()}/v1/projects/${id}/stats`);
  if (!response.ok) throw new Error('Failed to fetch project stats');
  return response.json();
}

// Project build status — used to restore UI state after page refresh or project switch
export interface ProjectBuildStatus {
  is_building: boolean;
  task_id?: string;
  progress?: number;
  phase?: string;
  message?: string;
}

export async function fetchProjectBuildStatus(projectId: string): Promise<ProjectBuildStatus> {
  const response = await fetch(`${getBaseURL()}/v1/projects/${projectId}/build-status`);
  if (!response.ok) throw new Error('Failed to fetch project build status');
  return response.json();
}

// New project creation request
export interface NewProjectRequest {
  name: string;        // Project name
  location: string;    // Parent directory where project will be created
  language: string;    // Language template (go, javascript, python, etc.)
  init_git: boolean;   // Whether to initialize git repository
}

export interface NewProjectResponse {
  id: string;
  name: string;
  root_path: string;
  language: string;
  git_init: boolean;
  created_at: string;
}

// Create a new project with template files
export async function newProject(request: NewProjectRequest): Promise<NewProjectResponse> {
  const response = await fetch(`${getBaseURL()}/v1/projects-new`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(request),
  });
  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.message || 'Failed to create new project');
  }
  return response.json();
}

// Clone API types
export interface CloneRequest {
  remote_url: string;
  branch?: string;
  clone_mode?: 'managed' | 'custom';
  workspace?: string;
  watch_enabled?: boolean;
}

export interface CloneResponse {
  success: boolean;
  local_path: string;
  name: string;
  provider: string;
  managed: boolean;
  branch: string;
  message?: string;
  error?: string;
}

// Clone remote repository
export async function cloneRepo(request: CloneRequest): Promise<CloneResponse> {
  const response = await fetch(`${getBaseURL()}/api/clone`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(request),
  });
  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.message || error.error || 'Failed to clone repository');
  }
  const result: CloneResponse = await response.json();
  // Check success flag - backend returns 200 OK with success: false on error
  if (!result.success) {
    throw new Error(result.error || 'Failed to clone repository');
  }
  return result;
}

export interface SearchOptions {
  mode?: 'keyword' | 'semantic' | 'hybrid';
  minScore?: number;
  kind?: string;
  file?: string;
  noTests?: boolean;
  projectId?: string;
}

export async function searchCode(
  query: string,
  limit: number = 20,
  repo?: string,
  options?: SearchOptions
): Promise<{ results: SearchResult[]; count: number; message?: string }> {
  const body: {
    query: string;
    limit: number;
    repo?: string;
    mode?: string;
    minScore?: number;
    kind?: string;
    file?: string;
    noTests?: boolean;
    projectId?: string;
  } = { query, limit };
  if (repo) body.repo = repo;
  if (options?.mode) body.mode = options.mode;
  if (options?.minScore) body.minScore = options.minScore;
  if (options?.kind) body.kind = options.kind;
  if (options?.file) body.file = options.file;
  if (options?.noTests) body.noTests = options.noTests;
  if (options?.projectId) body.projectId = options.projectId;

  const response = await fetch(`${getBaseURL()}/api/search`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!response.ok) throw new Error('Search failed');
  return response.json();
}

export async function semanticSearch(query: string, limit: number = 10, projectId?: string): Promise<{ results: SemanticSearchResult[]; count: number; message?: string }> {
  const body: { query: string; limit: number; project_id?: string } = { query, limit };
  if (projectId) body.project_id = projectId;

  const response = await fetch(`${getBaseURL()}/v1/semantic-search`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!response.ok) throw new Error('Semantic search failed');
  return response.json();
}

export async function chat(message: string, context?: string, graphData?: KnowledgeGraph, useSemantic: boolean = false, repo?: string): Promise<ChatResponse> {
  const body: { message: string; context?: string; graphData?: { nodes: unknown[]; relationships: unknown[] }; useSemantic?: boolean; repo?: string } = { message };
  if (context) body.context = context;
  if (graphData) body.graphData = { nodes: graphData.nodes.slice(0, 50), relationships: graphData.relationships.slice(0, 50) };
  if (useSemantic) body.useSemantic = true;
  if (repo) body.repo = repo;

  const response = await fetch(`${getBaseURL()}/api/chat`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!response.ok) throw new Error('Chat failed');
  return response.json();
}

export async function fetchFile(path: string, repo?: string): Promise<string> {
  let url = `${getBaseURL()}/api/file?path=${encodeURIComponent(path)}`;
  if (repo) url += `&repo=${encodeURIComponent(repo)}`;
  
  const response = await fetch(url);
  if (!response.ok) throw new Error('Failed to fetch file');
  const data = await response.json();
  return data.content;
}

export async function queryCypher(cypher: string, repo?: string): Promise<Record<string, unknown>[]> {
  const body: { cypher: string; repo?: string } = { cypher };
  if (repo) body.repo = repo;
  
  const response = await fetch(`${getBaseURL()}/api/query`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!response.ok) throw new Error('Query failed');
  const data = await response.json();
  return data.result;
}

export async function fetchRepoInfo(repo?: string): Promise<RepoInfo> {
  let url = `${getBaseURL()}/api/repo`;
  if (repo) url += `?repo=${encodeURIComponent(repo)}`;
  
  const response = await fetch(url);
  if (!response.ok) throw new Error('Failed to fetch repo info');
  return response.json();
}

// Build API
export interface BuildRequest {
  root_dir: string;
  full_build?: boolean;
  exclude_patterns?: string[];
  include_dataflow?: boolean;
  include_ast?: boolean;
  project_id?: string;
}

export interface BuildTask {
  task_id: string;
  status: string;
}

export interface TaskStatus {
  id: string;
  type: string;
  status: 'pending' | 'running' | 'complete' | 'error' | 'canceled';
  progress: number;
  message: string;
  result?: Record<string, unknown>;
  error?: string;
  created_at: string;
  updated_at: string;
}

export async function startBuild(request: BuildRequest): Promise<BuildTask> {
  const response = await fetch(`${getBaseURL()}/v1/build`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(request),
  });
  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.message || 'Failed to start build');
  }
  return response.json();
}

export async function fetchTaskStatus(taskId: string): Promise<TaskStatus> {
  const response = await fetch(`${getBaseURL()}/v1/tasks/${taskId}`);
  if (!response.ok) throw new Error('Failed to fetch task status');
  return response.json();
}

// Watch types
export interface WatchStatus {
  project_id: string;
  watch_enabled: boolean;
  watch_status: string;
  is_running: boolean;
  start_time?: string;
  last_error?: string;
  changes_queued: number;
}

export interface WatchStartResponse {
  status: string;
  project_id: string;
  root_dir: string;
  start_time?: string;
}

export interface WatchStopResponse {
  status: string;
  project_id: string;
  root_dir?: string;
}

export interface RestoreWatchResponse {
  restored: string[];
  failed: Array<{ project_id: string; error: string }>;
  count: number;
}

// Watch API - Project-level watch operations
export async function startProjectWatch(projectId: string): Promise<WatchStartResponse> {
  const response = await fetch(`${getBaseURL()}/v1/projects/${projectId}/watch/start`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
  });
  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.message || 'Failed to start project watch');
  }
  return response.json();
}

export async function stopProjectWatch(projectId: string): Promise<WatchStopResponse> {
  const response = await fetch(`${getBaseURL()}/v1/projects/${projectId}/watch/stop`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
  });
  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.message || 'Failed to stop project watch');
  }
  return response.json();
}

export async function fetchProjectWatchStatus(projectId: string): Promise<WatchStatus> {
  const response = await fetch(`${getBaseURL()}/v1/projects/${projectId}/watch/status`);
  if (!response.ok) throw new Error('Failed to fetch watch status');
  return response.json();
}

// Embedding API
export interface EmbedStatusResult {
  configured: boolean;
  status: string;
  embedding_count: number;
  code_embedding_count: number;
  model: string;
  needs_reembedding: boolean;
  is_configured: boolean;
}

export async function fetchEmbedStatus(projectId?: string): Promise<EmbedStatusResult> {
  const url = projectId
    ? `${getBaseURL()}/v1/embed/status?project_id=${encodeURIComponent(projectId)}`
    : `${getBaseURL()}/v1/embed/status`;
  const response = await fetch(url);
  if (!response.ok) throw new Error('Failed to fetch embed status');
  return response.json();
}

export async function triggerEmbed(options: {
  strategy?: 'incremental' | 'full';
  projectId?: string;
  rootDir?: string;
}): Promise<{ task_id: string; status: string }> {
  const body: Record<string, string> = {
    strategy: options.strategy || 'incremental',
  };
  if (options.projectId) body.project_id = options.projectId;
  if (options.rootDir) body.root_dir = options.rootDir;

  const response = await fetch(`${getBaseURL()}/v1/embed`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.message || 'Failed to trigger embedding');
  }
  return response.json();
}

export async function restoreAllWatches(): Promise<RestoreWatchResponse> {
  const response = await fetch(`${getBaseURL()}/v1/watch/restore`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
  });
  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.message || 'Failed to restore watches');
  }
  return response.json();
}

// ============================================================
// Agent Profile API
// ============================================================

export interface AgentProfile {
  id: string;
  name: string;
  description: string;
  icon: string;
  tools: string[];
  system_prompt: string;
  is_builtin: boolean;
  allow_write: boolean;
  created_at?: string;
}

export interface AgentTool {
  name: string;
  description: string;
}

export async function fetchAgents(): Promise<{ agents: AgentProfile[]; count: number }> {
  const response = await fetch(`${getBaseURL()}/api/agents`);
  if (!response.ok) throw new Error('Failed to fetch agents');
  return response.json();
}

export async function fetchAgentTools(): Promise<{ tools: AgentTool[]; count: number }> {
  const response = await fetch(`${getBaseURL()}/api/agent-tools`);
  if (!response.ok) throw new Error('Failed to fetch agent tools');
  return response.json();
}

export async function createAgent(data: {
  name: string;
  description: string;
  icon: string;
  tools: string[];
  system_prompt: string;
  allow_write: boolean;
}): Promise<{ id: string; status: string }> {
  const response = await fetch(`${getBaseURL()}/api/agents`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data),
  });
  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.message || 'Failed to create agent');
  }
  return response.json();
}

export async function updateAgent(id: string, data: {
  name: string;
  description: string;
  icon: string;
  tools: string[];
  system_prompt: string;
  allow_write: boolean;
}): Promise<void> {
  const response = await fetch(`${getBaseURL()}/api/agents/${id}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data),
  });
  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.message || 'Failed to update agent');
  }
}

export async function deleteAgent(id: string): Promise<void> {
  const response = await fetch(`${getBaseURL()}/api/agents/${id}`, {
    method: 'DELETE',
  });
  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.message || 'Failed to delete agent');
  }
}

// Chat Session API types
export interface ChatSession {
  session_id: string;
  agent_id?: string; // Agent ID for this session
  message_count: number;
  created_at: string;
  updated_at: string;
  messages?: ChatMessage[]; // Messages included in list response
}

export interface ChatMessage {
  role: string;
  content: string;
  name?: string;
  created_at?: string;
}

// List chat sessions for a project
export async function listChatSessions(projectId?: string): Promise<ChatSession[]> {
  const url = projectId
    ? `${getBaseURL()}/api/chat/sessions?project_id=${projectId}`
    : `${getBaseURL()}/api/chat/sessions`;
  const response = await fetch(url);
  if (!response.ok) {
    throw new Error('Failed to list chat sessions');
  }
  const data = await response.json();
  return data.sessions || [];
}

// Get chat session history
export async function getChatSessionHistory(sessionId: string): Promise<ChatMessage[]> {
  const response = await fetch(`${getBaseURL()}/api/chat/sessions/${sessionId}/history`);
  if (!response.ok) {
    throw new Error('Failed to get session history');
  }
  const data = await response.json();
  return data.messages || [];
}

// ============================================================
// App state API — fetches project list + active_project_id atomically.
// This avoids the React render window that existed when the two were fetched
// separately (projects.length > 0 but currentProject still null → DropZone flash).
// ============================================================

export interface AppState {
  projects: Project[];
  count: number;
  active_project_id: string;
}

/** Fetch project list and active project id in a single request. */
export async function fetchAppState(): Promise<AppState> {
  const response = await fetch(`${getBaseURL()}/v1/app/state`);
  if (!response.ok) throw new Error('Failed to fetch app state');
  return response.json();
}

/**
 * Persist the currently selected project id on the server so that
 * GET /v1/app/state returns it on the next page load.
 * Fire-and-forget safe — errors are swallowed.
 */
export async function setActiveProject(projectId: string): Promise<void> {
  try {
    await fetch(`${getBaseURL()}/v1/app/state/active-project`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ project_id: projectId }),
    });
  } catch {
    // Best-effort
  }
}

// ============================================================
// File Tree Operations API  (/api/filetree/*)
// ============================================================

export interface FileTreeEntry {
  name: string;
  path: string;       // relative to project root
  abs_path: string;   // absolute path on disk
  is_dir: boolean;
  size?: number;
  mod_time?: string;  // omitted in lite mode
  children?: FileTreeEntry[];
}

/** List a directory. Pass recursive=true to get the full subtree.
 *  lite=true skips size/mod_time for faster response (less disk I/O on backend). */
export async function listFileTree(
  projectId: string,
  path = '.',
  recursive = false,
  lite = false,
): Promise<{ path: string; entries: FileTreeEntry[]; count: number }> {
  const params = new URLSearchParams({ project_id: projectId, path, recursive: String(recursive), lite: String(lite) });
  const response = await fetch(`${getBaseURL()}/api/filetree?${params}`);
  if (!response.ok) {
    const err = await response.json().catch(() => ({}));
    const error = new Error(err.message || 'Failed to list file tree');
    (error as any).status = response.status;
    (error as any).code = err.code;
    throw error;
  }
  return response.json();
}

/** Create a new empty file. Returns the response including entry for incremental update. */
export async function createFile(path: string, projectId: string, content = ''): Promise<{
  message: string; path: string; abs_path: string; entry: FileTreeEntry;
}> {
  const response = await fetch(`${getBaseURL()}/api/filetree/file`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path, content, project_id: projectId }),
  });
  if (!response.ok) {
    const err = await response.json().catch(() => ({}));
    throw new Error(err.message || 'Failed to create file');
  }
  return response.json();
}

/** Delete a file (not a folder) */
export async function deleteFile(path: string, projectId: string): Promise<void> {
  const params = new URLSearchParams({ path, project_id: projectId });
  const response = await fetch(`${getBaseURL()}/api/filetree/file?${params}`, { method: 'DELETE' });
  if (!response.ok) {
    const err = await response.json().catch(() => ({}));
    throw new Error(err.message || 'Failed to delete file');
  }
}

/** Create a new folder. Returns the response including entry for incremental update. */
export async function createFolder(path: string, projectId: string): Promise<{
  message: string; path: string; abs_path: string; entry: FileTreeEntry;
}> {
  const response = await fetch(`${getBaseURL()}/api/filetree/folder`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path, project_id: projectId }),
  });
  if (!response.ok) {
    const err = await response.json().catch(() => ({}));
    throw new Error(err.message || 'Failed to create folder');
  }
  return response.json();
}

/** Delete a folder recursively */
export async function deleteFolder(path: string, projectId: string): Promise<void> {
  const params = new URLSearchParams({ path, project_id: projectId });
  const response = await fetch(`${getBaseURL()}/api/filetree/folder?${params}`, { method: 'DELETE' });
  if (!response.ok) {
    const err = await response.json().catch(() => ({}));
    throw new Error(err.message || 'Failed to delete folder');
  }
}

/** Copy a file or folder to a new destination. Returns the response including entry for incremental update.
 *  If dstPath is an existing directory, the entry is copied *into* it (keeping original name).
 *  If dstPath doesn't exist, it is treated as the new name/path. */
export async function copyEntry(srcPath: string, dstPath: string, projectId: string): Promise<{
  message: string; src_path: string; dst_path: string; entry: FileTreeEntry;
}> {
  const response = await fetch(`${getBaseURL()}/api/filetree/copy`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ src_path: srcPath, dst_path: dstPath, project_id: projectId }),
  });
  if (!response.ok) {
    const err = await response.json().catch(() => ({}));
    throw new Error(err.message || 'Failed to copy');
  }
  return response.json();
}

/** Rename or move a file or folder. Returns the response including entry for incremental update. */
export async function renameEntry(oldPath: string, newPath: string, projectId: string): Promise<{
  message: string; old_path: string; new_path: string; entry: FileTreeEntry;
}> {
  const response = await fetch(`${getBaseURL()}/api/filetree/rename`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ old_path: oldPath, new_path: newPath, project_id: projectId }),
  });
  if (!response.ok) {
    const err = await response.json().catch(() => ({}));
    throw new Error(err.message || 'Failed to rename');
  }
  return response.json();
}

/** Stat a single path — check existence and get metadata (for undo preValidate) */
export async function statEntry(path: string, projectId: string): Promise<{
  exists: boolean; is_dir?: boolean; size?: number; mod_time?: string; abs_path?: string;
}> {
  const params = new URLSearchParams({ path, project_id: projectId });
  const response = await fetch(`${getBaseURL()}/api/filetree/stat?${params}`);
  if (!response.ok) return { exists: false };
  return response.json();
}

// ===== Plugin API =====

export interface PluginInfo {
  id: string;
  name: string;
  version: string;
  description: string;
  author: string;
  icon: string;
  category: string;
  status: string;
  endpoint: string;
  port: number;
  frontend?: any;
  backend?: any;
}

/** Fetch list of installed plugins */
export async function fetchPlugins(): Promise<PluginInfo[]> {
  const response = await fetch(`${getBaseURL()}/v1/plugins`);
  if (!response.ok) throw new Error('Failed to fetch plugins');
  return response.json();
}

/** Scan plugin directory for new plugins */
export async function scanPlugins(): Promise<void> {
  await fetch(`${getBaseURL()}/v1/plugins/scan`, { method: 'POST' });
}

/** Install a plugin (run install script asynchronously) */
export async function installPlugin(pluginId: string): Promise<void> {
  const response = await fetch(`${getBaseURL()}/v1/plugins/${pluginId}/install`, { method: 'POST' });
  if (!response.ok) {
    const err = await response.json().catch(() => ({}));
    throw new Error(err.message || 'Failed to install plugin');
  }
  // Backend returns 202 Accepted — install runs asynchronously.
  // Listen for 'plugin.installed' or 'plugin.installFailed' SSE events for the result.
}

/** Start a plugin */
export async function startPlugin(pluginId: string): Promise<void> {
  const response = await fetch(`${getBaseURL()}/v1/plugins/${pluginId}/start`, { method: 'POST' });
  if (!response.ok) {
    const err = await response.json().catch(() => ({}));
    throw new Error(err.message || 'Failed to start plugin');
  }
}

/** Stop a plugin */
export async function stopPlugin(pluginId: string): Promise<void> {
  const response = await fetch(`${getBaseURL()}/v1/plugins/${pluginId}/stop`, { method: 'POST' });
  if (!response.ok) {
    const err = await response.json().catch(() => ({}));
    throw new Error(err.message || 'Failed to stop plugin');
  }
}

/** Uninstall a plugin */
export async function uninstallPlugin(pluginId: string, argValues: Record<string, boolean> = {}): Promise<void> {
  const params = new URLSearchParams();
  for (const [name, value] of Object.entries(argValues)) {
    if (value) {
      params.set(`args.${name}`, 'true');
    }
  }
  const qs = params.toString();
  const url = `${getBaseURL()}/v1/plugins/${pluginId}${qs ? '?' + qs : ''}`;
  const response = await fetch(url, { method: 'DELETE' });
  if (!response.ok) {
    const err = await response.json().catch(() => ({}));
    throw new Error(err.message || 'Failed to uninstall plugin');
  }
}

/** Import a plugin from a .tar.gz archive file */
export async function importPlugin(file: File): Promise<void> {
  const formData = new FormData();
  formData.append('file', file);
  const response = await fetch(`${getBaseURL()}/v1/plugins/import`, {
    method: 'POST',
    body: formData,
  });
  if (!response.ok) {
    const err = await response.json().catch(() => ({}));
    throw new Error(err.message || 'Failed to import plugin');
  }
}

// ============================================================
// Notification API
// ============================================================

export interface NotificationAction {
  id: string;
  label: string;
  url: string;
}

export interface Notification {
  id: string;
  source: string;
  type: 'info' | 'warning' | 'error' | 'success' | 'progress';
  title: string;
  message: string;
  group: string;
  actions: NotificationAction[];
  read: boolean;
  timestamp: string;
}

export interface NotificationListResponse {
  notifications: Notification[];
  total: number;
  unreadCount: number;
}

/** Fetch notification list */
export async function fetchNotifications(options?: {
  unread?: boolean;
  source?: string;
  type?: string;
  limit?: number;
  offset?: number;
}): Promise<NotificationListResponse> {
  const params = new URLSearchParams();
  if (options?.unread) params.set('unread', 'true');
  if (options?.source) params.set('source', options.source);
  if (options?.type) params.set('type', options.type);
  if (options?.limit) params.set('limit', String(options.limit));
  if (options?.offset) params.set('offset', String(options.offset));
  const qs = params.toString();
  const url = `${getBaseURL()}/v1/notifications${qs ? '?' + qs : ''}`;
  const response = await fetch(url);
  if (!response.ok) throw new Error('Failed to fetch notifications');
  return response.json();
}

/** Create a notification (host frontend scenario, generally not used — notifications should originate from backend) */
export async function createNotification(n: Partial<Notification>): Promise<Notification> {
  const response = await fetch(`${getBaseURL()}/v1/notifications`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(n),
  });
  if (!response.ok) throw new Error('Failed to create notification');
  return response.json();
}

/** Mark a notification as read */
export async function markNotificationRead(id: string): Promise<void> {
  const response = await fetch(`${getBaseURL()}/v1/notifications/${id}/read`, {
    method: 'PUT',
  });
  if (!response.ok) throw new Error('Failed to mark notification as read');
}

/** Mark all notifications as read */
export async function markAllNotificationsRead(): Promise<void> {
  const response = await fetch(`${getBaseURL()}/v1/notifications/read-all`, {
    method: 'PUT',
  });
  if (!response.ok) throw new Error('Failed to mark all notifications as read');
}

/** Delete a notification */
export async function deleteNotification(id: string): Promise<void> {
  const response = await fetch(`${getBaseURL()}/v1/notifications/${id}`, {
    method: 'DELETE',
  });
  if (!response.ok) throw new Error('Failed to delete notification');
}

/** Get unread notification count */
export async function fetchUnreadCount(): Promise<{ count: number }> {
  const response = await fetch(`${getBaseURL()}/v1/notifications/unread-count`);
  if (!response.ok) throw new Error('Failed to fetch unread count');
  return response.json();
}

// ─── Persistent State API ─────────────────────────────────────────────────────

/**
 * Get file tree expanded paths from backend persistence.
 * projectId is used to scope the stored paths per project.
 */
export async function getFileTreeExpandedPaths(projectId?: string): Promise<string[]> {
  try {
    const params = projectId ? `?project_id=${encodeURIComponent(projectId)}` : '';
    const response = await fetch(`${getBaseURL()}/v1/app/state/file-tree${params}`);
    if (!response.ok) throw new Error('Failed to fetch file tree state');
    const data = await response.json();
    return data.expanded_paths || [];
  } catch {
    return [];
  }
}

/**
 * Save file tree expanded paths to backend persistence.
 * projectId is used to scope the stored paths per project.
 */
export async function setFileTreeExpandedPaths(paths: string[], projectId?: string): Promise<void> {
  const params = projectId ? `?project_id=${encodeURIComponent(projectId)}` : '';
  const response = await fetch(`${getBaseURL()}/v1/app/state/file-tree${params}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ expanded_paths: paths }),
  });
  if (!response.ok) throw new Error('Failed to save file tree state');
}