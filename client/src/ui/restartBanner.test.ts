// @vitest-environment happy-dom
import { beforeEach, describe, expect, it } from 'vitest';
import { GameState } from '../state/world';
import { RestartBanner } from './restartBanner';

beforeEach(() => document.body.replaceChildren());

describe('RestartBanner', () => {
  it('adds, updates and removes the banner with countdown state', () => {
    const game = new GameState();
    const banner = new RestartBanner(document.body);
    const q = () => document.getElementById('restart-banner');

    banner.set(game.restartSeconds);
    expect(q()).toBeNull();

    game.handle({ type: 'restartCountdown', seconds: 30 }, 0);
    banner.set(game.restartSeconds);
    expect(q()?.textContent).toContain('30');

    game.handle({ type: 'restartCountdown', seconds: 29 }, 0);
    banner.set(game.restartSeconds);
    expect(q()?.textContent).toContain('29');
    expect(document.querySelectorAll('#restart-banner')).toHaveLength(1);

    game.restartSeconds = null;
    banner.set(game.restartSeconds);
    expect(q()).toBeNull();
  });
});
