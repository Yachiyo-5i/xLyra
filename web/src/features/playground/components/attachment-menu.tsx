import { useRef, useState, type ReactNode } from 'react'
import * as PopoverPrimitive from '@radix-ui/react-popover'
import { useTranslation } from 'react-i18next'
import { Camera, FileText, Image as ImageIcon, Plus } from 'lucide-react'
import { useLiquidGlassRef } from '@/components/glass/use-liquid-glass'
import { menuButtonClass, popoverPanelClass } from '@/components/ui/field-styles'
import { cn } from '@/lib/utils'

type AttachmentKind = 'photo' | 'camera' | 'file'

type AttachmentMenuProps = {
  /** 菜单里提供哪些来源；图片模式只给照片与拍照 */
  kinds: AttachmentKind[]
  onFiles: (files: FileList | null) => void
  disabled?: boolean
  label?: string
}

/** 拍照只在触屏设备上有意义（桌面浏览器会退化成普通文件选择） */
function isTouchDevice() {
  return typeof window !== 'undefined' && window.matchMedia('(pointer: coarse)').matches
}

/**
 * 输入框左下角的「+」：点开一个小菜单选择附件来源。
 * 每一项背后是一个隐藏的 <input type="file">，用 accept / capture 让移动端浏览器直接跳到
 * 相册（image/*）、相机（capture="environment"）或文件 App（不限类型）。
 */
export function AttachmentMenu({ kinds, onFiles, disabled = false, label }: AttachmentMenuProps) {
  const { t } = useTranslation('playground')
  const [open, setOpen] = useState(false)
  const glassRef = useLiquidGlassRef<HTMLDivElement>('regular')
  const photoRef = useRef<HTMLInputElement>(null)
  const cameraRef = useRef<HTMLInputElement>(null)
  const fileRef = useRef<HTMLInputElement>(null)
  const visibleKinds = kinds.filter((kind) => kind !== 'camera' || isTouchDevice())

  const items: Record<AttachmentKind, { icon: ReactNode; label: string; input: React.RefObject<HTMLInputElement | null> }> = {
    photo: { icon: <ImageIcon />, label: t('attach.photo'), input: photoRef },
    camera: { icon: <Camera />, label: t('attach.camera'), input: cameraRef },
    file: { icon: <FileText />, label: t('attach.file'), input: fileRef },
  }

  const handleChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    onFiles(event.target.files)
    event.target.value = ''
  }

  return (
    <>
      <input ref={photoRef} type="file" accept="image/*" multiple className="hidden" onChange={handleChange} />
      <input ref={cameraRef} type="file" accept="image/*" capture="environment" className="hidden" onChange={handleChange} />
      <input ref={fileRef} type="file" multiple className="hidden" onChange={handleChange} />
      <PopoverPrimitive.Root open={open} onOpenChange={setOpen}>
        <PopoverPrimitive.Trigger asChild>
          <button
            type="button"
            disabled={disabled}
            aria-label={label ?? t('attach.menu')}
            title={label ?? t('attach.menu')}
            className="flex size-11 items-center justify-center rounded-full text-label-2 transition-colors active:bg-fill-3 active:text-label disabled:cursor-not-allowed disabled:opacity-40 data-[state=open]:bg-fill-3 data-[state=open]:text-label md:size-8 md:hover:bg-fill-3 md:hover:text-label"
          >
            <Plus className={cn('size-5 transition-transform duration-200 md:size-[18px]', open && 'rotate-45')} />
          </button>
        </PopoverPrimitive.Trigger>
        <PopoverPrimitive.Portal>
          <PopoverPrimitive.Content
            ref={glassRef}
            side="top"
            align="start"
            sideOffset={10}
            collisionPadding={12}
            className={cn(popoverPanelClass, 'z-[130] w-48 origin-(--radix-popover-content-transform-origin)')}
          >
            {visibleKinds.map((kind) => (
              <button
                key={kind}
                type="button"
                className={cn(menuButtonClass, '[&_svg]:size-4 [&_svg]:text-label-2')}
                onClick={() => {
                  setOpen(false)
                  items[kind].input.current?.click()
                }}
              >
                {items[kind].icon}
                <span className="min-w-0 flex-1">{items[kind].label}</span>
              </button>
            ))}
          </PopoverPrimitive.Content>
        </PopoverPrimitive.Portal>
      </PopoverPrimitive.Root>
    </>
  )
}
