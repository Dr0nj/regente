/**
 * Workflow Versioning — Phase 5
 *
 * Keeps a version history for each workflow folder.
 * Allows rollback to any previous save.
 * Storage: localStorage (Supabase-ready).
 */

import type { WorkflowNode, WorkflowEdge } from "./database.types";

export interface WorkflowVersion {
  version: number;
  savedAt: string;
  label: string;
  nodeCount: number;
  edgeCount: number;
  nodes: WorkflowNode[];
  edges: WorkflowEdge[];
}

const LOCAL_KEY_PREFIX = "regente_versions_";
const MAX_VERSIONS = 30;

function storeKey(folderId: string) {
  return `${LOCAL_KEY_PREFIX}${folderId}`;
}

function readVersions(folderId: string): WorkflowVersion[] {
  try {
    const raw = localStorage.getItem(storeKey(folderId));
    return raw ? JSON.parse(raw) : [];
  } catch {
    return [];
  }
}

function writeVersions(folderId: string, versions: WorkflowVersion[]) {
  localStorage.setItem(storeKey(folderId), JSON.stringify(versions));
}

/** Push a new version snapshot. Returns the version number. */
export function pushVersion(
  folderId: string,
  nodes: WorkflowNode[],
  edges: WorkflowEdge[],
  label = "Manual save"
): number {
  const versions = readVersions(folderId);
  const nextVer = versions.length > 0 ? versions[versions.length - 1].version + 1 : 1;

  versions.push({
    version: nextVer,
    savedAt: new Date().toISOString(),
    label,
    nodeCount: nodes.length,
    edgeCount: edges.length,
    nodes: JSON.parse(JSON.stringify(nodes)),
    edges: JSON.parse(JSON.stringify(edges)),
  });

  // Trim old versions
  if (versions.length > MAX_VERSIONS) {
    versions.splice(0, versions.length - MAX_VERSIONS);
  }

  writeVersions(folderId, versions);
  return nextVer;
}

/** List all versions for a folder (metadata only — no nodes/edges) */
export function listVersions(folderId: string): Omit<WorkflowVersion, "nodes" | "edges">[] {
  return readVersions(folderId).map(({ nodes: _n, edges: _e, ...meta }) => meta);
}

/** Load a specific version's full data */
export function loadVersion(folderId: string, version: number): WorkflowVersion | null {
  const versions = readVersions(folderId);
  return versions.find((v) => v.version === version) ?? null;
}

/** Get the latest version number (0 if none) */
export function latestVersionNumber(folderId: string): number {
  const versions = readVersions(folderId);
  return versions.length > 0 ? versions[versions.length - 1].version : 0;
}

/** Delete all version history for a folder */
export function clearVersions(folderId: string) {
  localStorage.removeItem(storeKey(folderId));
}
