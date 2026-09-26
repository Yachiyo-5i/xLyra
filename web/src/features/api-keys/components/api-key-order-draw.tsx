import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ArrowDown, ArrowUp, GripVertical, LoaderCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Draw, DrawBody, DrawContent, DrawDescription, DrawFooter, DrawHeader, DrawTitle } from '@/components/ui/draw'
import { downstreamAPIKeyQueryKeys, listDownstreamAPIKeys, reorderDownstreamAPIKeys } from '@/features/api-keys/api/api-keys'
import { moveAPIKey } from '@/features/api-keys/lib/api-key-order'
import { analyticsQueryKeys } from '@/features/analytics/api/analytics'
import { toast } from '@/lib/toast'

export function APIKeyOrderDraw({ initialData, onClose }: {
  initialData: Awaited<ReturnType<typeof listDownstreamAPIKeys>>
  onClose: () => void
}) {
  const { t } = useTranslation('api-keys')
  const queryClient = useQueryClient()
  const [snapshot] = useState(initialData)
  const [items, setItems] = useState(initialData.items)
  const [draggedId, setDraggedId] = useState<string | null>(null)
  const saveMutation = useMutation({
    mutationFn: () => reorderDownstreamAPIKeys(items.map((item) => item.id)),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: downstreamAPIKeyQueryKeys.all }),
        queryClient.invalidateQueries({ queryKey: analyticsQueryKeys.options() }),
        queryClient.invalidateQueries({ queryKey: ['traffic-flow', 'topology'] }),
      ])
      toast.success(t('order.saved'))
      onClose()
    },
    onError: (error) => toast.error(t('order.failed'), { description: error.message }),
  })
  const pending = saveMutation.isPending
  const changed = items.some((item, index) => item.id !== snapshot.items[index]?.id)

  return (
    <Draw open onOpenChange={(open) => { if (!open && !pending) onClose() }}>
      <DrawContent side="right">
        <DrawHeader>
          <DrawTitle>{t('order.title')}</DrawTitle>
          <DrawDescription>{t('order.description')}</DrawDescription>
        </DrawHeader>
        <DrawBody className="space-y-2">
          <ol className="space-y-2" aria-label={t('order.title')}>
            {items.map((item, index) => (
              <li
                key={item.id}
                className="flex items-center gap-2 rounded-lg border border-[hsl(var(--glass-border))] p-2"
                onDragOver={(event) => { if (draggedId && !pending) event.preventDefault() }}
                onDrop={(event) => {
                  event.preventDefault()
                  if (draggedId && !pending) setItems((current) => moveAPIKey(current, draggedId, item.id))
                  setDraggedId(null)
                }}
              >
                <Button
                  variant="ghost"
                  size="icon"
                  draggable={!pending}
                  disabled={pending}
                  aria-label={t('order.drag', { name: item.name })}
                  onDragStart={(event) => {
                    event.dataTransfer.setData('text/plain', item.id)
                    event.dataTransfer.effectAllowed = 'move'
                    const row = event.currentTarget.closest('li')
                    if (row) {
                      const bounds = row.getBoundingClientRect()
                      event.dataTransfer.setDragImage(row, event.clientX - bounds.left, event.clientY - bounds.top)
                    }
                    setDraggedId(item.id)
                  }}
                  onDragEnd={() => setDraggedId(null)}
                  onKeyDown={(event) => {
                    const target = event.key === 'ArrowUp' ? items[index - 1] : event.key === 'ArrowDown' ? items[index + 1] : undefined
                    if (target) {
                      event.preventDefault()
                      setItems((current) => moveAPIKey(current, item.id, target.id))
                    }
                  }}
                >
                  <GripVertical className="h-4 w-4" />
                </Button>
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm font-medium">{item.name}</div>
                  <div className="truncate text-xs text-muted-foreground">{item.masked_key}</div>
                </div>
                <Button variant="ghost" size="icon" disabled={pending || index === 0} aria-label={t('order.up', { name: item.name })} onClick={() => setItems((current) => moveAPIKey(current, item.id, items[index - 1].id))}>
                  <ArrowUp className="h-4 w-4" />
                </Button>
                <Button variant="ghost" size="icon" disabled={pending || index === items.length - 1} aria-label={t('order.down', { name: item.name })} onClick={() => setItems((current) => moveAPIKey(current, item.id, items[index + 1].id))}>
                  <ArrowDown className="h-4 w-4" />
                </Button>
              </li>
            ))}
          </ol>
        </DrawBody>
        <DrawFooter className="flex justify-start gap-2">
          <Button disabled={pending || !changed} onClick={() => saveMutation.mutate()}>
            {pending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}
            {t('form.actions.save')}
          </Button>
          <Button variant="ghost" disabled={pending} onClick={onClose}>{t('workspace.deleteDialog.cancel')}</Button>
        </DrawFooter>
      </DrawContent>
    </Draw>
  )
}
