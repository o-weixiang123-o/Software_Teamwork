const FALLBACK_APP_VERSION = '0.0.0'
const GITHUB_REPO_URL = 'https://github.com/Sakayori-Iroha-168/Software_Teamwork'
const UPSTREAM_BRANCH = 'develop'

export const APP_UPDATE_COMMAND = 'git fetch upstream --prune && git rebase upstream/develop'

export type AppFreshnessStatus = 'current' | 'different' | 'unknown'

export function formatAppVersion(version: string | null | undefined) {
  const versionNumber = version?.trim().replace(/^v/i, '').trim()

  return `v${versionNumber || FALLBACK_APP_VERSION}`
}

export const appVersionLabel = formatAppVersion(__APP_VERSION__)
export const appCommitSha = __APP_COMMIT_SHA__.trim()
export const appCommitShortSha = __APP_COMMIT_SHORT_SHA__.trim() || 'unknown'

function normalizeSha(value: string) {
  return value.trim().toLowerCase()
}

function shortSha(value: string) {
  return value.slice(0, 8) || 'unknown'
}

export function compareAppFreshness(currentSha: string, latestSha: string): AppFreshnessStatus {
  const current = normalizeSha(currentSha)
  const latest = normalizeSha(latestSha)

  if (!current || !latest) return 'unknown'
  if (current === latest) return 'current'

  return 'different'
}

export function getUpstreamDevelopCommitsUrl() {
  return `${GITHUB_REPO_URL}/commits/${UPSTREAM_BRANCH}`
}

export function getAppCommitUrl(currentSha = appCommitSha) {
  const normalizedSha = normalizeSha(currentSha)

  return normalizedSha ? `${GITHUB_REPO_URL}/commit/${encodeURIComponent(normalizedSha)}` : null
}

export function getUpstreamDevelopCompareUrl(currentSha = appCommitSha) {
  const normalizedSha = normalizeSha(currentSha)

  if (!normalizedSha) return getUpstreamDevelopCommitsUrl()

  return `${GITHUB_REPO_URL}/compare/${encodeURIComponent(normalizedSha)}...${UPSTREAM_BRANCH}`
}

export function formatCommitLabel(sha: string) {
  return shortSha(sha.trim())
}
