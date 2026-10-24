import { beforeEach, describe, expect, it, vi } from 'vitest'
import { activateHoverCard, activeHoverCardId, releaseHoverCard } from './hover-details-registry'

describe('hover card registry', () => {
  beforeEach(() => {
    releaseHoverCard(activeHoverCardId() ?? '')
  })

  it('closes the previously visible card as soon as another one opens', () => {
    const first = { id: 'a', close: vi.fn() }
    const second = { id: 'b', close: vi.fn() }

    activateHoverCard(first)
    expect(first.close).not.toHaveBeenCalled()

    activateHoverCard(second)
    expect(first.close).toHaveBeenCalledTimes(1)
    expect(second.close).not.toHaveBeenCalled()
    expect(activeHoverCardId()).toBe('b')
  })

  it('does not close a card that is activated again', () => {
    const card = { id: 'a', close: vi.fn() }
    activateHoverCard(card)
    activateHoverCard(card)
    expect(card.close).not.toHaveBeenCalled()
  })

  it('only releases the slot for the card that owns it', () => {
    const first = { id: 'a', close: vi.fn() }
    const second = { id: 'b', close: vi.fn() }
    activateHoverCard(first)
    activateHoverCard(second)

    // The old card finishes its delayed close after the new one took over.
    releaseHoverCard('a')
    expect(activeHoverCardId()).toBe('b')

    releaseHoverCard('b')
    expect(activeHoverCardId()).toBeNull()
  })

  it('has nothing to close when no card was visible', () => {
    const card = { id: 'a', close: vi.fn() }
    expect(activeHoverCardId()).toBeNull()
    activateHoverCard(card)
    expect(card.close).not.toHaveBeenCalled()
  })
})
