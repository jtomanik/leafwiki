import { DialogManager } from '@/components/DialogManager'
import { HotKeyHandler } from '@/components/HotKeyHandler'
import { Button } from '@/components/ui/button'
import { TooltipProvider } from '@/components/ui/tooltip'
import UserToolbar from '@/components/UserToolbar'
import DesignToggle from '@/features/designtoggle/DesignToggle'
import { EditorTitleBar } from '@/features/editor/EditorTitleBar'
import {
  isDirtyState,
  usePageEditorStore,
} from '@/features/editor/pageEditorStore'
import { PageQuickSwitcherTrigger } from '@/features/page-switcher/PageQuickSwitcherTrigger'
import Progressbar from '@/features/progressbar/Progressbar'
import Sidebar from '@/features/sidebar/Sidebar'
import { Toolbar } from '@/features/toolbar/Toolbar'
import type { PresenceMode } from '@/lib/api/presence'
import { withBasePath } from '@/lib/routePath'
import { useAppMode, type AppMode } from '@/lib/useAppMode'
import { useAutoCloseSidebarOnMobile } from '@/lib/useAutoCloseSidebarOnMobile'
import { useIsMobile } from '@/lib/useIsMobile'
import { usePresenceHeartbeat } from '@/lib/usePresenceHeartbeat'
import { splitWorkspaceRoute } from '@/lib/workspaceRoute'
import { getWikiTargetRoutePath } from '@/lib/wikiPath'
import { useBrandingStore } from '@/stores/branding'
import { useTreeStore } from '@/stores/tree'
import {
  MAX_SIDEBAR_WIDTH,
  MIN_SIDEBAR_WIDTH,
  useSidebarStore,
} from '@/stores/sidebar'
import { useWorkspacesStore } from '@/stores/workspaces'
import { MenuIcon } from 'lucide-react'
import React, { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { Link, useLocation } from 'react-router-dom'

export const MOBILE_SIDEBAR_WIDTH = 320

export default function AppLayout({ children }: { children: React.ReactNode }) {
  const appMode = useAppMode()
  const location = useLocation()
  const setActiveWorkspaceId = useWorkspacesStore((s) => s.setActiveWorkspaceId)
  const ensureWorkspaceExpanded = useWorkspacesStore(
    (s) => s.ensureWorkspaceExpanded,
  )
  const reloadTree = useTreeStore((s) => s.reloadTree)
  const [isEditor, setIsEditor] = useState(appMode === 'edit')
  const editorDirty = usePageEditorStore(isDirtyState)

  // store resize handler in onMouseMove, onMouseUp in useRef
  const resizeHandlerRef = useRef<{
    onMouseMove: (e: MouseEvent) => void
    onMouseUp: (e: MouseEvent) => void
  } | null>(null)

  const [resizing, setResizing] = useState(false)
  const [hoveringResize, setHoveringResize] = useState(false)

  const sidebarVisible = useSidebarStore((s) => s.sidebarVisible)
  const setSidebarVisible = useSidebarStore((s) => s.setSidebarVisible)
  const sidebarWidth = useSidebarStore((s) => s.sidebarWidth)
  const setSidebarWidth = useSidebarStore((s) => s.setSidebarWidth)
  const isMobile = useIsMobile()
  const isPrintCycleRef = useRef(false)
  const sidebarVisibleBeforePrintRef = useRef<boolean | null>(null)
  const routeWorkspace = splitWorkspaceRoute(location.pathname)

  useAutoCloseSidebarOnMobile()
  useEffect(() => {
    const workspaceId = routeWorkspace.workspaceId
    let cancelled = false

    setActiveWorkspaceId(workspaceId)

    const loadRouteWorkspace = async () => {
      try {
        await ensureWorkspaceExpanded(workspaceId)
        if (cancelled) return
        const treeState = useTreeStore.getState().getWorkspaceState(workspaceId)
        if (!treeState.tree && !treeState.loading) {
          await reloadTree(workspaceId)
        }
      } catch (err) {
        console.error('Failed to load route workspace', err)
      }
    }

    void loadRouteWorkspace()

    return () => {
      cancelled = true
    }
  }, [
    ensureWorkspaceExpanded,
    location.pathname,
    reloadTree,
    routeWorkspace.workspaceId,
    setActiveWorkspaceId,
  ])
  usePresenceHeartbeat({
    mode: presenceModeForAppMode(appMode),
    path: presencePathForAppMode(appMode, location.pathname),
    dirty: appMode === 'edit' && editorDirty,
    workspaceId: routeWorkspace.workspaceId,
  })

  const { siteName, logoFile, logoVersion } = useBrandingStore()

  const sidebarContainerRef = useRef<HTMLDivElement | null>(null)
  const liveSidebarWidthRef = useRef(sidebarWidth)

  const handleSidebarResize = (e: React.MouseEvent<HTMLDivElement>) => {
    if (!sidebarVisible || isMobile) return

    e.preventDefault()
    e.stopPropagation()

    const startX = e.clientX
    const startWidth = sidebarWidth

    const onMouseMove = (moveEvent: MouseEvent) => {
      const delta = moveEvent.clientX - startX

      const viewportWidth = window.innerWidth
      const maxWidth = Math.min(viewportWidth - 320, MAX_SIDEBAR_WIDTH) // min. 320px for main content should remain
      const minWidth = MIN_SIDEBAR_WIDTH

      const nextWidth = Math.min(
        maxWidth,
        Math.max(minWidth, startWidth + delta),
      )
      liveSidebarWidthRef.current = nextWidth

      if (sidebarContainerRef.current) {
        sidebarContainerRef.current.style.width = `${nextWidth}px`
      }
    }

    const onMouseUp = () => {
      setSidebarWidth(liveSidebarWidthRef.current)
      setResizing(false)
      setHoveringResize(false)
      resizeHandlerRef.current = null
    }

    resizeHandlerRef.current = { onMouseMove, onMouseUp }
    setResizing(true)
  }

  useLayoutEffect(() => {
    // Update sidebar visibility on mobile change
    if (isMobile && !isPrintCycleRef.current) setSidebarVisible(false)
  }, [isMobile, setSidebarVisible])

  useEffect(() => {
    const handleBeforePrint = () => {
      isPrintCycleRef.current = true
      sidebarVisibleBeforePrintRef.current = sidebarVisible
    }

    const handleAfterPrint = () => {
      const sidebarVisibleBeforePrint = sidebarVisibleBeforePrintRef.current

      if (sidebarVisibleBeforePrint !== null) {
        setSidebarVisible(sidebarVisibleBeforePrint)
      }

      sidebarVisibleBeforePrintRef.current = null

      requestAnimationFrame(() => {
        isPrintCycleRef.current = false
      })
    }

    window.addEventListener('beforeprint', handleBeforePrint)
    window.addEventListener('afterprint', handleAfterPrint)

    return () => {
      window.removeEventListener('beforeprint', handleBeforePrint)
      window.removeEventListener('afterprint', handleAfterPrint)
    }
  }, [setSidebarVisible, sidebarVisible])

  useEffect(() => {
    if (!resizing || !resizeHandlerRef.current) return

    const { onMouseMove, onMouseUp } = resizeHandlerRef.current

    document.addEventListener('mousemove', onMouseMove)
    document.addEventListener('mouseup', onMouseUp)

    return () => {
      document.removeEventListener('mousemove', onMouseMove)
      document.removeEventListener('mouseup', onMouseUp)
    }
  }, [resizing])

  // cleanup on unmount
  useEffect(() => {
    return () => {
      if (resizeHandlerRef.current) {
        const { onMouseMove, onMouseUp } = resizeHandlerRef.current
        document.removeEventListener('mousemove', onMouseMove)
        document.removeEventListener('mouseup', onMouseUp)
      }
    }
  }, [])

  useEffect(() => {
    const frame = requestAnimationFrame(() => {
      setIsEditor(appMode === 'edit')
    })
    return () => cancelAnimationFrame(frame)
  }, [appMode])

  useEffect(() => {
    liveSidebarWidthRef.current = sidebarWidth
  }, [sidebarWidth])

  let mainContainerStyle = !isEditor
    ? 'custom-scrollbar app-layout__main-content-area-viewer'
    : 'app-layout__main-content-area-editor'

  // If on mobile and sidebar is visible, hide overflow to prevent double scrollbars
  if (isMobile && sidebarVisible) {
    mainContainerStyle += ' overflow-hidden'
  }

  const effectiveSidebarWidth = !sidebarVisible
    ? 0
    : isMobile
      ? MOBILE_SIDEBAR_WIDTH
      : sidebarWidth

  return (
    <TooltipProvider delayDuration={300}>
      <Progressbar />
      <HotKeyHandler />
      <DialogManager />
      {/* Header */}
      <header className="app-layout__header">
        <div className="app-layout__header-inner">
          <div className="app-layout__sidebar-toggle-container">
            {/* Sidebar Toggle Button */}
            <Button
              variant={'outline'}
              className="app-layout__sidebar-toggle-button"
              onClick={() => setSidebarVisible(!sidebarVisible)}
              aria-label="Toggle Sidebar"
              aria-expanded={sidebarVisible}
              data-testid="sidebar-toggle-button"
            >
              <MenuIcon className="app-layout__sidebar-toggle-button-icon" />
            </Button>
          </div>
          <div className="app-layout__logo-n-title">
            <h2>
              <Link to="/">
                {logoFile ? (
                  <img
                    src={`${withBasePath(`/branding/${logoFile}`)}?v=${logoVersion}`}
                    alt={siteName}
                    className="app-layout__logo-image"
                  />
                ) : (
                  <span className="app-layout__logo-emoji">🌿</span>
                )}{' '}
                <span className="app-layout__site-name max-md:hidden">
                  {siteName}
                </span>
              </Link>
            </h2>
          </div>
          <div className="app-layout__editor-title-bar-container">
            <EditorTitleBar />
          </div>
          <div className="app-layout__editor-toolbar-container">
            <PageQuickSwitcherTrigger />
            <DesignToggle />
            <Toolbar />
            <UserToolbar />
          </div>
        </div>
      </header>
      <div className="app-layout__header-spacer" />
      <div className="app-layout__content-wrapper">
        <div
          ref={sidebarContainerRef}
          id="sidebar-container"
          className={
            'app-layout__sidebar-container ' +
            (resizing ? '' : ' transition-[width] duration-200')
          }
          style={{
            width: effectiveSidebarWidth,
            pointerEvents: sidebarVisible ? 'auto' : 'none',
            marginLeft:
              isMobile && !sidebarVisible
                ? '-4px' /* is used to prevent a border when the sidebar is closed */
                : '',
          }}
        >
          {!isMobile && sidebarVisible && (
            <div
              className="app-layout__sidebar-resizer"
              onMouseDown={handleSidebarResize}
              onMouseEnter={() => setHoveringResize(true)}
              onMouseLeave={() => {
                if (!resizeHandlerRef.current) setHoveringResize(false)
              }}
              role="separator"
              aria-orientation="vertical"
              aria-label="Resize sidebar"
              data-testid="sidebar-resize-handle"
            >
              <div
                className={
                  'app-layout__sidebar-resize-handle ' +
                  (hoveringResize || resizing
                    ? 'app-layout__sidebar-resize-handle-hover'
                    : 'app-layout__sidebar-resize-handle-default')
                }
              />
            </div>
          )}
          <Sidebar />
        </div>

        {/* Overlay for mobile sidebar */}
        {isMobile && sidebarVisible && (
          <div className="app-layout__sidebar-overlay-mobile" />
        )}
        <div className="app-layout__main-column">
          <div id="app-subheader-root" className="app-layout__subheader-root" />
          {/* Main content area */}
          <main
            className={`${mainContainerStyle} app-layout__main-content-area`}
            id="scroll-container"
          >
            {children}
          </main>
        </div>
      </div>
    </TooltipProvider>
  )
}

function presenceModeForAppMode(appMode: AppMode): PresenceMode {
  if (appMode === 'user-management') return 'settings'
  if (appMode === 'dialog') return 'unknown'
  return appMode
}

function presencePathForAppMode(appMode: AppMode, pathname: string) {
  if (appMode !== 'view' && appMode !== 'edit' && appMode !== 'history') {
    return undefined
  }
  const viewPath = getWikiTargetRoutePath(pathname)
  return viewPath === '/' ? undefined : viewPath
}
