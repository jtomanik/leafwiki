/// <reference types="vite/client" />

import 'sonner'

declare global {
  const __APP_VERSION__: string
}

declare module '@fontsource/inter'

declare module 'sonner' {
  interface ToastT {
    messageId?: string
    importStatus?: string
    validationCode?: string
  }
}
