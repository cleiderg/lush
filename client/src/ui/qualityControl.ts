import type { QualityChoice, QualityPreset } from '../core/quality';
import { hudLayout, place } from './hudLayout';
import { addPressFeedback, BUTTON_CLASS } from './theme';

export const QUALITY_CYCLE: readonly QualityChoice[] = ['auto', 'low', 'medium', 'high'];

const LABELS: Record<QualityChoice, string> = { auto: 'Auto', low: 'Low', medium: 'Medium', high: 'High' };

/** The part of QualityController the button drives. */
export interface QualityModel {
  readonly choice: QualityChoice;
  /** The preset in use (what Auto resolved to, or the chosen one). */
  readonly preset: QualityPreset;
  setChoice(choice: QualityChoice): void;
}

export interface QualityControlOptions {
  parent: HTMLElement;
  model: QualityModel;
  viewport?: () => { width: number; height: number };
}

/** The HUD's Quality button: tap to cycle Auto → Low → Medium → High; it shows the choice and, on Auto, the preset in use. */
export class QualityControl {
  readonly button: HTMLButtonElement;
  private readonly onResize = (): void => this.layout();

  constructor(private readonly opts: QualityControlOptions) {
    injectStyle();
    this.button = document.createElement('button');
    this.button.type = 'button';
    this.button.className = `lush-quality-control ${BUTTON_CLASS}`;
    this.button.addEventListener('click', () => {
      const i = QUALITY_CYCLE.indexOf(opts.model.choice);
      opts.model.setChoice(QUALITY_CYCLE[(i + 1) % QUALITY_CYCLE.length]!);
      this.update();
    });
    addPressFeedback(this.button);
    opts.parent.append(this.button);
    window.addEventListener('resize', this.onResize);
    this.layout();
    this.update();
  }

  /** Refreshes the label; call when Auto may have stepped down. */
  update(): void {
    const { choice, preset } = this.opts.model;
    const text = choice === 'auto' ? `Auto: ${LABELS[preset]}` : LABELS[choice];
    if (this.button.dataset.label === text) return;
    this.button.dataset.label = text;
    this.button.replaceChildren();
    const title = document.createElement('span');
    title.className = 'lush-title';
    title.textContent = 'Quality';
    const value = document.createElement('span');
    value.textContent = text;
    this.button.append(title, value);
    this.button.setAttribute('aria-label', `Quality: ${text}`);
  }

  layout(): void {
    const v = this.opts.viewport?.() ?? { width: window.innerWidth, height: window.innerHeight };
    place(this.button, hudLayout(v.width, v.height).quality);
  }

  dispose(): void {
    window.removeEventListener('resize', this.onResize);
    this.button.remove();
  }
}

let styled = false;

function injectStyle(): void {
  if (styled) return;
  styled = true;
  const s = document.createElement('style');
  s.textContent = `
.lush-quality-control { box-sizing: border-box; z-index: 5; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 2px; padding: 0; border: var(--lush-border); border-radius: var(--lush-radius); background: var(--lush-panel-bg); box-shadow: var(--lush-shadow); color: var(--lush-text); font: 600 var(--lush-text-xs) var(--lush-font-body); }
.lush-quality-control .lush-title { font-size: 10px; }
`;
  document.head.append(s);
}
