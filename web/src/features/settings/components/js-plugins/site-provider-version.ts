export type CurrentVersionRow = {
  status: string
  version: string
  created_at?: string
}

function createdAtMs(value?: string): number | null {
  if (!value) return null
  const ms = Date.parse(value)
  return Number.isNaN(ms) ? null : ms
}

function semverCore(version: string) {
  return version.replace(/^[vV]/, '')
}

/** 返回值 < 0 表示 left 更新。段内先比前导数字；正式版新于同号预发布；后缀只按字符串，不做完整预发布优先级。 */
function compareSemverDesc(left: string, right: string): number {
  const a = semverCore(left).split('.')
  const b = semverCore(right).split('.')
  const count = Math.max(a.length, b.length)
  for (let index = 0; index < count; index += 1) {
    const av = a[index] ?? '0'
    const bv = b[index] ?? '0'
    const an = /^(\d+)/.exec(av)
    const bn = /^(\d+)/.exec(bv)
    if (an && bn) {
      const diff = Number(bn[1]) - Number(an[1])
      if (diff !== 0) return diff
      const asuf = av.slice(an[1].length)
      const bsuf = bv.slice(bn[1].length)
      if (asuf !== bsuf) {
        if (asuf === '') return -1
        if (bsuf === '') return 1
        return bsuf.localeCompare(asuf, 'en')
      }
    } else {
      const diff = bv.localeCompare(av, 'en')
      if (diff !== 0) return diff
    }
  }
  return 0
}

/**
 * 谁更新（返回值 < 0 表示 left 更新），不看数组原来的顺序。
 * 接口按 created_at DESC 返回，这里仍自己排，避免顺序一变就选错。
 * 1. 两边 created_at 都能解析且不相同：时间晚的更新。
 * 2. 只有一边能解析：有时间的更新（缺失的排后面）。
 * 3. 都没有、无法解析、或时间相同：semver 降序，再按原始版本字符串降序。
 */
function compareVersionRecency(left: CurrentVersionRow, right: CurrentVersionRow): number {
  const leftAt = createdAtMs(left.created_at)
  const rightAt = createdAtMs(right.created_at)
  if (leftAt !== null && rightAt !== null && leftAt !== rightAt) return rightAt - leftAt
  if (leftAt !== null && rightAt === null) return -1
  if (leftAt === null && rightAt !== null) return 1
  const byVersion = compareSemverDesc(left.version, right.version)
  if (byVersion !== 0) return byVersion
  return right.version.localeCompare(left.version, 'en')
}

/**
 * 站点提供方区块用的那一个版本。
 * 有 status === 'enabled' 就用它；多个启用时取其中最新的一个。
 * 都没启用则用 compareVersionRecency 定义的最新版本。
 */
export function pickCurrentVersionRow<T extends CurrentVersionRow>(versions: readonly T[]): T | null {
  if (versions.length === 0) return null
  const enabled = versions.filter((row) => row.status === 'enabled')
  const pool = enabled.length > 0 ? enabled : versions
  return [...pool].sort(compareVersionRecency)[0] ?? null
}
