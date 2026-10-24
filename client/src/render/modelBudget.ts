// Per-model triangle and file-size budgets for glTF models (issue #86, answer to #82).
// PLACEHOLDER values: tune after profiling on an iPhone 13 (PRD section 13).

export interface ModelBudget {
  maxTriangles: number;
  maxBytes: number;
}

export const CHARACTER_BUDGET: ModelBudget = { maxTriangles: 1500, maxBytes: 150 * 1024 };
export const PROP_BUDGET: ModelBudget = { maxTriangles: 1000, maxBytes: 100 * 1024 };
export const BUILDING_BUDGET: ModelBudget = { maxTriangles: 8000, maxBytes: 500 * 1024 };
export const DEFAULT_MODEL_BUDGET: ModelBudget = { maxTriangles: 3000, maxBytes: 200 * 1024 };

export const MODEL_BUDGETS_BY_FOLDER: Readonly<Record<string, ModelBudget>> = {
  characters: CHARACTER_BUDGET,
  props: PROP_BUDGET,
  buildings: BUILDING_BUDGET,
};

/** Budget for a model path relative to client/public/models/ (first folder picks the budget). */
export function budgetForModel(relPath: string): ModelBudget {
  const parts = relPath.split(/[\\/]/).filter(Boolean);
  const folder = parts.length > 1 ? parts[0] : undefined;
  return (folder && MODEL_BUDGETS_BY_FOLDER[folder]) || DEFAULT_MODEL_BUDGET;
}

interface GltfAccessor {
  count: number;
}
interface GltfPrimitive {
  attributes?: Record<string, number>;
  indices?: number;
  mode?: number;
}
interface Gltf {
  accessors?: GltfAccessor[];
  meshes?: { primitives?: GltfPrimitive[] }[];
  nodes?: { mesh?: number }[];
}

const GLB_MAGIC = 0x46546c67;
const CHUNK_JSON = 0x4e4f534a;

function parseGlbJson(bytes: Uint8Array): Gltf {
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  if (bytes.byteLength < 20 || view.getUint32(0, true) !== GLB_MAGIC) {
    throw new Error('not a GLB file');
  }
  const jsonLen = view.getUint32(12, true);
  if (view.getUint32(16, true) !== CHUNK_JSON || 20 + jsonLen > bytes.byteLength) {
    throw new Error('GLB is missing its JSON chunk');
  }
  return JSON.parse(new TextDecoder().decode(bytes.subarray(20, 20 + jsonLen))) as Gltf;
}

function primitiveTriangles(gltf: Gltf, p: GltfPrimitive): number {
  const accessorIdx = p.indices ?? p.attributes?.POSITION;
  const count = accessorIdx === undefined ? 0 : (gltf.accessors?.[accessorIdx]?.count ?? 0);
  switch (p.mode ?? 4) {
    case 4:
      return Math.floor(count / 3);
    case 5: // triangle strip
    case 6: // triangle fan
      return Math.max(0, count - 2);
    default:
      return 0; // points and lines draw no triangles
  }
}

/** Triangles drawn by a GLB; a mesh used by several nodes counts once per node. */
export function countGlbTriangles(bytes: Uint8Array): number {
  const gltf = parseGlbJson(bytes);
  const meshTris = (gltf.meshes ?? []).map((m) =>
    (m.primitives ?? []).reduce((sum, p) => sum + primitiveTriangles(gltf, p), 0),
  );
  const instanced = (gltf.nodes ?? []).filter((n) => n.mesh !== undefined);
  if (instanced.length === 0) return meshTris.reduce((a, b) => a + b, 0);
  return instanced.reduce((sum, n) => sum + (meshTris[n.mesh as number] ?? 0), 0);
}

/** Human-readable budget violations for a model; empty when within budget. */
export function checkModelBudget(relPath: string, bytes: Uint8Array): string[] {
  const budget = budgetForModel(relPath);
  const problems: string[] = [];
  const triangles = countGlbTriangles(bytes);
  if (triangles > budget.maxTriangles) {
    problems.push(`${relPath}: ${triangles} triangles exceeds budget of ${budget.maxTriangles}`);
  }
  if (bytes.byteLength > budget.maxBytes) {
    problems.push(`${relPath}: ${bytes.byteLength} bytes exceeds budget of ${budget.maxBytes}`);
  }
  return problems;
}
