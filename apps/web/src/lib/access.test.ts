import { describe, expect, it } from 'vitest'

import { adminShellAccess } from './access'
import { canAccess } from './permissions'
import type { UserSummary } from './types'

const businessUser: UserSummary = {
  id: 'user-standard',
  username: 'standard',
  roles: ['standard'],
  permissions: ['qa:use', 'knowledge:read', 'document:upload'],
}

describe('shared access requirements', () => {
  it('grants the admin shell to standard knowledge readers', () => {
    expect(canAccess(businessUser, adminShellAccess)).toBe(true)
  })

  it('grants the admin shell to ordinary retrieval permissions', () => {
    expect(
      canAccess(
        {
          ...businessUser,
          permissions: ['knowledge:read', 'qa:use'],
        },
        adminShellAccess,
      ),
    ).toBe(true)
  })

  it('does not grant the admin shell to unrelated standard business permissions', () => {
    expect(
      canAccess(
        {
          ...businessUser,
          permissions: ['qa:use', 'document:upload'],
        },
        adminShellAccess,
      ),
    ).toBe(false)
  })

  it('grants the admin shell to report authorities used by admin report routes', () => {
    expect(
      canAccess(
        {
          ...businessUser,
          permissions: ['report:read'],
        },
        adminShellAccess,
      ),
    ).toBe(true)
  })

  it('grants the admin shell to QA settings authorities used by Agent prompt routes', () => {
    expect(
      canAccess(
        {
          ...businessUser,
          permissions: ['qa:settings:read'],
        },
        adminShellAccess,
      ),
    ).toBe(true)

    expect(
      canAccess(
        {
          ...businessUser,
          permissions: ['qa:settings:write'],
        },
        adminShellAccess,
      ),
    ).toBe(true)
  })

  it('grants the admin shell to admin authorities', () => {
    expect(
      canAccess(
        {
          ...businessUser,
          roles: ['admin'],
          permissions: [],
        },
        adminShellAccess,
      ),
    ).toBe(true)
  })
})
