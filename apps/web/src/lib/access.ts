import type { PermissionRequirement } from './permissions'

export const adminShellAccess: PermissionRequirement = {
  authorities: [
    'admin',
    'super_admin',
    'system:admin',
    'knowledge:read',
    'knowledge:admin',
    'knowledge:write',
    'admin:model-profile:write',
    'admin:parser-config:write',
    'qa:settings:read',
    'qa:settings:write',
    'report:read',
    'report:write',
    'reports:write',
  ],
}
