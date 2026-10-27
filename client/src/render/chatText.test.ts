import { Window } from 'happy-dom';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { CSS2DObject } from 'three/examples/jsm/renderers/CSS2DRenderer.js';
import { WorldScene } from './scene';
import { CHAT_HEIGHT, MAX_CHAT_CHARS, clipChat } from './chatText';
import { CHARACTER_HEIGHT } from './character';
import type { ChatBubbleView, WorldView } from './worldView';

function view(chat?: readonly ChatBubbleView[], remoteX = 2): WorldView {
  return {
    local: { id: 0, x: 0, z: 0, heading: 0 },
    remotes: [
      { id: 1, x: remoteX, z: 3, heading: 0 },
      { id: 2, x: 4, z: 0, heading: 0 },
    ],
    chat,
  };
}

function texts(w: WorldScene): CSS2DObject[] {
  const out: CSS2DObject[] = [];
  w.scene.traverse((o) => {
    if (o instanceof CSS2DObject) out.push(o);
  });
  return out;
}

function count(w: WorldScene): number {
  let n = 0;
  w.scene.traverse(() => n++);
  return n;
}

describe('overhead chat text', () => {
  // A DOM for the labels only; the happy-dom test environment can't load the campus map file.
  const g = globalThis as { document?: unknown };
  const prev = g.document;
  let win: Window;
  beforeAll(() => {
    win = new Window();
    g.document = win.document;
  });
  afterAll(async () => {
    g.document = prev;
    await win.happyDOM.close();
  });

  it('AC1: adds no text objects when chat is missing or empty', () => {
    const w = new WorldScene();
    w.update(view());
    const before = count(w);
    w.update(view(undefined));
    w.update(view([]));
    expect(texts(w)).toHaveLength(0);
    expect(count(w)).toBe(before);
    expect(w.chat.group.parent).toBeNull();
  });

  it('AC2: shows text above the local and a remote player and follows their x/z', () => {
    const w = new WorldScene();
    const chat = [
      { id: 0, text: 'hi' },
      { id: 1, text: 'hello' },
    ];
    w.update(view(chat));
    const t = texts(w);
    expect(t).toHaveLength(2);
    const local = w.chat.shown.get(0)!;
    const remote = w.chat.shown.get(1)!;
    expect(local.element.textContent).toBe('hi');
    expect(remote.element.textContent).toBe('hello');
    expect(local.position.toArray()).toEqual([0, CHAT_HEIGHT, 0]);
    expect(remote.position.toArray()).toEqual([2, CHAT_HEIGHT, 3]);
    expect(CHAT_HEIGHT).toBeGreaterThan(CHARACTER_HEIGHT);

    const moved: WorldView = { ...view(chat, 7), local: { id: 0, x: -5, z: 1, heading: 0 } };
    w.update(moved);
    expect(w.chat.shown.get(0)).toBe(local);
    expect(local.position.toArray()).toEqual([-5, CHAT_HEIGHT, 1]);
    expect(remote.position.toArray()).toEqual([7, CHAT_HEIGHT, 3]);
  });

  it('ignores bubbles for ids not in view', () => {
    const w = new WorldScene();
    w.update(view([{ id: 99, text: 'ghost' }]));
    expect(texts(w)).toHaveLength(0);
  });

  it('AC3: removes text when the bubble or the entity leaves view, and updates changed text', () => {
    const w = new WorldScene();
    w.update(view([
      { id: 1, text: 'a' },
      { id: 2, text: 'b' },
    ]));
    expect(texts(w)).toHaveLength(2);
    const label1 = w.chat.shown.get(1)!;

    w.update(view([
      { id: 1, text: 'changed' },
      { id: 2, text: 'b' },
    ]));
    expect(w.chat.shown.get(1)).toBe(label1);
    expect(label1.element.textContent).toBe('changed');

    // Bubble leaves the view.
    w.update(view([{ id: 2, text: 'b' }]));
    expect(texts(w).map((t) => t.name)).toEqual(['chat-2']);
    expect(label1.parent).toBeNull();

    // Entity leaves the view while its bubble is still listed.
    const gone: WorldView = { ...view([{ id: 2, text: 'b' }]), remotes: [{ id: 1, x: 2, z: 3, heading: 0 }] };
    w.update(gone);
    expect(texts(w)).toHaveLength(0);

    w.update(view([]));
    expect(texts(w)).toHaveLength(0);
  });

  it('AC3: keeps the scene object count steady across 100 renders of one view', () => {
    const w = new WorldScene();
    const v = view([
      { id: 0, text: 'hi' },
      { id: 1, text: 'there' },
    ]);
    w.update(v);
    const n = count(w);
    for (let i = 0; i < 100; i++) w.update(v);
    expect(count(w)).toBe(n);
    expect(texts(w)).toHaveLength(2);
  });

  it('AC4: draws markup literally as plain text', () => {
    const w = new WorldScene();
    const msg = '<img src=x onerror=alert(1)><b>hi</b>';
    w.update(view([{ id: 1, text: msg }]));
    const el = w.chat.shown.get(1)!.element;
    expect(el.textContent).toBe(msg);
    expect(el.children).toHaveLength(0);
    expect(el.querySelector('img')).toBeNull();
    expect(el.innerHTML).toContain('&lt;img');
  });

  it('clips very long messages', () => {
    const long = 'x'.repeat(200);
    expect([...clipChat(long)]).toHaveLength(MAX_CHAT_CHARS);
    expect(clipChat('short')).toBe('short');
  });
});
