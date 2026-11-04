// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { QuestStore } from '../state/quests';
import { DialoguePanel } from './dialoguePanel';

let panel: DialoguePanel | null = null;

function setup() {
  const quests = new QuestStore();
  const sendReply = vi.fn<(i: number) => void>();
  panel = new DialoguePanel({ parent: document.body, quests, npcName: (id) => `NPC ${id}`, sendReply, viewport: () => ({ width: 390, height: 844 }) });
  return { quests, sendReply, panel };
}

const buttons = (): HTMLElement[] => [...document.querySelectorAll<HTMLElement>('.lush-dialogue-reply')];

beforeEach(() => document.body.replaceChildren());
afterEach(() => { panel?.dispose(); panel = null; });

describe('DialoguePanel', () => {
  it('is hidden until a dialogue opens, then shows the name, lines and replies (AC1)', () => {
    const { quests, panel } = setup();
    expect(panel.isOpen).toBe(false);
    quests.setDialogue({ npcId: 5, lines: ['Hello', 'Welcome'], replies: ['A', 'B', 'C'] });
    expect(panel.isOpen).toBe(true);
    expect(panel.panel.classList.contains('lush-panel')).toBe(true);
    expect(document.querySelector('.lush-dialogue-title')!.textContent).toBe('NPC 5');
    expect([...document.querySelectorAll('.lush-dialogue-line')].map((e) => e.textContent)).toEqual(['Hello', 'Welcome']);
    expect(buttons().map((b) => b.textContent)).toEqual(['A', 'B', 'C']);
  });

  it('shows at most 4 replies, and Continue when there are none (AC1)', () => {
    const { quests } = setup();
    quests.setDialogue({ npcId: 1, lines: ['x'], replies: ['1', '2', '3', '4', '5', '6'] });
    expect(buttons()).toHaveLength(4);
    quests.setDialogue({ npcId: 1, lines: ['x'], replies: [] });
    expect(buttons().map((b) => b.textContent)).toEqual(['Continue']);
  });

  it('tapping reply N sends it (AC2)', () => {
    const { quests, sendReply } = setup();
    quests.setDialogue({ npcId: 1, lines: ['x'], replies: ['A', 'B', 'C'] });
    buttons()[2].click();
    expect(sendReply).toHaveBeenCalledWith(2);
  });

  it('closes on DialogueClose (AC2)', () => {
    const { quests, panel } = setup();
    quests.setDialogue({ npcId: 1, lines: ['x'], replies: ['A'] });
    quests.closeDialogue();
    expect(panel.isOpen).toBe(false);
  });

  it('closes on its close button (AC2)', () => {
    const { quests, panel } = setup();
    quests.setDialogue({ npcId: 1, lines: ['x'], replies: ['A'] });
    (document.querySelector('.lush-dialogue-close') as HTMLElement).click();
    expect(panel.isOpen).toBe(false);
    expect(quests.dialogue).toBeNull();
  });

  it('closes when the player moves (AC2)', () => {
    const { quests, panel } = setup();
    quests.setDialogue({ npcId: 1, lines: ['x'], replies: ['A'] });
    panel.playerMoved();
    expect(panel.isOpen).toBe(false);
    expect(quests.dialogue).toBeNull();
  });

  it('replaces the contents of a new dialogue without a second panel (AC3)', () => {
    const { quests } = setup();
    quests.setDialogue({ npcId: 1, lines: ['old'], replies: ['A', 'B'] });
    quests.setDialogue({ npcId: 2, lines: ['new'], replies: ['C'] });
    expect(document.querySelectorAll('.lush-dialogue')).toHaveLength(1);
    expect([...document.querySelectorAll('.lush-dialogue-line')].map((e) => e.textContent)).toEqual(['new']);
    expect(buttons().map((b) => b.textContent)).toEqual(['C']);
    expect(document.querySelector('.lush-dialogue-title')!.textContent).toBe('NPC 2');
  });
});
