import Graph from 'graphology';
import type { KnowledgeGraph, NodeLabel, SigmaNodeAttributes, SigmaEdgeAttributes, EdgeType, GraphNode, GraphRelationship } from '../types/graph';
import { NODE_COLORS, NODE_SIZES, DEFAULT_VISIBLE_EDGES } from '../types/graph';
import type { GraphDeltaResponse } from '../services/api';

// Get node size scaled for graph density
const getScaledNodeSize = (baseSize: number, nodeCount: number): number => {
  if (nodeCount > 50000) return Math.max(1, baseSize * 0.4);
  if (nodeCount > 20000) return Math.max(1.5, baseSize * 0.5);
  if (nodeCount > 5000) return Math.max(2, baseSize * 0.65);
  if (nodeCount > 1000) return Math.max(2.5, baseSize * 0.8);
  return baseSize;
};

// Get mass for node type - higher mass = more repulsion in ForceAtlas2
const getNodeMass = (nodeType: NodeLabel, nodeCount: number): number => {
  const baseMassMultiplier = nodeCount > 5000 ? 2 : nodeCount > 1000 ? 1.5 : 1;

  switch (nodeType) {
    case 'Project': return 50 * baseMassMultiplier;
    case 'Package': return 30 * baseMassMultiplier;
    case 'Module': return 20 * baseMassMultiplier;
    case 'Folder': return 15 * baseMassMultiplier;
    case 'File': return 3 * baseMassMultiplier;
    case 'Class':
    case 'Interface': return 5 * baseMassMultiplier;
    case 'Function':
    case 'Method': return 2 * baseMassMultiplier;
    default: return 1;
  }
};

// Dim a color by mixing with dark background
const hexToRgb = (hex: string): { r: number; g: number; b: number } => {
  const result = /^#?([a-f\d]{2})([a-f\d]{2})([a-f\d]{2})$/i.exec(hex);
  return result
    ? { r: parseInt(result[1], 16), g: parseInt(result[2], 16), b: parseInt(result[3], 16) }
    : { r: 100, g: 100, b: 100 };
};

const rgbToHex = (r: number, g: number, b: number): string => {
  return '#' + [r, g, b].map(x => {
    const hex = Math.max(0, Math.min(255, Math.round(x))).toString(16);
    return hex.length === 1 ? '0' + hex : hex;
  }).join('');
};

const dimColor = (hex: string, amount: number, isDarkTheme: boolean = true): string => {
  const rgb = hexToRgb(hex);
  // Use different background colors based on theme
  const bg = isDarkTheme ? { r: 18, g: 18, b: 28 } : { r: 255, g: 255, b: 255 };
  return rgbToHex(
    bg.r + (rgb.r - bg.r) * amount,
    bg.g + (rgb.g - bg.g) * amount,
    bg.b + (rgb.b - bg.b) * amount
  );
};

const brightenColor = (hex: string, factor: number): string => {
  const rgb = hexToRgb(hex);
  return rgbToHex(
    rgb.r + (255 - rgb.r) * (factor - 1) / factor,
    rgb.g + (255 - rgb.g) * (factor - 1) / factor,
    rgb.b + (255 - rgb.b) * (factor - 1) / factor
  );
};

// Edge styles
const EDGE_STYLES: Record<string, { color: string; sizeMultiplier: number }> = {
  CONTAINS: { color: '#2d5a3d', sizeMultiplier: 0.4 },
  DEFINES: { color: '#0e7490', sizeMultiplier: 0.5 },
  IMPORTS: { color: '#1d4ed8', sizeMultiplier: 0.6 },
  CALLS: { color: '#7c3aed', sizeMultiplier: 0.8 },
  EXTENDS: { color: '#c2410c', sizeMultiplier: 1.0 },
  IMPLEMENTS: { color: '#be185d', sizeMultiplier: 0.9 },
};

export function knowledgeGraphToGraphology(
  knowledgeGraph: KnowledgeGraph,
  _visibleEdgeTypes: EdgeType[] = DEFAULT_VISIBLE_EDGES
): Graph<SigmaNodeAttributes, SigmaEdgeAttributes> {
  console.log('[graph-adapter] Converting knowledge graph:', {
    nodes: knowledgeGraph.nodes.length,
    relationships: knowledgeGraph.relationships.length
  });

  const graph = new Graph<SigmaNodeAttributes, SigmaEdgeAttributes>();
  const nodeCount = knowledgeGraph.nodes.length;

  // Build parent-child map
  const parentToChildren = new Map<string, string[]>();
  const childToParent = new Map<string, string>();
  const hierarchyRelations = new Set(['CONTAINS', 'DEFINES', 'IMPORTS']);

  knowledgeGraph.relationships.forEach(rel => {
    if (hierarchyRelations.has(rel.type)) {
      const sourceId = rel.sourceId || rel.startNode;
      const targetId = rel.targetId || rel.endNode;
      if (sourceId && targetId) {
        if (!parentToChildren.has(sourceId)) {
          parentToChildren.set(sourceId, []);
        }
        parentToChildren.get(sourceId)!.push(targetId);
        childToParent.set(targetId, sourceId);
      }
    }
  });

  const nodeMap = new Map(knowledgeGraph.nodes.map(n => [n.id, n]));

  // Separate structural nodes
  const structuralTypes = new Set(['Project', 'Package', 'Module', 'Folder']);
  const structuralNodes = knowledgeGraph.nodes.filter(n => structuralTypes.has(n.label));

  const structuralSpread = Math.sqrt(nodeCount) * 40;
  const childJitter = Math.sqrt(nodeCount) * 3;

  const nodePositions = new Map<string, { x: number; y: number }>();

  // Position structural nodes
  structuralNodes.forEach((node, index) => {
    const goldenAngle = Math.PI * (3 - Math.sqrt(5));
    const angle = index * goldenAngle;
    const radius = structuralSpread * Math.sqrt((index + 1) / Math.max(structuralNodes.length, 1));

    const jitter = structuralSpread * 0.15;
    const x = radius * Math.cos(angle) + (Math.random() - 0.5) * jitter;
    const y = radius * Math.sin(angle) + (Math.random() - 0.5) * jitter;

    nodePositions.set(node.id, { x, y });

    const baseSize = NODE_SIZES[node.label] || 8;
    const scaledSize = getScaledNodeSize(baseSize, nodeCount);

    graph.addNode(node.id, {
      x,
      y,
      size: scaledSize,
      color: NODE_COLORS[node.label] || '#9ca3af',
      label: node.properties.name,
      nodeType: node.label,
      filePath: node.properties.filePath || '',
      startLine: node.properties.startLine,
      endLine: node.properties.endLine,
      hidden: false,
      mass: getNodeMass(node.label, nodeCount),
    });
  });

  // Add remaining nodes
  const addNodeWithPosition = (nodeId: string) => {
    if (graph.hasNode(nodeId)) return;

    const node = nodeMap.get(nodeId);
    if (!node) return;

    let x: number, y: number;

    const parentId = childToParent.get(nodeId);
    const parentPos = parentId ? nodePositions.get(parentId) : null;

    if (parentPos) {
      x = parentPos.x + (Math.random() - 0.5) * childJitter;
      y = parentPos.y + (Math.random() - 0.5) * childJitter;
    } else {
      x = (Math.random() - 0.5) * structuralSpread * 0.5;
      y = (Math.random() - 0.5) * structuralSpread * 0.5;
    }

    nodePositions.set(nodeId, { x, y });

    const baseSize = NODE_SIZES[node.label] || 8;
    const scaledSize = getScaledNodeSize(baseSize, nodeCount);

    graph.addNode(nodeId, {
      x,
      y,
      size: scaledSize,
      color: NODE_COLORS[node.label] || '#9ca3af',
      label: node.properties.name,
      nodeType: node.label,
      filePath: node.properties.filePath || '',
      startLine: node.properties.startLine,
      endLine: node.properties.endLine,
      hidden: false,
      mass: getNodeMass(node.label, nodeCount),
    });
  };

  // BFS from structural nodes
  const queue: string[] = [...structuralNodes.map(n => n.id)];
  const visited = new Set<string>(queue);

  while (queue.length > 0) {
    const currentId = queue.shift()!;
    const children = parentToChildren.get(currentId) || [];

    for (const childId of children) {
      if (!visited.has(childId)) {
        visited.add(childId);
        addNodeWithPosition(childId);
        queue.push(childId);
      }
    }
  }

  // Add orphan nodes
  knowledgeGraph.nodes.forEach(node => {
    if (!graph.hasNode(node.id)) {
      addNodeWithPosition(node.id);
    }
  });

  // Add edges
  const edgeBaseSize = nodeCount > 20000 ? 0.4 : nodeCount > 5000 ? 0.6 : 1.0;
  let addedEdges = 0;
  let skippedEdges = 0;

  knowledgeGraph.relationships.forEach(rel => {
    const sourceId = rel.sourceId || rel.startNode;
    const targetId = rel.targetId || rel.endNode;

    if (!sourceId || !targetId) {
      skippedEdges++;
      return;
    }

    // Check if both source and target nodes exist in the graph
    if (graph.hasNode(sourceId) && graph.hasNode(targetId)) {
      // Avoid duplicate edges
      if (!graph.hasEdge(sourceId, targetId)) {
        const style = EDGE_STYLES[rel.type] || { color: '#4a4a5a', sizeMultiplier: 0.5 };
        const curvature = 0.12 + Math.random() * 0.08;

        graph.addEdge(sourceId, targetId, {
          size: edgeBaseSize * style.sizeMultiplier,
          color: style.color,
          relationType: rel.type,
          type: 'curved',
          curvature,
          edgeId: rel.id,
        });
        addedEdges++;
      }
    } else {
      skippedEdges++;
    }
  });

  console.log('[graph-adapter] Graph conversion complete:', {
    nodes: graph.order,
    edges: graph.size,
    addedEdges,
    skippedEdges
  });

  return graph;
}

export function filterGraphByLabels(
  graph: Graph<SigmaNodeAttributes, SigmaEdgeAttributes>,
  visibleLabels: NodeLabel[]
): void {
  graph.forEachNode((nodeId, attributes) => {
    const isVisible = visibleLabels.includes(attributes.nodeType);
    graph.setNodeAttribute(nodeId, 'hidden', !isVisible);
  });
}

export function filterGraphByEdges(
  graph: Graph<SigmaNodeAttributes, SigmaEdgeAttributes>,
  visibleEdgeTypes: EdgeType[]
): void {
  graph.forEachEdge((edge, attributes) => {
    const isVisible = visibleEdgeTypes.includes(attributes.relationType as EdgeType);
    graph.setEdgeAttribute(edge, 'hidden', !isVisible);
  });
}

export function getNodesWithinHops(
  graph: Graph<SigmaNodeAttributes, SigmaEdgeAttributes>,
  startNodeId: string,
  maxHops: number
): Set<string> {
  const visited = new Set<string>();
  const queue: { nodeId: string; depth: number }[] = [{ nodeId: startNodeId, depth: 0 }];

  while (queue.length > 0) {
    const { nodeId, depth } = queue.shift()!;

    if (visited.has(nodeId)) continue;
    visited.add(nodeId);

    if (depth < maxHops) {
      graph.forEachNeighbor(nodeId, neighborId => {
        if (!visited.has(neighborId)) {
          queue.push({ nodeId: neighborId, depth: depth + 1 });
        }
      });
    }
  }

  return visited;
}

// Get unique node types from a graph
export function getUniqueNodeLabels(
  graph: Graph<SigmaNodeAttributes, SigmaEdgeAttributes>
): NodeLabel[] {
  const labels = new Set<NodeLabel>();
  graph.forEachNode((_nodeId, attributes) => {
    labels.add(attributes.nodeType);
  });
  return Array.from(labels);
}

export { dimColor, brightenColor };

// ====== Depth Filter (N-hop neighborhood filtering) ======

/**
 * Filter graph by depth (N-hop neighborhood of selected node).
 * Hides nodes that are outside the specified hop range.
 * Uses the existing getNodesWithinHops function.
 */
export function filterGraphByDepth(
  graph: Graph<SigmaNodeAttributes, SigmaEdgeAttributes>,
  selectedNodeId: string | null,
  maxHops: number | null,
  visibleLabels: NodeLabel[]
): void {
  if (maxHops === null || selectedNodeId === null) {
    filterGraphByLabels(graph, visibleLabels);
    return;
  }
  const nodesInRange = getNodesWithinHops(graph, selectedNodeId, maxHops);
  graph.forEachNode((nodeId, attributes) => {
    const isLabelVisible = visibleLabels.includes(attributes.nodeType);
    const isInRange = nodesInRange.has(nodeId);
    graph.setNodeAttribute(nodeId, 'hidden', !isLabelVisible || !isInRange);
  });
}

// ====== Auto-degradation strategy ======

export interface DegradationLevel {
  level: 'full' | 'reduced' | 'structure' | 'minimal';
  visibleLabels: NodeLabel[];
  description: string;
}

const ALL_LABELS: NodeLabel[] = ['Project', 'Package', 'Module', 'Folder', 'File', 'Class', 'Interface', 'Function', 'Method', 'Variable', 'Enum', 'Decorator', 'Import', 'Type', 'Struct', 'Trait', 'Record', 'Constant', 'Property', 'Field', 'Parameter', 'CodeElement'];
const CLASS_LEVEL_LABELS: NodeLabel[] = ['Project', 'Package', 'Module', 'Folder', 'File', 'Class', 'Interface', 'Enum', 'Struct', 'Trait', 'Record', 'Type'];
const STRUCTURE_LEVEL_LABELS: NodeLabel[] = ['Project', 'Package', 'Module', 'Folder', 'File'];

/**
 * Get the recommended degradation level based on node count.
 */
export function getAutoDegradationLevel(nodeCount: number): DegradationLevel {
  if (nodeCount > 20000) {
    return {
      level: 'structure',
      visibleLabels: STRUCTURE_LEVEL_LABELS,
      description: 'Large project: showing structure view (modules & files). Click nodes to explore.',
    };
  }
  if (nodeCount > 5000) {
    return {
      level: 'reduced',
      visibleLabels: CLASS_LEVEL_LABELS,
      description: 'Medium-large project: showing class-level view. Functions hidden for performance.',
    };
  }
  return {
    level: 'full',
    visibleLabels: ALL_LABELS,
    description: '',
  };
}

// ====== Incremental graph merge functions ======

// Helper: convert a GraphNode to graphology node attributes, matching knowledgeGraphToGraphology logic
function graphNodeToAttributes(
  node: GraphNode,
  nodeCount: number
): SigmaNodeAttributes {
  const baseSize = NODE_SIZES[node.label] || 8;
  const scaledSize = getScaledNodeSize(baseSize, nodeCount);
  return {
    x: 0, // will be positioned by mergeGraphDelta
    y: 0,
    size: scaledSize,
    color: NODE_COLORS[node.label] || '#9ca3af',
    label: node.properties.name,
    nodeType: node.label,
    filePath: node.properties.filePath || '',
    startLine: node.properties.startLine,
    endLine: node.properties.endLine,
    hidden: false,
    mass: getNodeMass(node.label, nodeCount),
  };
}

// Helper: convert a GraphRelationship to edge attributes, matching knowledgeGraphToGraphology logic
function graphRelToEdgeAttributes(
  rel: GraphRelationship,
  nodeCount: number
): SigmaEdgeAttributes {
  const style = EDGE_STYLES[rel.type] || { color: '#4a4a5a', sizeMultiplier: 0.5 };
  const edgeBaseSize = nodeCount > 20000 ? 0.4 : nodeCount > 5000 ? 0.6 : 1.0;
  const curvature = 0.12 + Math.random() * 0.08;
  return {
    size: edgeBaseSize * style.sizeMultiplier,
    color: style.color,
    relationType: rel.type,
    type: 'curved' as const,
    curvature,
  };
}

export interface MergeDeltaResult {
  addedNodeCount: number;
  removedNodeCount: number;
  addedEdgeCount: number;
  removedEdgeCount: number;
  addedNodeIds: string[]; // IDs of newly added nodes (for localized noverlap)
}

/**
 * mergeGraphDelta - Incrementally merge delta changes into an existing graphology graph.
 * Instead of creating a new graph, this modifies the existing graph in-place:
 * - Removes nodes/edges by ID
 * - Adds new nodes near their parent/neighbor positions
 * - Adds new edges
 * - Returns stats about the merge
 */
export function mergeGraphDelta(
  graph: Graph<SigmaNodeAttributes, SigmaEdgeAttributes>,
  delta: GraphDeltaResponse,
  edgeIdIndex?: Map<string, string>
): MergeDeltaResult {
  const nodeCount = graph.order;
  const result: MergeDeltaResult = {
    addedNodeCount: 0,
    removedNodeCount: 0,
    addedEdgeCount: 0,
    removedEdgeCount: 0,
    addedNodeIds: [],
  };

  // 1. Remove edges first (before removing nodes to avoid dangling reference issues)
  // Use edgeId index for O(1) lookup when available, otherwise fall back to full scan
  const edgeKeysToRemove: string[] = [];
  for (const edgeId of delta.removed_edge_ids) {
    if (edgeIdIndex && edgeIdIndex.has(edgeId)) {
      edgeKeysToRemove.push(edgeIdIndex.get(edgeId)!);
    } else {
      // Fallback: scan all edges (only needed before index is built)
      graph.forEachEdge((edgeKey, attrs) => {
        if (attrs.edgeId === edgeId) {
          edgeKeysToRemove.push(edgeKey);
        }
      });
    }
  }
  for (const edgeKey of edgeKeysToRemove) {
    try {
      // Clean up index before dropping
      const attrs = graph.getEdgeAttributes(edgeKey);
      if (attrs.edgeId && edgeIdIndex) {
        edgeIdIndex.delete(attrs.edgeId);
      }
      graph.dropEdge(edgeKey);
      result.removedEdgeCount++;
    } catch {
      // Ignore - edge may have already been dropped
    }
  }

  // 2. Remove nodes (collect edges from removed nodes first)
  const nodeEdgeKeysToRemove: string[] = [];
  for (const nodeId of delta.removed_node_ids) {
    if (graph.hasNode(nodeId)) {
      graph.forEachEdge(nodeId, (edgeKey) => {
        nodeEdgeKeysToRemove.push(edgeKey);
      });
    }
  }
  // Drop collected edges
  for (const edgeKey of nodeEdgeKeysToRemove) {
    try { graph.dropEdge(edgeKey); } catch { /* ignore */ }
  }
  // Drop nodes
  for (const nodeId of delta.removed_node_ids) {
    if (graph.hasNode(nodeId)) {
      graph.dropNode(nodeId);
      result.removedNodeCount++;
    }
  }

  // 3. Add new nodes - position near their parent/neighbor
  const childJitter = Math.sqrt(Math.max(nodeCount, 1)) * 3;

  for (const node of delta.added_nodes) {
    if (graph.hasNode(node.id)) {
      // Node already exists (update case) - update attributes
      const attrs = graphNodeToAttributes(node, nodeCount);
      const existingAttrs = graph.getNodeAttributes(node.id);
      graph.mergeNodeAttributes(node.id, {
        ...attrs,
        x: existingAttrs.x, // keep existing position
        y: existingAttrs.y,
      });
      continue;
    }

    // Find a neighbor position to place the new node near
    let x = (Math.random() - 0.5) * childJitter * 10;
    let y = (Math.random() - 0.5) * childJitter * 10;

    // Look for edges that connect to this node to find a neighbor
    const connectedEdges = delta.added_edges.filter(
      e => (e.sourceId || e.startNode) === node.id || (e.targetId || e.endNode) === node.id
    );

    for (const edge of connectedEdges) {
      const neighborId = (edge.sourceId || edge.startNode) === node.id
        ? (edge.targetId || edge.endNode)
        : (edge.sourceId || edge.startNode);

      if (neighborId && graph.hasNode(neighborId)) {
        const neighborAttrs = graph.getNodeAttributes(neighborId);
        x = neighborAttrs.x + (Math.random() - 0.5) * childJitter;
        y = neighborAttrs.y + (Math.random() - 0.5) * childJitter;
        break;
      }
    }

    const attrs = graphNodeToAttributes(node, nodeCount);
    attrs.x = x;
    attrs.y = y;
    graph.addNode(node.id, attrs);
    result.addedNodeIds.push(node.id);
    result.addedNodeCount++;
  }

  // 4. Add new edges
  for (const rel of delta.added_edges) {
    const sourceId = rel.sourceId || rel.startNode;
    const targetId = rel.targetId || rel.endNode;

    if (!sourceId || !targetId) {
      console.warn('[mergeGraphDelta] Edge skipped: missing sourceId or targetId', rel);
      continue;
    }
    if (!graph.hasNode(sourceId) || !graph.hasNode(targetId)) {
      console.warn('[mergeGraphDelta] Edge skipped: node not in graph', {
        edgeId: rel.id, sourceId, targetId,
        hasSource: graph.hasNode(sourceId), hasTarget: graph.hasNode(targetId)
      });
      continue;
    }

    // Avoid duplicate edges
    if (graph.hasEdge(sourceId, targetId)) {
      // Not an error — backend delta may include edges that already exist in the graph.
      // Silently skip to avoid console noise and wasted cycles.
      continue;
    }

    const edgeAttrs = graphRelToEdgeAttributes(rel, nodeCount);
    edgeAttrs.edgeId = rel.id; // Store backend edge ID for future delta removal
    const edgeKey = graph.addEdge(sourceId, targetId, edgeAttrs);
    // Maintain edgeId → graphology key index for O(1) removal lookups
    if (edgeIdIndex && rel.id) {
      edgeIdIndex.set(rel.id, edgeKey);
    }
    result.addedEdgeCount++;
  }

  return result;
}

// ====== Community-based initial layout clustering ======

/**
 * Assign initial positions to nodes using golden angle clustering based on community membership.
 * This positions nodes in the same community near each other, significantly accelerating
 * ForceAtlas2 convergence — same-community nodes start close to their cluster center,
 * so spring forces don't need to span long distances.
 *
 * Algorithm:
 * 1. Calculate cluster center for each community using golden angle
 * 2. Place each node near its community's cluster center with jitter
 *
 * Golden angle: ~137.508° — ensures even angular distribution of community centers
 */
export function assignCommunityClusterPositions(
  graph: Graph<SigmaNodeAttributes, SigmaEdgeAttributes>
): void {
  const nodeCount = graph.order;
  if (nodeCount === 0) return;

  // Collect community memberships
  const communities: Map<number, string[]> = new Map();

  graph.forEachNode((nodeId, attrs) => {
    const communityId = attrs.community ?? 0;
    if (!communities.has(communityId)) {
      communities.set(communityId, []);
    }
    communities.get(communityId)!.push(nodeId);
  });

  // If no community data (or only 1 community), use random positions
  if (communities.size <= 1) {
    // Default random placement within a bounded area
    graph.forEachNode((nodeId) => {
      graph.setNodeAttribute(nodeId, 'x', Math.random() * nodeCount * 2 - nodeCount);
      graph.setNodeAttribute(nodeId, 'y', Math.random() * nodeCount * 2 - nodeCount);
    });
    return;
  }

  // Golden angle in radians (≈ 137.508°)
  const GOLDEN_ANGLE = 2.39996323;
  // Jitter radius — how far nodes scatter from their community center
  const clusterJitter = Math.sqrt(nodeCount) * 1.5;
  // Scale for community center radius — how far community centers are from origin
  const centerScale = Math.sqrt(nodeCount) * 5;

  // Calculate cluster centers using golden angle
  const communityCenters: Map<number, { x: number; y: number }> = new Map();
  const communityIds = Array.from(communities.keys()).sort((a, b) => a - b);

  for (let i = 0; i < communityIds.length; i++) {
    const angle = i * GOLDEN_ANGLE;
    const radius = Math.sqrt(i + 1) * centerScale;
    communityCenters.set(communityIds[i], {
      x: radius * Math.cos(angle),
      y: radius * Math.sin(angle),
    });
  }

  // Assign each node a position near its community's cluster center
  for (const [communityId, nodeIds] of communities) {
    const center = communityCenters.get(communityId)!;
    for (const nodeId of nodeIds) {
      const jitterX = (Math.random() - 0.5) * clusterJitter;
      const jitterY = (Math.random() - 0.5) * clusterJitter;
      graph.setNodeAttribute(nodeId, 'x', center.x + jitterX);
      graph.setNodeAttribute(nodeId, 'y', center.y + jitterY);
    }
  }
}

// ====== Multi-layout: Tree & Circles ======

export type LayoutMode = 'force' | 'tree' | 'circles';

/**
 * Apply hierarchical tree layout based on CONTAINS/DEFINES edges.
 * Nodes are positioned in layers by depth from the root.
 * Each layer is evenly spaced vertically, with horizontal distribution within.
 */
export function applyTreeLayout(
  graph: Graph<SigmaNodeAttributes, SigmaEdgeAttributes>
): void {
  if (graph.order === 0) return;

  // Build hierarchy using CONTAINS/DEFINES edges
  const children: Map<string, string[]> = new Map();
  const parent: Map<string, string> = new Map();
  const roots: string[] = [];

  graph.forEachNode((nodeId) => {
    children.set(nodeId, []);
  });

  graph.forEachEdge((edge, attrs) => {
    const relType = attrs.relationType as string;
    if (relType === 'CONTAINS' || relType === 'DEFINES') {
      const [source, target] = graph.extremities(edge);
      // source contains/defines target
      children.get(source)?.push(target);
      parent.set(target, source);
    }
  });

  // Find root nodes (no parent)
  graph.forEachNode((nodeId) => {
    if (!parent.has(nodeId)) {
      roots.push(nodeId);
    }
  });

  // BFS to assign levels
  const levels: Map<string, number> = new Map();
  const queue: { nodeId: string; level: number }[] = roots.map(r => ({ nodeId: r, level: 0 }));

  while (queue.length > 0) {
    const { nodeId, level } = queue.shift()!;
    if (levels.has(nodeId)) continue;
    levels.set(nodeId, level);
    for (const child of (children.get(nodeId) || [])) {
      if (!levels.has(child)) {
        queue.push({ nodeId: child, level: level + 1 });
      }
    }
  }

  // Handle disconnected nodes
  graph.forEachNode((nodeId) => {
    if (!levels.has(nodeId)) {
      levels.set(nodeId, 0);
      roots.push(nodeId);
    }
  });

  // Group nodes by level
  const levelGroups: Map<number, string[]> = new Map();
  let maxLevel = 0;
  for (const [nodeId, level] of levels) {
    if (!levelGroups.has(level)) levelGroups.set(level, []);
    levelGroups.get(level)!.push(nodeId);
    maxLevel = Math.max(maxLevel, level);
  }

  // Position nodes
  const layerHeight = 120;
  const maxWidth = Math.max(...Array.from(levelGroups.values()).map(g => g.length));
  const horizontalSpacing = Math.max(80, Math.min(200, 2000 / maxWidth));

  for (const [level, nodeIds] of levelGroups) {
    const y = level * layerHeight;
    const totalWidth = (nodeIds.length - 1) * horizontalSpacing;
    const startX = -totalWidth / 2;

    for (let i = 0; i < nodeIds.length; i++) {
      graph.setNodeAttribute(nodeIds[i], 'x', startX + i * horizontalSpacing);
      graph.setNodeAttribute(nodeIds[i], 'y', y);
    }
  }
}

/**
 * Apply concentric circles layout based on node type hierarchy.
 * Node types are arranged in rings from center to outside:
 *   Ring 1 (innermost): Project, Package
 *   Ring 2: Module, File
 *   Ring 3: Class, Interface, Struct
 *   Ring 4 (outermost): Function, Method, Variable, etc.
 */
export function applyCirclesLayout(
  graph: Graph<SigmaNodeAttributes, SigmaEdgeAttributes>
): void {
  if (graph.order === 0) return;

  // Define ring assignments for node types
  const ringAssignment: Record<string, number> = {
    'Project': 0, 'Package': 0,
    'Module': 1, 'Folder': 1, 'File': 1,
    'Class': 2, 'Interface': 2, 'Struct': 2, 'Enum': 2, 'Trait': 2, 'Type': 2,
    'Function': 3, 'Method': 3, 'Variable': 3, 'Constant': 3, 'Property': 3,
    'Field': 3, 'Parameter': 3, 'Import': 3, 'Decorator': 3, 'CodeElement': 3,
  };

  // Ring radii (scaled by node count)
  const baseRadii = [90, 240, 420, 620];
  const scaleFactor = Math.max(1, Math.sqrt(graph.order / 500));
  const radii = baseRadii.map(r => r * scaleFactor);

  // Group nodes by ring
  const rings: Map<number, string[]> = new Map();
  graph.forEachNode((nodeId, attrs) => {
    const ring = ringAssignment[attrs.nodeType as string] ?? 3;
    if (!rings.has(ring)) rings.set(ring, []);
    rings.get(ring)!.push(nodeId);
  });

  // Position nodes on rings
  for (const [ring, nodeIds] of rings) {
    const radius = radii[ring] || radii[radii.length - 1];
    const count = nodeIds.length;

    for (let i = 0; i < count; i++) {
      const angle = (2 * Math.PI * i) / count - Math.PI / 2; // Start from top
      graph.setNodeAttribute(nodeIds[i], 'x', radius * Math.cos(angle));
      graph.setNodeAttribute(nodeIds[i], 'y', radius * Math.sin(angle));
    }
  }
}