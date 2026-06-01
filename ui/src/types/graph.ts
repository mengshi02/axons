// Graph types matching the Go backend

export type NodeLabel =
  | 'Project'
  | 'Package'
  | 'Module'
  | 'Folder'
  | 'File'
  | 'Class'
  | 'Function'
  | 'Method'
  | 'Variable'
  | 'Interface'
  | 'Enum'
  | 'Decorator'
  | 'Import'
  | 'Type'
  | 'Struct'
  | 'Trait'
  | 'Record'
  | 'Constant'
  | 'Property'
  | 'Field'
  | 'Parameter'
  | 'CodeElement'
  | 'Community'
  | 'Process';

export interface GraphNode {
  id: string;
  label: NodeLabel;
  properties: {
    name: string;
    path?: string;
    filePath?: string;
    startLine?: number;
    endLine?: number;
    content?: string;
    heuristicLabel?: string;
    cohesion?: number;
    symbolCount?: number;
    processType?: string;
    stepCount?: number;
    communities?: string[];
    entryPointId?: string;
    terminalId?: string;
    x?: number;
    y?: number;
    size?: number;
    color?: string;
  };
}

export interface GraphRelationship {
  id: string;
  type: string;
  startNode: string;
  endNode: string;
  sourceId?: string;
  targetId?: string;
  confidence?: number;
  reason?: string;
  step?: number;
}

export interface KnowledgeGraph {
  nodes: GraphNode[];
  relationships: GraphRelationship[];
  stats?: GraphStats;
}

export interface GraphStats {
  total_nodes: number;
  total_edges: number;
  returned_nodes: number;
  returned_edges: number;
  filtered_connected: boolean;
}

export interface RepoInfo {
  name: string;
  path: string;
  indexedAt?: string;
  lastCommit?: string;
  stats?: {
    files?: number;
    nodes?: number;
    symbols?: number;
    calls?: number;
  };
}

export interface SearchResult {
  id: string;
  name: string;
  type: NodeLabel;
  filePath: string;
  startLine: number;
  endLine: number;
  score: number;
  content?: string;
}

export interface CodeReference {
  id: string;
  filePath: string;
  startLine?: number;
  endLine?: number;
  nodeId?: string;
  label?: NodeLabel;
  name?: string;
  source: 'user' | 'ai';
}

export type EdgeType = 'CONTAINS' | 'DEFINES' | 'IMPORTS' | 'CALLS' | 'EXTENDS' | 'IMPLEMENTS';

export const ALL_EDGE_TYPES: EdgeType[] = [
  'CONTAINS',
  'DEFINES',
  'IMPORTS',
  'CALLS',
  'EXTENDS',
  'IMPLEMENTS',
];

export const EDGE_INFO: Record<EdgeType, { color: string; label: string }> = {
  CONTAINS: { color: '#2d5a3d', label: 'Contains' },
  DEFINES: { color: '#0e7490', label: 'Defines' },
  IMPORTS: { color: '#1d4ed8', label: 'Imports' },
  CALLS: { color: '#7c3aed', label: 'Calls' },
  EXTENDS: { color: '#c2410c', label: 'Extends' },
  IMPLEMENTS: { color: '#be185d', label: 'Implements' },
};

// Node colors by type - high contrast palette
export const NODE_COLORS: Record<NodeLabel, string> = {
  // Structural nodes - Purple/Indigo spectrum
  Project: '#a855f7',    // Purple
  Package: '#8b5cf6',    // Violet
  Module: '#7c3aed',     // Deep violet
  Folder: '#6366f1',     // Indigo
  File: '#3b82f6',       // Blue

  // Type definitions - Warm colors (orange/yellow spectrum)
  Class: '#f59e0b',      // Amber
  Interface: '#ec4899',  // Pink
  Struct: '#ea580c',     // Deep orange
  Enum: '#f97316',       // Orange
  Trait: '#db2777',      // Deep pink
  Record: '#d97706',     // Dark amber
  Type: '#a78bfa',       // Light violet

  // Code elements - Green/Teal/Cyan spectrum
  Function: '#10b981',   // Emerald green
  Method: '#0891b2',     // Cyan (distinct from Function)
  Variable: '#64748b',   // Slate gray
  Constant: '#84cc16',   // Lime
  Property: '#06b6d4',   // Cyan
  Field: '#0ea5e9',      // Sky blue
  Parameter: '#a3e635',  // Light lime

  // Other
  Decorator: '#eab308',  // Yellow
  Import: '#475569',     // Dark slate
  CodeElement: '#64748b', // Slate gray
  Community: '#818cf8',  // Light indigo
  Process: '#f43f5e',    // Rose
};

// Node sizes by type
export const NODE_SIZES: Record<NodeLabel, number> = {
  Project: 20,
  Package: 16,
  Module: 13,
  Folder: 10,
  File: 6,
  Class: 8,
  Function: 4,
  Method: 3,
  Variable: 2,
  Interface: 7,
  Enum: 5,
  Decorator: 2,
  Import: 1.5,
  Type: 3,
  Struct: 8,
  Trait: 7,
  Record: 8,
  Constant: 2,
  Property: 2,
  Field: 2,
  Parameter: 1.5,
  CodeElement: 2,
  Community: 0,
  Process: 0,
};

// Filterable labels
export const FILTERABLE_LABELS: NodeLabel[] = [
  'Folder',
  'File',
  'Class',
  'Function',
  'Method',
  'Variable',
  'Interface',
  'Import',
];

export const DEFAULT_VISIBLE_LABELS: NodeLabel[] = [
  'Project',
  'Package',
  'Module',
  'Folder',
  'File',
  'Class',
  'Function',
  'Method',
  'Interface',
  'Enum',
  'Type',
];

export const DEFAULT_VISIBLE_EDGES: EdgeType[] = [
  'CONTAINS',
  'DEFINES',
  'IMPORTS',
  'EXTENDS',
  'IMPLEMENTS',
  'CALLS',
];

// Community color palette
export const COMMUNITY_COLORS = [
  '#ef4444', '#f97316', '#eab308', '#22c55e', '#06b6d4',
  '#3b82f6', '#8b5cf6', '#d946ef', '#ec4899', '#f43f5e',
  '#14b8a6', '#84cc16',
];

export const getCommunityColor = (communityIndex: number): string => {
  return COMMUNITY_COLORS[communityIndex % COMMUNITY_COLORS.length];
};

// Sigma.js node/edge attributes
export interface SigmaNodeAttributes {
  x: number;
  y: number;
  size: number;
  color: string;
  label: string;
  labelColor?: string;
  nodeType: NodeLabel;
  filePath: string;
  startLine?: number;
  endLine?: number;
  hidden?: boolean;
  zIndex?: number;
  highlighted?: boolean;
  mass?: number;
  community?: number;
  communityColor?: string;
}

export interface SigmaEdgeAttributes {
  size: number;
  color: string;
  relationType: string;
  type?: string;
  curvature?: number;
  hidden?: boolean;
  zIndex?: number;
  edgeId?: string; // Backend edge ID for incremental delta removal
}