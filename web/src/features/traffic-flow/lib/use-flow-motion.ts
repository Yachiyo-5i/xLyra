import { useEffect, useRef, useState, type MutableRefObject } from 'react'
import type { TrafficFlowRequest } from '@/features/traffic-flow/api/traffic-flow'
import {
  emptyFlowMotion,
  pulsePoints,
  sameLitKeys,
  stepFlowMotion,
  type FlowMotionState,
  type FlowPulse,
} from '@/features/traffic-flow/lib/flow-packets'
import type { LaneKind } from '@/features/traffic-flow/lib/wing-layout'

export type FlowPulseView = {
  id: string
  requestId: string
  role: FlowPulse['role']
  color: string
  x: number
  y: number
  trail: Array<{ x: number; y: number }>
}

export type FlowSeat = {
  x: number
  y: number
  kind: LaneKind
}

type UseFlowMotionOptions = {
  requests: TrafficFlowRequest[]
  retiringRequestIDs: Set<string>
  seatsRef: MutableRefObject<Map<string, FlowSeat>>
  sizeRef: MutableRefObject<{ width: number; height: number }>
  paused: boolean
  reducedMotion: boolean
  onDrained: (requestID: string) => void
}

const emptyView = {
  pulses: [] as FlowPulseView[],
  litKeys: new Set<string>(),
  active: false,
}

export function useFlowMotion({
  requests,
  retiringRequestIDs,
  seatsRef,
  sizeRef,
  paused,
  reducedMotion,
  onDrained,
}: UseFlowMotionOptions) {
  const [view, setView] = useState(emptyView)
  const stateRef = useRef<FlowMotionState>(emptyFlowMotion())
  const clockRef = useRef({ origin: 0, pausedAt: 0, running: false })
  const requestsRef = useRef(requests)
  const retiringRef = useRef(retiringRequestIDs)
  const pausedRef = useRef(paused)
  const reducedRef = useRef(reducedMotion)
  const onDrainedRef = useRef(onDrained)
  const litRef = useRef(emptyView.litKeys)
  const signatureRef = useRef('')
  requestsRef.current = requests
  retiringRef.current = retiringRequestIDs
  pausedRef.current = paused
  reducedRef.current = reducedMotion
  onDrainedRef.current = onDrained

  useEffect(() => {
    const clockNow = () => {
      const clock = clockRef.current
      if (!clock.running) {
        clock.origin = performance.now()
        clock.running = true
      }
      if (pausedRef.current) {
        if (!clock.pausedAt) clock.pausedAt = performance.now()
        return clock.pausedAt - clock.origin
      }
      if (clock.pausedAt) {
        clock.origin += performance.now() - clock.pausedAt
        clock.pausedAt = 0
      }
      return performance.now() - clock.origin
    }

    const tick = () => {
      const now = clockNow()
      const stepped = stepFlowMotion(stateRef.current, requestsRef.current, retiringRef.current, now, reducedRef.current)
      stateRef.current = stepped.state
      const pulses: FlowPulseView[] = []
      for (const pulse of stepped.state.pulses) {
        const points = pulsePoints(pulse, seatsRef.current, sizeRef.current, now)
        if (!points) continue
        pulses.push({
          id: pulse.id,
          requestId: pulse.requestId,
          role: pulse.role,
          color: pulse.color,
          x: points.head.x,
          y: points.head.y,
          trail: points.trail,
        })
      }
      const litKeys = sameLitKeys(litRef.current, stepped.litKeys) ? litRef.current : stepped.litKeys
      litRef.current = litKeys
      const active = pulses.length > 0 || litKeys.size > 0
      const signature = `${pulses.map((pulse) => `${pulse.id}:${Math.round(pulse.x)}:${Math.round(pulse.y)}`).join('|')}:${[...litKeys].join(',')}:${active}`
      if (signatureRef.current !== signature) {
        signatureRef.current = signature
        setView({ pulses, litKeys, active })
      }
      if (stepped.drained.length > 0) {
        const ids = stepped.drained
        queueMicrotask(() => {
          for (const id of ids) onDrainedRef.current(id)
        })
      }
    }

    tick()
    let frame = window.requestAnimationFrame(function loop() {
      tick()
      frame = window.requestAnimationFrame(loop)
    })
    return () => window.cancelAnimationFrame(frame)
  }, [seatsRef, sizeRef])

  return view
}
