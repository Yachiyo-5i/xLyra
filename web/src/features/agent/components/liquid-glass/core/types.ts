export interface LiquidGlassSettings {
  blur: number;
  refraction: number;
  chromaticAberration: number;
  edgeHighlight: number;
  specular: number;
  fresnel: number;
  radius: number;
  depth: number;
  brightness: number;
  saturation: number;
  darkTint: number;
  tintStrength: number;
  tintColor: [number, number, number];
  tint: number;
  opacity: number;
  bevel: number;
  lensWidth: number;
  lensHeight: number;
}

export type LiquidGlassVariant = "clear" | "frosted" | "dark" | "prism" | "dome";

export const liquidGlassPresets: Record<LiquidGlassVariant, LiquidGlassSettings> = {
  clear: {
    blur: 1,
    refraction: 1.2,
    chromaticAberration: 0.012,
    edgeHighlight: 0,
    specular: 0.04,
    fresnel: 0.56,
    radius: 18,
    depth: 14,
    brightness: 0.02,
    saturation: 0,
    darkTint: 0,
    tintStrength: 0.38,
    tintColor: [0.92, 0.95, 1.05],
    tint: 0,
    opacity: 0.88,
    bevel: 1,
    lensWidth: 56,
    lensHeight: 32
  },
  frosted: {
    blur: 0.52,
    refraction: 0.58,
    chromaticAberration: 0.035,
    edgeHighlight: 0.11,
    specular: 0.08,
    fresnel: 0.95,
    radius: 22,
    depth: 36,
    brightness: 0.04,
    saturation: -0.08,
    darkTint: 0.09,
    tintStrength: 0.12,
    tintColor: [0.92, 0.95, 1.05],
    tint: 0,
    opacity: 1,
    bevel: 0,
    lensWidth: 54,
    lensHeight: 34
  },
  dark: {
    blur: 0.18,
    refraction: 0.72,
    chromaticAberration: 0.045,
    edgeHighlight: 0.08,
    specular: 0.14,
    fresnel: 1.08,
    radius: 22,
    depth: 42,
    brightness: -0.03,
    saturation: 0.03,
    darkTint: 0.28,
    tintStrength: 0.06,
    tintColor: [0.92, 0.95, 1.05],
    tint: 0,
    opacity: 1,
    bevel: 0,
    lensWidth: 54,
    lensHeight: 34
  },
  prism: {
    blur: 0.06,
    refraction: 0.82,
    chromaticAberration: 0.18,
    edgeHighlight: 0.13,
    specular: 0.08,
    fresnel: 1.18,
    radius: 22,
    depth: 48,
    brightness: 0.02,
    saturation: 0.1,
    darkTint: 0.12,
    tintStrength: 0.14,
    tintColor: [0.92, 0.95, 1.05],
    tint: 0,
    opacity: 1,
    bevel: 0,
    lensWidth: 58,
    lensHeight: 36
  },
  dome: {
    blur: 0.08,
    refraction: 0.74,
    chromaticAberration: 0.06,
    edgeHighlight: 0.12,
    specular: 0.16,
    fresnel: 1.05,
    radius: 22,
    depth: 56,
    brightness: 0.02,
    saturation: 0.02,
    darkTint: 0.11,
    tintStrength: 0.1,
    tintColor: [0.92, 0.95, 1.05],
    tint: 0,
    opacity: 1,
    bevel: 1,
    lensWidth: 56,
    lensHeight: 38
  }
};

export function resolveLiquidGlassSettings(
  variant: LiquidGlassVariant = "frosted",
  overrides?: Partial<LiquidGlassSettings>
): LiquidGlassSettings {
  return { ...liquidGlassPresets[variant], ...overrides };
}
