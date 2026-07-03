import {
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  redirect,
  useNavigate,
  useSearch,
} from '@tanstack/react-router'

import { DEFAULT_ADMIN_USERS_SEARCH, normalizeAdminUsersSearch } from '@/features/auth'
import { AppLayout } from '@/layouts/app-layout'
import { adminShellAccess } from '@/lib/access'
import type { PermissionRequirement } from '@/lib/permissions'
import { canAccess } from '@/lib/permissions'
import { KnowledgeConfig } from '@/pages/admin/knowledge-config'
import { KnowledgeManagement } from '@/pages/admin/knowledge-management'
import { ModelProfilesPage } from '@/pages/admin/model-profiles'
import { AdminPage } from '@/pages/admin/page'
import { ParserConfigsPage } from '@/pages/admin/parser-configs'
import { QASettings } from '@/pages/admin/qa-settings'
import { QASystemPromptPage } from '@/pages/admin/qa-system-prompt'
import { StatsOverviewPage } from '@/pages/admin/stats-overview'
import { SystemSettings } from '@/pages/admin/system-settings'
import { AdminUsersPage } from '@/pages/admin/users'
import { ForbiddenPage } from '@/pages/auth/forbidden'
import { KnowledgeChunksPage } from '@/pages/knowledge/chunks/page'
import { KnowledgeDocumentsPage } from '@/pages/knowledge/documents/page'
import { KnowledgeSearchPage } from '@/pages/knowledge/search/page'
import { LoginPage } from '@/pages/login/page'
import { PasswordChangeRequiredPage } from '@/pages/password/change-required'
import { ProfilePage } from '@/pages/profile/page'
import { ChatPage } from '@/pages/qa/chat/page'
import { QARetrievalTestPage } from '@/pages/qa/retrieval-test/page'
import { ReportGeneratePage } from '@/pages/reports/generate/page'
import { ReportRecordsPage } from '@/pages/reports/records/page'
import { ReportTemplatesPage } from '@/pages/reports/templates/page'
import { useAuthStore } from '@/stores/auth-store'

async function restoreAuthForRoute() {
  const store = useAuthStore.getState()

  if (store.status === 'idle' || (store.accessToken && !store.user)) {
    await store.restoreSession()
  }

  return useAuthStore.getState()
}

function requireAuth(
  requirement?: PermissionRequirement,
  options?: { allowPasswordChange?: boolean },
) {
  return async () => {
    const store = await restoreAuthForRoute()

    if (store.status === 'anonymous') {
      throw redirect({ to: '/login' })
    }

    if (store.status === 'error') {
      return
    }

    if (store.user?.mustChangePassword && !options?.allowPasswordChange) {
      throw redirect({ to: '/password/change-required' })
    }

    if (!canAccess(store.user, requirement)) {
      throw redirect({ to: '/forbidden' })
    }
  }
}

async function redirectToReportHome() {
  const store = await restoreAuthForRoute()

  if (store.user?.mustChangePassword) {
    throw redirect({ to: '/password/change-required' })
  }

  if (canAccess(store.user, reportWriteAccess)) {
    throw redirect({ to: '/reports/generate' })
  }

  if (canAccess(store.user, reportAccess)) {
    throw redirect({ to: '/reports/records' })
  }

  throw redirect({ to: '/forbidden' })
}

async function redirectToAppHome() {
  const store = await restoreAuthForRoute()

  if (store.user?.mustChangePassword) {
    throw redirect({ to: '/password/change-required' })
  }

  if (canAccess(store.user, qaAccess)) {
    throw redirect({ to: '/chat' })
  }

  if (canAccess(store.user, knowledgeReadAccess)) {
    throw redirect({ to: '/knowledge/search' })
  }

  if (canAccess(store.user, reportWriteAccess)) {
    throw redirect({ to: '/reports/generate' })
  }

  if (canAccess(store.user, reportAccess)) {
    throw redirect({ to: '/reports/records' })
  }

  if (canAccess(store.user, adminShellAccess)) {
    throw redirect({ to: '/admin' })
  }

  throw redirect({ to: '/forbidden' })
}

async function redirectToAdminHome() {
  const store = await restoreAuthForRoute()

  if (store.user?.mustChangePassword) {
    throw redirect({ to: '/password/change-required' })
  }

  if (canAccess(store.user, { any: ['system:admin'] })) {
    throw redirect({ to: '/admin/stats' })
  }

  if (canAccess(store.user, reportAccess)) {
    throw redirect({ to: '/admin/reports/records' })
  }

  if (canAccess(store.user, knowledgeWriteAccess)) {
    throw redirect({ to: '/admin/knowledge' })
  }

  if (canAccess(store.user, { any: ['knowledge:admin', 'knowledge:write', 'system:admin'] })) {
    throw redirect({ to: '/admin/knowledge-config' })
  }

  if (canAccess(store.user, qaSettingsReadAccess)) {
    throw redirect({ to: '/admin/prompts' })
  }

  if (canAccess(store.user, qaAdminAccess)) {
    throw redirect({ to: '/admin/qa-settings' })
  }

  if (canAccess(store.user, parserConfigsPerm)) {
    throw redirect({ to: '/admin/parser-configs' })
  }

  if (canAccess(store.user, adminUsersAccess)) {
    throw redirect({ to: '/admin/users', search: DEFAULT_ADMIN_USERS_SEARCH })
  }

  throw redirect({ to: '/forbidden' })
}

const qaAccess: PermissionRequirement = { any: ['qa:use'] }
const qaAdminAccess: PermissionRequirement = {
  any: ['admin:model-profile:write', 'admin:parser-config:write', 'system:admin'],
}
const qaSettingsReadAccess: PermissionRequirement = { any: ['qa:settings:read'] }
const systemAdminAccess: PermissionRequirement = { any: ['system:admin'] }
const reportAccess: PermissionRequirement = {
  any: ['report:read', 'report:write', 'reports:write'],
}
const reportWriteAccess: PermissionRequirement = { any: ['report:write', 'reports:write'] }
const knowledgeReadAccess: PermissionRequirement = { any: ['knowledge:read'] }
const knowledgeWriteAccess: PermissionRequirement = { any: ['knowledge:write'] }
const adminUsersAccess: PermissionRequirement = {
  roles: ['admin', 'super_admin'],
}

const rootRoute = createRootRoute({
  component: Outlet,
})

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: 'login',
  beforeLoad: async () => {
    const store = await restoreAuthForRoute()
    if (store.status === 'authenticated') {
      if (store.user?.mustChangePassword) {
        throw redirect({ to: '/password/change-required' })
      }
      await redirectToAppHome()
    }
  },
  component: LoginPage,
})

const passwordChangeRequiredRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: 'password/change-required',
  beforeLoad: requireAuth(undefined, { allowPasswordChange: true }),
  component: PasswordChangeRequiredPage,
})

const authenticatedRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: 'authenticated',
  beforeLoad: requireAuth(),
  component: () => (
    <AppLayout>
      <Outlet />
    </AppLayout>
  ),
})

const indexRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: '/',
  beforeLoad: redirectToAppHome,
})

const forbiddenRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: 'forbidden',
  component: ForbiddenPage,
})

const profileRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: 'profile',
  component: ProfilePage,
})

const chatRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: 'chat',
  beforeLoad: requireAuth(qaAccess),
  component: ChatPage,
})

const qaRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: 'qa',
  beforeLoad: requireAuth(qaAccess),
  component: Outlet,
})

const qaRetrievalTestRoute = createRoute({
  getParentRoute: () => qaRoute,
  path: 'retrieval-test',
  component: QARetrievalTestPage,
})

const reportsRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: 'reports',
  beforeLoad: requireAuth(reportAccess),
  component: Outlet,
})

const reportsIndexRoute = createRoute({
  getParentRoute: () => reportsRoute,
  path: '/',
  beforeLoad: redirectToReportHome,
})

const reportGenerateRoute = createRoute({
  getParentRoute: () => reportsRoute,
  path: 'generate',
  beforeLoad: requireAuth(reportWriteAccess),
  component: ReportGeneratePage,
})

const reportRecordsRoute = createRoute({
  getParentRoute: () => reportsRoute,
  path: 'records',
  component: ReportRecordsPage,
})

const reportTemplatesRoute = createRoute({
  getParentRoute: () => reportsRoute,
  path: 'templates',
  beforeLoad: requireAuth(reportWriteAccess),
  component: ReportTemplatesPage,
})

const knowledgeRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: 'knowledge',
  beforeLoad: requireAuth(knowledgeReadAccess),
  component: Outlet,
})

const knowledgeSearchRoute = createRoute({
  getParentRoute: () => knowledgeRoute,
  path: 'search',
  component: KnowledgeSearchPage,
})

const adminRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: 'admin',
  beforeLoad: requireAuth(adminShellAccess),
  component: AdminPage,
})

const adminIndexRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: '/',
  beforeLoad: redirectToAdminHome,
})

const adminKnowledgeRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: 'knowledge',
  beforeLoad: requireAuth(knowledgeWriteAccess),
  component: KnowledgeManagement,
})

const adminKnowledgeConfigRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: 'knowledge-config',
  beforeLoad: requireAuth({ any: ['knowledge:admin', 'knowledge:write', 'system:admin'] }),
  component: KnowledgeConfig,
})

// ── Knowledge sub-pages ──

interface AdminKnowledgeDocumentsSearch {
  knowledgeBaseId?: string
}

function AdminKnowledgeDocumentsPage() {
  const navigate = useNavigate()
  const search = useSearch({ strict: false }) as AdminKnowledgeDocumentsSearch

  return (
    <KnowledgeDocumentsPage
      knowledgeBaseId={search.knowledgeBaseId}
      onNavigateChunks={(documentId: string, knowledgeBaseId: string) => {
        void navigate({
          to: '/admin/knowledge/chunks',
          search: { documentId, knowledgeBaseId },
        })
      }}
    />
  )
}

const adminKnowledgeDocumentsRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: 'knowledge/documents',
  beforeLoad: requireAuth({ any: ['knowledge:write', 'knowledge:admin', 'system:admin'] }),
  component: AdminKnowledgeDocumentsPage,
  validateSearch: (search: Record<string, unknown>): AdminKnowledgeDocumentsSearch => ({
    knowledgeBaseId:
      typeof search.knowledgeBaseId === 'string' ? search.knowledgeBaseId : undefined,
  }),
})

const adminKnowledgeSearchRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: 'knowledge/search',
  beforeLoad: requireAuth({ any: ['knowledge:write', 'knowledge:admin', 'system:admin'] }),
  component: KnowledgeSearchPage,
})

interface AdminKnowledgeChunksSearch {
  documentId: string
  knowledgeBaseId: string
}

function AdminKnowledgeChunksPage() {
  const navigate = useNavigate()
  const search = useSearch({ strict: false }) as AdminKnowledgeChunksSearch

  return (
    <KnowledgeChunksPage
      documentId={search.documentId}
      knowledgeBaseId={search.knowledgeBaseId}
      onNavigateBack={() => {
        void navigate({
          to: '/admin/knowledge/documents',
          search: { knowledgeBaseId: search.knowledgeBaseId },
        })
      }}
    />
  )
}

const adminKnowledgeChunksRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: 'knowledge/chunks',
  beforeLoad: requireAuth({ any: ['knowledge:write', 'knowledge:admin', 'system:admin'] }),
  component: AdminKnowledgeChunksPage,
  validateSearch: (search: Record<string, unknown>): AdminKnowledgeChunksSearch => {
    const documentId = typeof search.documentId === 'string' ? search.documentId : ''
    const knowledgeBaseId = typeof search.knowledgeBaseId === 'string' ? search.knowledgeBaseId : ''
    return { documentId, knowledgeBaseId }
  },
})

const adminQASettingsRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: 'qa-settings',
  beforeLoad: requireAuth(qaAdminAccess),
  component: QASettings,
})

const adminQASystemPromptRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: 'prompts',
  beforeLoad: requireAuth(qaSettingsReadAccess),
  component: QASystemPromptPage,
})

const adminQARetrievalTestRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: 'qa-retrieval-test',
  beforeLoad: requireAuth(qaAdminAccess),
  component: QARetrievalTestPage,
})

const adminSettingsRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: 'settings',
  beforeLoad: requireAuth(systemAdminAccess),
  component: SystemSettings,
})

const adminStatsRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: 'stats',
  beforeLoad: requireAuth({ any: ['system:admin'] }),
  component: StatsOverviewPage,
})

const modelProfilesPerm: PermissionRequirement = {
  any: ['admin:model-profile:write', 'system:admin'],
}
const parserConfigsPerm: PermissionRequirement = {
  any: ['admin:parser-config:write', 'knowledge:admin', 'system:admin'],
}

const adminModelProfilesRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: 'model-profiles',
  beforeLoad: requireAuth(modelProfilesPerm),
  component: ModelProfilesPage,
})

const adminParserConfigsRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: 'parser-configs',
  beforeLoad: requireAuth(parserConfigsPerm),
  component: ParserConfigsPage,
})

const adminUsersRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: 'users',
  beforeLoad: requireAuth(adminUsersAccess),
  component: AdminUsersPage,
  validateSearch: normalizeAdminUsersSearch,
})

const adminReportRecordsRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: 'reports/records',
  beforeLoad: requireAuth(reportAccess),
  component: ReportRecordsPage,
})

const adminReportTemplatesRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: 'reports/templates',
  beforeLoad: requireAuth(reportWriteAccess),
  component: ReportTemplatesPage,
})

const routeTree = rootRoute.addChildren([
  loginRoute,
  passwordChangeRequiredRoute,
  authenticatedRoute.addChildren([
    indexRoute,
    forbiddenRoute,
    profileRoute,
    chatRoute,
    qaRoute.addChildren([qaRetrievalTestRoute]),
    reportsRoute.addChildren([
      reportsIndexRoute,
      reportGenerateRoute,
      reportRecordsRoute,
      reportTemplatesRoute,
    ]),
    knowledgeRoute.addChildren([knowledgeSearchRoute]),
    adminRoute.addChildren([
      adminIndexRoute,
      adminKnowledgeRoute,
      adminKnowledgeConfigRoute,
      adminKnowledgeDocumentsRoute,
      adminKnowledgeSearchRoute,
      adminKnowledgeChunksRoute,
      adminQASettingsRoute,
      adminQASystemPromptRoute,
      adminQARetrievalTestRoute,
      adminModelProfilesRoute,
      adminParserConfigsRoute,
      adminUsersRoute,
      adminSettingsRoute,
      adminStatsRoute,
      adminReportRecordsRoute,
      adminReportTemplatesRoute,
    ]),
  ]),
])

export const router = createRouter({ routeTree })

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}
