import { Link, useRouterState } from '@tanstack/react-router'
import {
  BarChart3,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  Database,
  FileText,
  LayoutTemplate,
  MessageSquareText,
  Package,
  Settings,
  UsersRound,
  Wrench,
} from 'lucide-react'
import { useMemo, useState } from 'react'

import type { PermissionRequirement } from '@/lib/permissions'
import { canAccess } from '@/lib/permissions'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'
import { useUiStore } from '@/stores/ui-store'

type AdminNavigationItem = {
  children?: AdminNavigationItem[]
  key: string
  label: string
  path?: string
  requirement?: PermissionRequirement
}

const menuItems: AdminNavigationItem[] = [
  {
    key: 'system',
    label: '系统管理',
    children: [
      {
        key: 'users',
        label: '用户管理',
        path: '/admin/users',
        requirement: { roles: ['admin', 'super_admin'] },
      },
      {
        key: 'prompts',
        label: 'Agent 提示词',
        path: '/admin/prompts',
        requirement: { any: ['qa:settings:read'] },
      },
    ],
  },
  {
    key: 'reports',
    label: '报告生成',
    requirement: { any: ['report:read', 'report:write', 'reports:write'] },
    children: [
      { key: 'report-records', label: '报告记录', path: '/admin/reports/records' },
      {
        key: 'report-templates',
        label: '模板素材',
        path: '/admin/reports/templates',
        requirement: { any: ['report:write', 'reports:write'] },
      },
    ],
  },
  {
    key: 'rag',
    label: 'RAG 知识库',
    requirement: {
      any: [
        'knowledge:read',
        'system:admin',
        'knowledge:write',
        'knowledge:admin',
        'admin:model-profile:write',
        'admin:parser-config:write',
      ],
    },
    children: [
      {
        key: 'knowledge',
        label: '知识管理',
        path: '/admin/knowledge',
        requirement: { any: ['knowledge:write', 'knowledge:admin', 'system:admin'] },
      },
      {
        key: 'knowledge-documents',
        label: '文档管理',
        path: '/admin/knowledge/documents',
        requirement: { any: ['knowledge:read'] },
      },
      {
        key: 'knowledge-search',
        label: '知识检索',
        path: '/admin/knowledge/search',
        requirement: { any: ['knowledge:read'] },
      },
      {
        key: 'knowledge-config',
        label: '知识配置',
        path: '/admin/knowledge-config',
        requirement: { any: ['knowledge:admin', 'knowledge:write', 'system:admin'] },
      },
      {
        key: 'qa-settings',
        label: 'QA / LLM 配置',
        path: '/admin/qa-settings',
        requirement: {
          any: ['admin:model-profile:write', 'admin:parser-config:write', 'system:admin'],
        },
      },
      {
        key: 'qa-retrieval-test',
        label: 'QA 检索测试',
        path: '/admin/qa-retrieval-test',
        requirement: {
          any: ['admin:model-profile:write', 'admin:parser-config:write', 'system:admin'],
        },
      },
      {
        key: 'model-profiles',
        label: '模型管理',
        path: '/admin/model-profiles',
        requirement: { any: ['admin:model-profile:write', 'system:admin'] },
      },
      {
        key: 'parser-configs',
        label: '解析器配置',
        path: '/admin/parser-configs',
        requirement: {
          any: ['admin:parser-config:write', 'knowledge:admin', 'system:admin'],
        },
      },
    ],
  },
  {
    key: 'stats',
    label: 'QA 统计',
    path: '/admin/stats',
    requirement: { any: ['system:admin'] },
  },
  {
    key: 'settings',
    label: '系统设置',
    path: '/admin/settings',
    requirement: { any: ['system:admin'] },
  },
]

const ICON_MAP: Record<string, typeof Settings> = {
  system: Settings,
  users: UsersRound,
  stats: BarChart3,
  reports: FileText,
  templates: LayoutTemplate,
  materials: Package,
  prompts: MessageSquareText,
  rag: Database,
  settings: Wrench,
}

function filterMenu(
  items: readonly AdminNavigationItem[],
  user: ReturnType<typeof useAuthStore.getState>['user'],
): AdminNavigationItem[] {
  return items
    .map((item) => {
      const children = item.children ? filterMenu(item.children, user) : undefined
      return { ...item, children }
    })
    .filter(
      (item) => canAccess(user, item.requirement) && (!item.children || item.children.length > 0),
    )
}

export function AdminSidebar() {
  const pathname = useRouterState({ select: (state) => state.location.pathname })
  const user = useAuthStore((state) => state.user)
  const visibleMenuItems = useMemo(() => filterMenu(menuItems, user), [user])
  const [expanded, setExpanded] = useState<Set<string>>(new Set(['system', 'reports', 'rag']))
  const sidebarCollapsed = useUiStore((s) => s.sidebarCollapsed)
  const toggleSidebar = useUiStore((s) => s.toggleSidebar)

  const toggle = (key: string) => {
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(key)) {
        next.delete(key)
      } else {
        next.add(key)
      }
      return next
    })
  }

  const handleGroupClick = (key: string) => {
    if (sidebarCollapsed) {
      toggleSidebar()
      setExpanded((prev) => {
        const next = new Set(prev)
        next.add(key)
        return next
      })
      return
    }

    toggle(key)
  }

  /** Exact match — for link highlighting. Avoids sibling prefix collision
   *  e.g. /admin/knowledge won't match /admin/knowledge/documents */
  const isExactActive = (path?: string): boolean => {
    if (!path) return false
    return pathname === path
  }

  /** Prefix match — for group indicators. Covers deep routes without menu items
   *  e.g. /admin/knowledge/chunks would still match /admin/knowledge */
  const isPrefixActive = (path?: string): boolean => {
    if (!path) return false
    return pathname === path || pathname.startsWith(`${path}/`)
  }

  const hasActiveChild = (item: AdminNavigationItem): boolean => {
    if (!item.children) return false
    return item.children.some((child) => isPrefixActive(child.path))
  }

  return (
    <aside
      className={cn(
        'flex flex-shrink-0 flex-col overflow-hidden border-r border-border bg-sidebar',
        'transition-[width] duration-300',
        sidebarCollapsed ? 'w-14' : 'w-56',
      )}
    >
      <div className="flex items-center border-b border-border">
        {!sidebarCollapsed && (
          <h2 className="flex-1 whitespace-nowrap px-4 py-3 text-sm font-semibold text-sidebar-foreground transition-opacity duration-300">
            管理面板
          </h2>
        )}
        <button
          aria-label={sidebarCollapsed ? '展开侧边栏' : '折叠侧边栏'}
          className={cn(
            'flex shrink-0 items-center justify-center text-muted-foreground transition-all hover:bg-accent hover:text-foreground',
            sidebarCollapsed ? 'mx-auto my-3 size-7 rounded-md' : 'mr-1 size-7 rounded-md',
          )}
          type="button"
          onClick={toggleSidebar}
        >
          {sidebarCollapsed ? (
            <ChevronRight className="size-4 transition-transform duration-300" />
          ) : (
            <ChevronLeft className="size-4 transition-transform duration-300" />
          )}
        </button>
      </div>

      <nav className="flex flex-1 flex-col gap-0.5 overflow-auto py-1">
        {visibleMenuItems.map((item) => {
          const hasChildren = item.children && item.children.length > 0
          const Icon = ICON_MAP[item.key]

          if (hasChildren) {
            const open = expanded.has(item.key) && !sidebarCollapsed
            return (
              <div key={item.key}>
                <button
                  className={cn(
                    'flex w-full items-center text-left text-sm font-medium text-sidebar-foreground transition-colors hover:bg-primary/5 hover:text-primary',
                    sidebarCollapsed ? 'justify-center px-0 py-2' : 'gap-1.5 px-4 py-2',
                  )}
                  title={sidebarCollapsed ? item.label : undefined}
                  type="button"
                  onClick={() => handleGroupClick(item.key)}
                >
                  {sidebarCollapsed ? (
                    Icon && (
                      <span
                        className={cn(
                          'inline-flex items-center justify-center rounded-full p-1.5 transition-colors',
                          hasActiveChild(item) && 'shadow-[0_0_0_2px_var(--primary)] bg-primary/5',
                        )}
                      >
                        <Icon className="size-5 shrink-0" />
                      </span>
                    )
                  ) : (
                    <>
                      {open ? (
                        <ChevronDown
                          aria-hidden="true"
                          className="shrink-0 text-muted-foreground"
                          size={12}
                        />
                      ) : (
                        <ChevronRight
                          aria-hidden="true"
                          className="shrink-0 text-muted-foreground"
                          size={12}
                        />
                      )}
                      <span
                        className={cn(
                          'inline-block h-1.5 w-1.5 shrink-0 rounded-full transition-colors',
                          hasActiveChild(item)
                            ? 'bg-primary shadow-[0_0_4px_var(--primary)]'
                            : 'bg-muted-foreground/30',
                        )}
                      />
                      <span className="whitespace-nowrap">{item.label}</span>
                    </>
                  )}
                </button>
                {open && (
                  <div className="bg-sidebar-accent/40 py-0.5 transition-[max-height] duration-200">
                    {item.children!.map((child) => (
                      <Link
                        key={child.key}
                        className={cn(
                          'relative block whitespace-nowrap px-4 py-1.5 pl-10 text-sm text-muted-foreground transition-colors hover:bg-primary/5 hover:text-primary',
                          isExactActive(child.path) &&
                            'border-l-[3px] border-l-primary bg-primary/5 font-medium text-primary pl-[37px]',
                        )}
                        to={child.path!}
                      >
                        {child.label}
                      </Link>
                    ))}
                  </div>
                )}
              </div>
            )
          }

          return (
            <Link
              key={item.key}
              className={cn(
                'relative flex items-center gap-2 text-sm font-medium text-sidebar-foreground transition-colors hover:bg-primary/5 hover:text-primary',
                sidebarCollapsed ? 'justify-center px-0 py-2' : 'px-4 py-2',
                isExactActive(item.path) &&
                  !sidebarCollapsed &&
                  'border-l-[3px] border-l-primary bg-primary/5 font-medium text-primary pl-[13px]',
                isExactActive(item.path) && sidebarCollapsed && 'bg-primary/5',
              )}
              title={sidebarCollapsed ? item.label : undefined}
              to={item.path!}
            >
              {sidebarCollapsed && Icon ? (
                <span
                  className={cn(
                    'inline-flex items-center justify-center rounded-full p-1.5 transition-colors',
                    isExactActive(item.path) && 'shadow-[0_0_0_2px_var(--primary)] bg-primary/5',
                  )}
                >
                  <Icon className="size-5 shrink-0" />
                </span>
              ) : (
                <>
                  <span
                    className={cn(
                      'inline-block h-1.5 w-1.5 shrink-0 rounded-full',
                      isExactActive(item.path)
                        ? 'bg-primary shadow-[0_0_4px_var(--primary)]'
                        : 'bg-muted-foreground/30',
                    )}
                  />
                  <span className="whitespace-nowrap">{item.label}</span>
                </>
              )}
            </Link>
          )
        })}
      </nav>
    </aside>
  )
}
