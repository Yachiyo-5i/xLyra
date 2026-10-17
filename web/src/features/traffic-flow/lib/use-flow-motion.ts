import { useEffect, useRef, useState, type MutableRefObject } from 'react'
import type { TrafficFlowRequest } from '@/features/traffic-flow/api/traffic-flow'
import {
  emptyFlowMotion,
  pulseProgress,
  pulseSeatKey,
  sameLitKeys,
  stepFlowMotion,
  type FlowMotionState,
  type FlowPulse,
} from '@/features/traffic-flow/lib/flow-packets'
import type { LaneKind } from '@/features/traffic-flow/lib/wing-layout'

/** 一个正在航道上飞行的请求：只带「在哪条航道、飞到哪了」，具体怎么画由 Canvas 决定 */
export type FlowPulseView = {
  id: string
  requestId: string
  role: FlowPulse['role']
  color: string
  /** 所在航道（节点键） */
  seatKey: string
  /** 沿航道的位置，0 在节点端、1 在中枢端 */
  t: number
  /** 是否正驶向中枢 */
  toward: boolean
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
  paused: boolean
  reducedMotion: boolean
  onDrained: (requestID: string) => void
}

const emptyView = {
  litKeys: new Set<string>(),
  active: false,
}

export function useFlowMotion({
  requests,
  retiringRequestIDs,
  seatsRef,
  paused,
  reducedMotion,
  onDrained,
}: UseFlowMotionOptions) {
  const [view, setView] = useState(emptyView)
  // 每一帧的飞行位置放在 ref 里给 Canvas 读，不触发 React 重渲染；只有点亮的节点集合变化时才更新 state
  const pulsesRef = useRef<FlowPulseView[]>([])
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
        const seatKey = pulseSeatKey(pulse)
        if (!seatKey || !seatsRef.current.has(seatKey)) continue
        const progress = pulseProgress(pulse, now)
        const toward = pulse.hop === 0
        pulses.push({
          id: pulse.id,
          requestId: pulse.requestId,
          role: pulse.role,
          color: pulse.color,
          seatKey,
          t: toward ? progress : 1 - progress,
          toward,
        })
      }
      pulsesRef.current = pulses
      const litKeys = sameLitKeys(litRef.current, stepped.litKeys) ? litRef.current : stepped.litKeys
      litRef.current = litKeys
      const active = pulses.length > 0 || litKeys.size > 0
      const signature = `${[...litKeys].join(',')}:${active}`
      if (signatureRef.current !== signature) {
        signatureRef.current = signature
        setView({ litKeys, active })
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
  }, [seatsRef])

  return { pulsesRef, litKeys: view.litKeys, active: view.active }
}
