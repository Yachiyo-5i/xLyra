import { useMobileLayout } from '@/hooks/use-media-query'

/** Class names so nested confirm/login dialogs stack above the account list dialog. */
export function useNestedDialogClassNames() {
  const isMobile = useMobileLayout()
  return {
    overlayClassName: isMobile ? undefined : 'z-[60]',
    className: isMobile ? undefined : 'z-[60]',
  }
}
