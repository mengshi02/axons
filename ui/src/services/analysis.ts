/**
 * Analysis API service - provides calls for all new analysis endpoints.
 */

import { getBaseURL } from '../lib/config';

// ─── Shared Types ──────────────────────────────────────────────────────────

export interface NodeRef {
  id: number;
  name: string;
  kind: string;
  file: string;
  line?: number;
}

// ─── Hotspots ──────────────────────────────────────────────────────────────

export interface HotspotItem {
  id: number;
  name: string;
  kind: string;
  file: string;
  line: number;
  fan_in: number;
  fan_out: number;
  score: number;
  exported: boolean;
}

export interface HotspotsResponse {
  hotspots: HotspotItem[];
  count: number;
}

export async function fetchHotspots(limit = 20, projectId?: string): Promise<HotspotsResponse> {
  let url = `${getBaseURL()}/v1/analysis/hotspots?limit=${limit}`;
  if (projectId) url += `&project_id=${projectId}`;
  const res = await fetch(url);
  if (!res.ok) throw new Error('Failed to fetch hotspots');
  return res.json();
}

// ─── Dead Code ─────────────────────────────────────────────────────────────

export interface DeadCodeItem {
  id: number;
  name: string;
  kind: string;
  file: string;
  line: number;
  exported: boolean;
}

export interface DeadCodeResponse {
  dead_nodes: DeadCodeItem[];
  unused_exports: DeadCodeItem[];
  count: number;
}

export async function fetchDeadCode(projectId?: string): Promise<DeadCodeResponse> {
  let url = `${getBaseURL()}/v1/analysis/deadcode`;
  if (projectId) url += `?project_id=${projectId}`;
  const res = await fetch(url);
  if (!res.ok) throw new Error('Failed to fetch dead code');
  return res.json();
}

// ─── Co-Change ─────────────────────────────────────────────────────────────

export interface CoChangeItem {
  file_a: string;
  file_b: string;
  commit_count: number;
  jaccard: number;
}

export interface CoChangeResponse {
  co_changes: CoChangeItem[];
  count: number;
}

export async function fetchCoChanges(limit = 50, minCount = 3, file?: string, projectId?: string): Promise<CoChangeResponse> {
  let url = `${getBaseURL()}/v1/analysis/cochange?limit=${limit}&min_count=${minCount}`;
  if (file) url += `&file=${encodeURIComponent(file)}`;
  if (projectId) url += `&project_id=${projectId}`;
  const res = await fetch(url);
  if (!res.ok) throw new Error('Failed to fetch co-changes');
  return res.json();
}

// ─── Graph Metrics ─────────────────────────────────────────────────────────

export interface GraphMetrics {
  total_nodes: number;
  total_edges: number;
  avg_in_degree: number;
  avg_out_degree: number;
  max_in_degree: number;
  max_out_degree: number;
  density: number;
  num_sccs: number;
  largest_scc_size: number;
  num_communities: number;
  modularity: number;
  is_dag: boolean;
}

export async function fetchGraphMetrics(projectId?: string): Promise<GraphMetrics> {
  let url = `${getBaseURL()}/v1/graph/metrics`;
  if (projectId) url += `?project_id=${projectId}`;
  const res = await fetch(url);
  if (!res.ok) throw new Error('Failed to fetch graph metrics');
  return res.json();
}

// ─── Communities ───────────────────────────────────────────────────────────

export interface CommunityNode {
  id: number;
  name: string;
  kind: string;
  file: string;
}

export interface Community {
  id: number;
  nodes: CommunityNode[];
  size: number;
  density: number;
}

export interface CommunitiesResponse {
  communities: Community[];
  count: number;
}

export async function fetchCommunities(resolution = 1.0, projectId?: string): Promise<CommunitiesResponse> {
  let url = `${getBaseURL()}/v1/graph/communities?resolution=${resolution}`;
  if (projectId) url += `&project_id=${projectId}`;
  const res = await fetch(url);
  if (!res.ok) throw new Error('Failed to fetch communities');
  return res.json();
}

// ─── PageRank ──────────────────────────────────────────────────────────────

export interface PageRankItem {
  id: number;
  name: string;
  kind: string;
  file: string;
  page_rank: number;
}

export interface PageRankResponse {
  rankings: PageRankItem[];
  count: number;
}

export async function fetchPageRank(limit = 50, projectId?: string): Promise<PageRankResponse> {
  let url = `${getBaseURL()}/v1/graph/pagerank?limit=${limit}`;
  if (projectId) url += `&project_id=${projectId}`;
  const res = await fetch(url);
  if (!res.ok) throw new Error('Failed to fetch PageRank');
  return res.json();
}

// ─── Cycles ────────────────────────────────────────────────────────────────

export interface CycleItem {
  node_ids: number[];
  nodes: CommunityNode[];
  length: number;
}

export interface CyclesResponse {
  cycles: CycleItem[];
  count: number;
}

export async function fetchCycles(projectId?: string): Promise<CyclesResponse> {
  let url = `${getBaseURL()}/v1/graph/cycles`;
  if (projectId) url += `?project_id=${projectId}`;
  const res = await fetch(url);
  if (!res.ok) throw new Error('Failed to fetch cycles');
  return res.json();
}

// ─── Impact Analysis ───────────────────────────────────────────────────────

export interface ImpactResponse {
  root: NodeRef | null;
  impacted_nodes: NodeRef[];
  total_affected: number;
  impact_radius: number;
  by_depth: Record<string, NodeRef[]>;
}

export async function fetchImpact(nodeId: number, depth = 3, projectId?: string): Promise<ImpactResponse> {
  let url = `${getBaseURL()}/v1/symbols/${nodeId}/impact?depth=${depth}`;
  if (projectId) url += `&project_id=${projectId}`;
  const res = await fetch(url);
  if (!res.ok) throw new Error('Failed to fetch impact analysis');
  return res.json();
}

// ─── Call Chain ────────────────────────────────────────────────────────────

export interface CallChainPath {
  nodes: NodeRef[];
}

export interface CallChainResponse {
  from: NodeRef | null;
  to: NodeRef | null;
  paths: CallChainPath[];
  found: boolean;
  count: number;
}

export async function fetchCallChain(fromId: number, toId: number, maxDepth = 5, projectId?: string): Promise<CallChainResponse> {
  const res = await fetch(`${getBaseURL()}/v1/callchain`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ from_id: fromId, to_id: toId, max_depth: maxDepth, project_id: projectId }),
  });
  if (!res.ok) throw new Error('Failed to fetch call chain');
  return res.json();
}

// ─── CFG ───────────────────────────────────────────────────────────────────

export interface CfgBlock {
  index: number;
  type: string;
  start_line: number;
  end_line: number;
  text?: string;
}

export interface CfgEdge {
  source: number;
  target: number;
  kind: string;
}

export interface CfgData {
  blocks: CfgBlock[];
  edges: CfgEdge[];
}

export interface CfgResponse {
  node: NodeRef;
  cfg: CfgData;
}

export async function fetchCfg(nodeId: number, projectId?: string): Promise<CfgResponse> {
  let url = `${getBaseURL()}/v1/symbols/${nodeId}/cfg`;
  if (projectId) url += `?project_id=${projectId}`;
  const res = await fetch(url);
  if (!res.ok) throw new Error('Failed to fetch CFG');
  return res.json();
}