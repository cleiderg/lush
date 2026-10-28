// @vitest-environment happy-dom
import { beforeEach, describe, expect, it } from 'vitest';
import { GameState } from '../state/world';
import { PVP_WARNING_BAND_METRES } from '../state/pvpZone';
import { hudLayout } from './hudLayout';
import { PVP_ZONES, PvpWarning } from './pvpWarning';

beforeEach(() => document.body.replaceChildren());

const warning = () => document.getElementById('pvp-warning');
const indicator = () => document.getElementById('pvp-indicator');

describe('PvpWarning', () => {
  it('uses the PvP polygon from areas.json', () => {
    expect(PVP_ZONES.length).toBeGreaterThan(0);
  });

  it('shows the warning as the local player approaches the zone and hides it when they walk away', () => {
    const game = new GameState();
    const pos = game.prediction.pos;
    const ui = new PvpWarning({ parent: document.body, position: () => pos, viewport: () => ({ width: 390, height: 844 }) });
    // South of the zone (south edge at z = -280 m), walking north toward it.
    pos.x = 300;
    pos.z = -250;
    ui.update();
    expect(warning()).toBeNull();
    expect(indicator()).toBeNull();

    pos.z = -280 + PVP_WARNING_BAND_METRES - 1;
    ui.update();
    expect(warning()?.textContent).toMatch(/PvP/);
    expect(indicator()).toBeNull();
    ui.update();
    expect(document.querySelectorAll('#pvp-warning')).toHaveLength(1);
    const r = hudLayout(390, 844).pvp;
    expect(warning()!.style.top).toBe(`${r.y}px`);

    pos.z = -250;
    ui.update();
    expect(warning()).toBeNull();
    expect(indicator()).toBeNull();
  });

  it('shows the in-zone indicator while the player stands inside the zone', () => {
    const pos = { x: 300, z: -275 };
    let vp = { width: 390, height: 844 };
    const ui = new PvpWarning({ parent: document.body, position: () => pos, viewport: () => vp });
    ui.update();
    expect(warning()).not.toBeNull();

    pos.z = -300;
    ui.update();
    expect(indicator()?.textContent).toBe('PvP zone');
    expect(warning()).toBeNull();

    pos.z = -350;
    ui.update();
    expect(document.querySelectorAll('#pvp-indicator')).toHaveLength(1);
    // Rotating the phone moves the indicator to the new strip.
    vp = { width: 844, height: 390 };
    window.dispatchEvent(new Event('resize'));
    expect(indicator()!.style.top).toBe(`${hudLayout(844, 390).pvp.y}px`);

    pos.z = -275;
    ui.update();
    expect(indicator()).toBeNull();
    expect(warning()).not.toBeNull();
  });
});
