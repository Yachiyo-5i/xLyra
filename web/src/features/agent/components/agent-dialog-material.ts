import type { AgentLiquidGlassSettings } from './liquid-glass/agent-liquid-glass'

/**
 * Agent 弹窗（确认 / 完整访问 / 编辑重放）共用的玻璃底色默认值。
 * dark（会话中）与 light（空会话）各一套；radius 由各弹窗自己覆盖。
 * lightDarkTint 供个别弹窗覆盖亮色分支的 tint（完整访问/编辑重放弹窗是 0.12，
 * 确认弹窗是 0.24）。
 */
export function agentDialogGlassDefaults(dark: boolean, lightDarkTint = 0.24): AgentLiquidGlassSettings {
  return {
    blur: dark ? 0.18 : 0.34,
    refraction: dark ? 0.72 : 0.38,
    chromaticAberration: dark ? 0.045 : 0.025,
    darkTint: dark ? 0.18 : lightDarkTint,
    tintStrength: dark ? 0.06 : 0.1,
    edgeHighlight: dark ? 0.08 : 0.1,
    specular: dark ? 0.14 : 0.12,
    fresnel: dark ? 1.08 : 0.9,
    bevel: 0,
    depth: 32,
    radius: 26,
    opacity: dark ? 1 : 0.96,
  }
}
