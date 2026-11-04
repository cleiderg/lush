import type { HousingStore } from '../state/housing';
import { hudLayout, place } from './hudLayout';
import { addPressFeedback, BUTTON_CLASS } from './theme';

export interface HomeOpenToggleOptions {
  parent: HTMLElement;
  housing: HousingStore;
  /** Session.sendHomeSetOpen. */
  sendHomeSetOpen: (open: boolean) => void;
  viewport?: () => { width: number; height: number };
}

/** An Open/Locked toggle shown only to the owner inside their own home; tapping it sends HomeSetOpen with the opposite value. */
export class HomeOpenToggle {
  readonly button: HTMLButtonElement;
  private readonly unsubscribe: () => void;
  private readonly onResize = (): void => this.layout();

  constructor(private readonly opts: HomeOpenToggleOptions) {
    injectStyle();
    this.button = document.createElement('button');
    this.button.type = 'button';
    this.button.className = `lush-open-toggle lush-panel ${BUTTON_CLASS}`;
    this.button.addEventListener('click', () => {
      const mine = opts.housing.myHouse();
      if (mine) opts.sendHomeSetOpen(!mine.open);
    });
    addPressFeedback(this.button);
    opts.parent.append(this.button);
    this.unsubscribe = opts.housing.subscribe(() => this.render());
    window.addEventListener('resize', this.onResize);
    this.layout();
    this.render();
  }

  layout(): void {
    const v = this.opts.viewport?.() ?? { width: window.innerWidth, height: window.innerHeight };
    place(this.button, hudLayout(v.width, v.height).openToggle);
  }

  render(): void {
    const mine = this.opts.housing.myHouse();
    this.button.hidden = !this.opts.housing.inOwnHome();
    this.button.textContent = mine?.open ? 'Open' : 'Locked';
  }

  dispose(): void {
    this.unsubscribe();
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
.lush-open-toggle { z-index: 6; display: flex; align-items: center; justify-content: center; }
.lush-open-toggle[hidden] { display: none; }
`;
  document.head.append(s);
}
