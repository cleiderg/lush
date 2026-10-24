import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { clearModelCache, loadModel } from './modelLoader';

function makeGlb(): ArrayBuffer {
  const json = JSON.stringify({
    asset: { version: '2.0' },
    scene: 0,
    scenes: [{ nodes: [0] }],
    nodes: [{ name: 'root' }],
  });
  const enc = new TextEncoder().encode(json);
  const padded = new Uint8Array(Math.ceil(enc.length / 4) * 4).fill(0x20);
  padded.set(enc);
  const buf = new ArrayBuffer(20 + padded.length);
  const v = new DataView(buf);
  v.setUint32(0, 0x46546c67, true);
  v.setUint32(4, 2, true);
  v.setUint32(8, buf.byteLength, true);
  v.setUint32(12, padded.length, true);
  v.setUint32(16, 0x4e4f534a, true);
  new Uint8Array(buf, 20).set(padded);
  return buf;
}

const respond = (body: ArrayBuffer) => ({ ok: true, status: 200, arrayBuffer: async () => body });

describe('loadModel', () => {
  let fetchMock: ReturnType<typeof vi.fn>;
  beforeEach(() => {
    clearModelCache();
    fetchMock = vi.fn(async () => respond(makeGlb()));
    vi.stubGlobal('fetch', fetchMock);
  });
  afterEach(() => vi.unstubAllGlobals());

  it('fetches once for repeated and concurrent calls', async () => {
    const [a, b] = await Promise.all([loadModel('/m/a.glb'), loadModel('/m/a.glb')]);
    await loadModel('/m/a.glb');
    expect(a.children[0]?.name).toBe('root');
    expect(b.children[0]?.name).toBe('root');
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('returns independent clones', async () => {
    const a = await loadModel('/m/a.glb');
    const b = await loadModel('/m/a.glb');
    expect(a).not.toBe(b);
    a.position.set(5, 6, 7);
    expect(b.position.toArray()).toEqual([0, 0, 0]);
  });

  it('rejects with the url on fetch error and retries later', async () => {
    fetchMock.mockRejectedValueOnce(new Error('network down'));
    await expect(loadModel('/m/bad.glb')).rejects.toThrow('/m/bad.glb');
    await expect(loadModel('/m/bad.glb')).resolves.toBeDefined();
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('resolves external glTF buffers relative to the model url', async () => {
    const gltf = JSON.stringify({
      asset: { version: '2.0' },
      scene: 0,
      scenes: [{ nodes: [0] }],
      nodes: [{ name: 'root', mesh: 0 }],
      meshes: [{ primitives: [{ attributes: { POSITION: 0 } }] }],
      accessors: [
        { bufferView: 0, componentType: 5126, count: 3, type: 'VEC3', min: [0, 0, 0], max: [1, 1, 0] },
      ],
      bufferViews: [{ buffer: 0, byteLength: 36 }],
      buffers: [{ uri: 'tri.bin', byteLength: 36 }],
    });
    const bin = new Float32Array([0, 0, 0, 1, 0, 0, 0, 1, 0]).buffer;
    fetchMock.mockImplementation(async (input: unknown) => {
      const url = input instanceof Request ? input.url : String(input);
      if (url === 'https://cdn.test/models/tri.bin') return respond(bin);
      if (url === 'https://cdn.test/models/tri.gltf') return respond(new TextEncoder().encode(gltf).buffer);
      throw new Error(`unexpected fetch ${url}`);
    });
    const model = await loadModel('https://cdn.test/models/tri.gltf');
    expect(model.getObjectByName('root')).toBeDefined();
  });

  it('rejects with the url on invalid GLB and retries later', async () => {
    fetchMock.mockResolvedValueOnce(respond(new Uint8Array([1, 2, 3, 4]).buffer));
    const err = await loadModel('/m/junk.glb').catch((e: unknown) => e);
    expect(err).toBeInstanceOf(Error);
    expect((err as Error).message).toContain('/m/junk.glb');
    await expect(loadModel('/m/junk.glb')).resolves.toBeDefined();
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });
});
