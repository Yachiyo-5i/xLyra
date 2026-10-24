/**
 * 画质档位：2 = 完整，1 = 减半，0 = 最省。
 * 按真实的帧间隔自动往下调，低配设备或被系统限频（低电量模式）时不会一直卡着；
 * 只降不升，避免在临界点来回抖动。
 */
export type QualityTier = 0 | 1 | 2

/** 初始档位：核心数少或内存小的设备直接从中档开始 */
export function initialQualityTier(): QualityTier {
  if (typeof navigator === 'undefined') return 2
  const cores = navigator.hardwareConcurrency ?? 8
  const memory = (navigator as Navigator & { deviceMemory?: number }).deviceMemory ?? 8
  if (cores <= 4 || memory <= 4) return 1
  return 2
}

const SLOW_FRAME_MS = 24
const SLOW_FRAMES_TO_DROP = 40
const IGNORE_ABOVE_MS = 250

export function createFrameGovernor(start: QualityTier = initialQualityTier()) {
  let tier: QualityTier = start
  let average = 16.7
  let slowFrames = 0

  return {
    get tier() {
      return tier
    },
    /** 每帧调用一次，传入距上一帧的毫秒数；返回当前档位 */
    sample(delta: number): QualityTier {
      // 切走标签页再回来会有一个超长的帧，不算
      if (delta <= 0 || delta > IGNORE_ABOVE_MS) return tier
      average = average * 0.9 + delta * 0.1
      if (average > SLOW_FRAME_MS) {
        slowFrames += 1
        if (slowFrames >= SLOW_FRAMES_TO_DROP && tier > 0) {
          tier = (tier - 1) as QualityTier
          slowFrames = 0
          average = 16.7
        }
      } else {
        slowFrames = 0
      }
      return tier
    },
  }
}
