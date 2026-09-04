import { useTranslation } from 'react-i18next'
import { LoaderCircle } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { AgentLiquidGlassPanel, type AgentLiquidGlassSettings } from '@/features/agent/components/liquid-glass/agent-liquid-glass'
import { agentDialogGlassDefaults } from '@/features/agent/components/agent-dialog-material'

type AgentConfirmDialogProps = {
  open: boolean
  title: string
  description: string
  confirmLabel: string
  backgroundImage: string
  /** 会话中为 true：与页面一致渲染深色玻璃 */
  dark: boolean
  /** 玻璃着色器参数：传侧栏/页面的 glassSettings 与之同款，不传用默认 */
  glassSettings?: AgentLiquidGlassSettings
  pending?: boolean
  destructive?: boolean
  onCancel: () => void
  onConfirm: () => void
}

/**
 * Agent 页面专用的确认弹窗：不复用主程序的通用 ConfirmDialog，改用本页面的
 * 液态玻璃面板材质（与设置/权限弹窗同一套）。弹层实体仍是 DialogContent，
 * 表面由 agent 自己的 AgentLiquidGlassPanel 承担。
 */
export function AgentConfirmDialog({
  open,
  title,
  description,
  confirmLabel,
  backgroundImage,
  dark,
  glassSettings,
  pending = false,
  destructive,
  onCancel,
  onConfirm,
}: AgentConfirmDialogProps) {
  const { t } = useTranslation('common')

  return (
    <Dialog open={open} onOpenChange={(nextOpen) => { if (!nextOpen) onCancel() }}>
      <DialogContent className="agent-liquid-access-dialog-host w-[min(92vw,440px)]">
        <AgentLiquidGlassPanel
          backgroundImage={backgroundImage}
          variant={dark ? 'dark' : 'frosted'}
          sampleBackground={!dark}
          className="agent-liquid-access-dialog"
          contentClassName="agent-liquid-access-dialog__content"
          settings={{ ...agentDialogGlassDefaults(dark), ...glassSettings }}
        >
          <DialogHeader>
            <DialogTitle>{title}</DialogTitle>
            <DialogDescription>{description}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={onCancel} disabled={pending}>
              {t('actions.cancel')}
            </Button>
            <Button variant={destructive ? 'destructive' : 'default'} onClick={onConfirm} disabled={pending}>
              {pending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}
              {confirmLabel}
            </Button>
          </DialogFooter>
        </AgentLiquidGlassPanel>
      </DialogContent>
    </Dialog>
  )
}
