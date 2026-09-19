export function sortAPIKeysForDisplay<T extends { id: string; sort_order?: number; created_at: string }>(items: T[]): T[] {
  return [...items].sort((a, b) => {
    const aOrder = a.sort_order ?? 0
    const bOrder = b.sort_order ?? 0
    if (aOrder !== bOrder) {
      if (aOrder === 0 || bOrder === 0) return aOrder === 0 ? 1 : -1
      return aOrder - bOrder
    }
    const createdOrder = Date.parse(a.created_at) - Date.parse(b.created_at)
    if (Number.isFinite(createdOrder) && createdOrder !== 0) return createdOrder
    return a.id.localeCompare(b.id)
  })
}

export function moveAPIKey<T extends { id: string }>(items: T[], id: string, targetId: string): T[] {
  const from = items.findIndex((item) => item.id === id)
  const to = items.findIndex((item) => item.id === targetId)
  if (from < 0 || to < 0 || from === to) return items
  const next = [...items]
  const [item] = next.splice(from, 1)
  next.splice(to, 0, item)
  return next
}
