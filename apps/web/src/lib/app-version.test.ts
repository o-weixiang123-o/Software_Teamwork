import { describe, expect, it } from 'vitest'

import {
  compareAppFreshness,
  formatAppVersion,
  getAppCommitUrl,
  getUpstreamDevelopCommitsUrl,
  getUpstreamDevelopCompareUrl,
} from './app-version'

describe('app version helpers', () => {
  it('formats package versions with a stable fallback', () => {
    expect(formatAppVersion('0.1.0')).toBe('v0.1.0')
    expect(formatAppVersion('v0.2.0')).toBe('v0.2.0')
    expect(formatAppVersion('')).toBe('v0.0.0')
    expect(formatAppVersion(null)).toBe('v0.0.0')
  })

  it('compares local and upstream commit freshness without network access', () => {
    expect(compareAppFreshness('11111111', '11111111')).toBe('current')
    expect(compareAppFreshness('11111111', '22222222')).toBe('different')
    expect(compareAppFreshness('', '22222222')).toBe('unknown')
  })

  it('builds GitHub commit and compare urls without calling the GitHub API', () => {
    expect(getAppCommitUrl('ABCDEF123456')).toBe(
      'https://github.com/Sakayori-Iroha-168/Software_Teamwork/commit/abcdef123456',
    )
    expect(getAppCommitUrl('')).toBeNull()
    expect(getUpstreamDevelopCommitsUrl()).toBe(
      'https://github.com/Sakayori-Iroha-168/Software_Teamwork/commits/develop',
    )
    expect(getUpstreamDevelopCompareUrl('ABCDEF123456')).toBe(
      'https://github.com/Sakayori-Iroha-168/Software_Teamwork/compare/abcdef123456...develop',
    )
    expect(getUpstreamDevelopCompareUrl('')).toBe(getUpstreamDevelopCommitsUrl())
  })
})
