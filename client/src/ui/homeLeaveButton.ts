import type { HousingStore } from '../state/housing';
import { hudLayout, place } from './hudLayout';
import { addPressFeedback, BUTTON_CLASS } from './theme';

export interface HomeLeaveButtonOptions {
  parent: HTMLElement;
  housing: HousingStore;
  /** Session.sendHomeLeave. */
  sendHomeLeave: () => void;
  viewport?: () => { width: number; height: number };
}

/** A Leave button shown only while the player is inside a home; tapping it sends HomeLeave. */
export class HomeLeaveButton {
  readonly button: HTMLButtonElement;
  private readonly unsubscribe: () => void;
  private readonly onResize = (): void => this.layout();

  constructor(private readonly opts: HomeLeaveButtonOptions) {
    injectStyle();
    this.button = document.createElement('button');
    this.button.type = 'button';
    this.button.className = `lush-leave-button lush-panel ${BUTTON_CLASS}`;
    this.button.textContent = 'Leave';
    this.button.addEventListener('click', () => opts.sendHomeLeave());
    addPressFeedback(this.button);
    opts.parent.append(this.button);
    this.unsubscribe = opts.housing.subscribe(() => this.render());
    window.addEventListener('resize', this.onResize);
    this.layout();
    this.render();
  }

  layout(): void {
    const v = this.opts.viewport?.() ?? { width: window.innerWidth, height: window.innerHeight };
    place(this.button, hudLayout(v.width, v.height).leaveButton);
  }

  render(): void {
    this.button.hidden = !this.opts.housing.inHome();
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
.lush-leave-button { z-index: 6; display: flex; align-items: center; justify-content: center; }
.lush-leave-button[hidden] { display: none; }
`;
  document.head.append(s);
}
