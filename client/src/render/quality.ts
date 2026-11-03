import { SHADOW_MAP_SIZE } from './sky';

/** Render quality (visual-style.md Performance). High is the reference look. */
export type QualityPreset = 'low' | 'medium' | 'high';

/** Sun shadow map size per side in texels; 0 means no sun shadows. Placeholders until profiled on an iPhone 13 (PRD 13, Q1). */
export const QUALITY_SHADOW_MAP: Readonly<Record<QualityPreset, number>> = { high: SHADOW_MAP_SIZE, medium: 1024, low: 0 };

/** Factor on the grass tuft and flower density. Placeholders until profiled on an iPhone 13 (PRD 13, Q1). */
export const QUALITY_GRASS: Readonly<Record<QualityPreset, number>> = { high: 1, medium: 0.5, low: 0.25 };
