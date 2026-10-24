import type { Object3D } from 'three';
import { GLTFLoader } from 'three/examples/jsm/loaders/GLTFLoader.js';

const loader = new GLTFLoader();
const cache = new Map<string, Promise<Object3D>>();

async function fetchModel(url: string): Promise<Object3D> {
  try {
    const res = await fetch(url);
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    const gltf = await loader.parseAsync(await res.arrayBuffer(), url);
    return gltf.scene;
  } catch (err) {
    const reason = err instanceof Error ? err.message : String(err);
    throw new Error(`Failed to load model ${url}: ${reason}`, { cause: err });
  }
}

/**
 * Loads a glTF/GLB model once per URL and returns an independent clone on
 * every call. Geometry and materials are shared between clones. Failed loads
 * are not cached, so a later call retries.
 */
export async function loadModel(url: string): Promise<Object3D> {
  let pending = cache.get(url);
  if (!pending) {
    pending = fetchModel(url);
    cache.set(url, pending);
    pending.catch(() => {
      if (cache.get(url) === pending) cache.delete(url);
    });
  }
  return (await pending).clone(true);
}

/** Drops all cached models (tests, memory pressure). */
export function clearModelCache(): void {
  cache.clear();
}
