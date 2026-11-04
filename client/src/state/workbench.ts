import { CraftResultCode } from '../protocol';
import type { WorkbenchRecipeInfo } from '../net/codec';

export type { WorkbenchRecipeInfo };

/** The workbench recipes; each WorkbenchCatalog replaces the list. */
export class WorkbenchStore {
  recipes: WorkbenchRecipeInfo[] = [];
  private listeners = new Set<() => void>();

  /** Calls fn after every catalog; returns an unsubscribe function. */
  subscribe(fn: () => void): () => void {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  }

  setRecipes(recipes: WorkbenchRecipeInfo[]): void {
    this.recipes = recipes.map((r) => ({ ...r, inputs: r.inputs.map((i) => ({ ...i })) }));
    for (const fn of this.listeners) fn();
  }

  recipe(outputItemId: number): WorkbenchRecipeInfo | undefined {
    return this.recipes.find((r) => r.outputItemId === outputItemId);
  }
}

/** Chat line for every CraftResult code. */
export function craftResultText(code: CraftResultCode): string {
  switch (code) {
    case CraftResultCode.Ok: return 'You knock it together at the workbench';
    case CraftResultCode.TooFar: return 'You need to be closer to a workbench';
    case CraftResultCode.UnknownRecipe: return "You don't know how to make that";
    case CraftResultCode.MissingInputs: return "You don't have the materials for that";
    case CraftResultCode.NotEnoughGold: return "You don't have enough gold for that";
    case CraftResultCode.InventoryFull: return 'Your pack is too full to carry that';
    case CraftResultCode.SaveFailed: return 'The craft could not be saved. Try again';
    default: return 'Something went wrong at the workbench';
  }
}
