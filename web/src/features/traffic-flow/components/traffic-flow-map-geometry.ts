import type { TrafficFlowNode } from '@/features/traffic-flow/api/traffic-flow'

export type FlowNode = TrafficFlowNode & { kind: 'downstream' | 'gateway' | 'upstream' }

export function nodePosition(node: FlowNode, nodes: FlowNode[]) {
  const sameKind = nodes.filter((item) => item.kind === node.kind)
  const index = sameKind.findIndex((item) => item.id === node.id)
  const y = sameKind.length <= 1 ? 50 : 15 + (index / (sameKind.length - 1)) * 70
  const x = node.kind === 'downstream' ? 13 : node.kind === 'gateway' ? 50 : 87
  return { x, y }
}
