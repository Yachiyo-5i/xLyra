/**
 * Only one hover card may be visible at a time. Every HoverDetails keeps its
 * own open state and closes with a short delay, so without a shared registry two
 * neighbouring triggers show their cards together while the pointer moves from
 * one to the next (the old card is still waiting for its close timer).
 */

export type HoverCardHandle = {
  id: string
  /** Hides the card immediately, without the close delay. */
  close: () => void
}

let activeCard: HoverCardHandle | null = null

/** Marks a card as the visible one and closes the previously visible card. */
export function activateHoverCard(handle: HoverCardHandle) {
  const previous = activeCard
  activeCard = handle
  if (previous && previous.id !== handle.id) previous.close()
}

/** Called when a card closes; only releases the slot if the card still owns it. */
export function releaseHoverCard(id: string) {
  if (activeCard?.id === id) activeCard = null
}

export function activeHoverCardId() {
  return activeCard?.id ?? null
}
