import { Link, useRouter, useRouterState } from '@tanstack/react-router'
import {
  ChevronRight,
  HelpCircle,
  Loader2,
  LogOut,
  RefreshCw,
  ShieldAlert,
  UserRound,
} from 'lucide-react'
import { type PropsWithChildren, type ReactNode, useEffect, useMemo, useRef, useState } from 'react'

import { apiClient } from '@/api/client'
import { AppVersionBadge } from '@/components/common/app-version-badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { adminShellAccess } from '@/lib/access'
import type { PermissionRequirement } from '@/lib/permissions'
import { canAccess } from '@/lib/permissions'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'
import { usePageTransitionStore } from '@/stores/page-transition-store'

const pathLabels: Record<string, string> = {
  '/chat': '智能问答',
  '/qa': '智能问答',
  '/knowledge': '知识检索',
  '/reports': '报告生成',
  '/admin': '系统管理',
  '/profile': '个人资料',
  '/forbidden': '权限不足',
}

const navItems: Array<{
  label: string
  to: '/chat' | '/qa/retrieval-test' | '/knowledge/search' | '/reports' | '/admin'
  requirement?: PermissionRequirement
}> = [
  { label: '问答', to: '/chat', requirement: { any: ['qa:use'] } },
  { label: '检索测试', to: '/qa/retrieval-test', requirement: { any: ['qa:use'] } },
  { label: '知识', to: '/knowledge/search', requirement: { any: ['knowledge:read'] } },
  {
    label: '报告',
    to: '/reports',
    requirement: { any: ['report:read', 'report:write', 'reports:write'] },
  },
  {
    label: '管理',
    to: '/admin',
    requirement: adminShellAccess,
  },
]

function FullPageState({
  action,
  children,
  title,
}: PropsWithChildren<{ action?: ReactNode; title: string }>) {
  return (
    <div className="flex h-full items-center justify-center bg-background p-6 text-foreground">
      <section className="w-full max-w-md rounded-lg border border-border bg-card p-6 text-center shadow-sm">
        <h1 className="text-lg font-semibold">{title}</h1>
        <div className="mt-2 text-sm text-muted-foreground">{children}</div>
        {action && <div className="mt-5">{action}</div>}
      </section>
    </div>
  )
}

export function AppLayout({ children }: PropsWithChildren) {
  const router = useRouter()
  const routerState = useRouterState()
  const pathname = routerState.location.pathname
  const [helpOpen, setHelpOpen] = useState(false)
  const currentLabel =
    Object.entries(pathLabels).find(([key]) => pathname.startsWith(key))?.[1] ?? '首页'
  const user = useAuthStore((state) => state.user)
  const status = useAuthStore((state) => state.status)
  const error = useAuthStore((state) => state.error)
  const restoreSession = useAuthStore((state) => state.restoreSession)

  // Page transition entrance
  useEffect(() => {
    usePageTransitionStore.getState().reveal()
  }, [])

  // ── Nav slider position ──
  const visibleNavItems = useMemo(
    () => navItems.filter((item) => canAccess(user, item.requirement)),
    [user],
  )
  const navRefs = useRef<Record<string, HTMLAnchorElement | null>>({})
  const [sliderStyle, setSliderStyle] = useState<{ left: number; width: number }>({
    left: 0,
    width: 0,
  })

  useEffect(() => {
    const id = pathname.startsWith('/chat')
      ? '/chat'
      : pathname.startsWith('/qa/retrieval-test')
        ? '/qa/retrieval-test'
        : pathname.startsWith('/knowledge')
          ? '/knowledge/search'
          : pathname.startsWith('/reports')
            ? '/reports'
            : '/admin'
    const raf = requestAnimationFrame(() => {
      const el = navRefs.current[id]
      if (el) {
        const parentRect = el.parentElement!.getBoundingClientRect()
        const elRect = el.getBoundingClientRect()
        const newLeft = elRect.left - parentRect.left
        const newWidth = elRect.width
        setSliderStyle((prev) =>
          prev.left === newLeft && prev.width === newWidth
            ? prev
            : { left: newLeft, width: newWidth },
        )
      }
    })
    return () => cancelAnimationFrame(raf)
  }, [pathname, visibleNavItems])

  if (status === 'restoring' || status === 'idle') {
    return (
      <FullPageState title="正在恢复会话">
        <span className="inline-flex items-center gap-2">
          <Loader2 className="size-4 animate-spin" />
          正在读取当前用户信息
        </span>
      </FullPageState>
    )
  }

  if (status === 'error') {
    return (
      <FullPageState
        action={
          <Button variant="outline" onClick={() => void restoreSession()}>
            <RefreshCw className="size-4" />
            重试
          </Button>
        }
        title="会话恢复失败"
      >
        {error ?? '无法读取当前用户，请稍后重试。'}
      </FullPageState>
    )
  }

  const handleLogout = () => {
    const token = apiClient.getToken()
    useAuthStore.getState().clearSession()
    void router.navigate({ to: '/login' })
    // Fire-and-forget: end the server session with captured token
    if (token && token !== 'dev-token-bypass') {
      void fetch(`${apiClient.baseUrl}/sessions/current`, {
        method: 'DELETE',
        headers: { Authorization: `Bearer ${token}` },
      }).catch(() => {
        /* best-effort */
      })
    }
  }

  return (
    <div className="flex h-full flex-col bg-background text-foreground">
      <header className="flex h-14 items-center justify-between border-b border-primary/30 bg-primary/5 px-6">
        <div className="flex min-w-0 items-center gap-3">
          <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-primary text-sm font-semibold text-primary-foreground">
            电
          </div>
          <span className="truncate text-sm text-primary">电力行业知识助手</span>
          <span className="truncate text-sm font-semibold">{currentLabel}</span>
          <AppVersionBadge className="hidden shrink-0 lg:inline-flex" />
        </div>

        <nav
          aria-label="主导航"
          className="relative flex items-center gap-1 rounded-lg bg-muted/50 p-1 text-sm"
        >
          {/* Sliding pill — only visible when on a main nav item, not /profile */}
          {visibleNavItems.some((item) => pathname.startsWith(item.to)) && (
            <div
              aria-hidden
              className="absolute top-1 h-[calc(100%-8px)] rounded-md bg-background shadow-sm transition-all duration-300 ease-out"
              style={{ left: sliderStyle.left, width: sliderStyle.width }}
            />
          )}
          {visibleNavItems.map((item) => (
            <Link
              key={item.to}
              to={item.to}
              ref={(el) => {
                navRefs.current[item.to] = el as HTMLAnchorElement | null
              }}
              className="relative z-10 rounded-md px-3 py-1.5 transition-colors hover:text-foreground"
              activeProps={{ className: 'text-foreground font-medium' }}
              inactiveProps={{ className: 'text-muted-foreground' }}
            >
              {item.label}
            </Link>
          ))}
        </nav>

        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          <Button
            aria-label="打开帮助"
            size="icon-sm"
            title="帮助"
            type="button"
            variant="ghost"
            onClick={() => setHelpOpen(true)}
          >
            <HelpCircle />
          </Button>
          <Link
            aria-label="打开个人资料"
            className={cn(
              'hidden max-w-40 items-center gap-1 rounded-md border border-border bg-background px-2 py-1 shadow-sm transition-all hover:border-ring/30 hover:shadow hover:text-foreground sm:inline-flex',
              pathname === '/profile' &&
                'border-primary/40 bg-primary/10 text-primary shadow-none font-medium',
            )}
            to="/profile"
          >
            <UserRound className="size-3.5" />
            <span className="truncate">{user?.displayName || user?.username || '未登录'}</span>
            <ChevronRight className="size-3 shrink-0 opacity-50" />
          </Link>
          {user && user.roles.length > 0 && (
            <span className="hidden rounded-md bg-muted px-2 py-1 sm:inline">
              {user.roles.join(', ')}
            </span>
          )}
          {!visibleNavItems.length && (
            <span className="inline-flex items-center gap-1 text-destructive">
              <ShieldAlert className="size-3.5" />
              无可用菜单
            </span>
          )}
          <Button
            aria-label="退出登录"
            size="icon-sm"
            type="button"
            variant="ghost"
            onClick={handleLogout}
          >
            <LogOut />
          </Button>
        </div>
      </header>

      <main
        key={pathname}
        className="page-enter-right flex-1 overflow-y-auto overflow-x-hidden"
        style={{ scrollbarGutter: 'stable' }}
      >
        {children}
      </main>

      <Dialog open={helpOpen} onOpenChange={setHelpOpen}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>Helpme</DialogTitle>
            <DialogDescription>常用流程的最短路径。</DialogDescription>
          </DialogHeader>
          <div className="grid gap-3 text-sm">
            <section className="rounded-lg border border-border p-3">
              <h2 className="font-medium text-foreground">首次配置</h2>
              <ol className="mt-2 list-decimal space-y-1 pl-5 text-muted-foreground">
                <li>进入管理，新增并启用用途为 chat 的模型 Profile。</li>
                <li>进入管理的系统设置，发布当前 QA LLM 配置。</li>
                <li>进入报告生成页，发布文档生成模型配置。</li>
              </ol>
            </section>
            <section className="rounded-lg border border-border p-3">
              <h2 className="font-medium text-foreground">开始问答</h2>
              <ol className="mt-2 list-decimal space-y-1 pl-5 text-muted-foreground">
                <li>先在知识库上传并等待文档处理完成。</li>
                <li>回到问答页新建会话，输入问题后发送。</li>
              </ol>
            </section>
            <section className="rounded-lg border border-border p-3">
              <h2 className="font-medium text-foreground">生成报告</h2>
              <ol className="mt-2 list-decimal space-y-1 pl-5 text-muted-foreground">
                <li>在报告模板页上传 DOCX 模板。</li>
                <li>进入报告生成页，选择报告类型、模板和参数。</li>
                <li>生成大纲，确认后继续生成正文并导出 DOCX。</li>
              </ol>
            </section>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  )
}
