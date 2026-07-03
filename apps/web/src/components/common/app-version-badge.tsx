import { AlertTriangle, CheckCircle2, ExternalLink, GitBranch, Loader2 } from 'lucide-react'
import { useEffect, useState } from 'react'

import { getCachedAppVersionFreshness } from '@/api/app-version'
import { badgeVariants } from '@/components/ui/badge'
import { buttonVariants } from '@/components/ui/button'
import {
  Popover,
  PopoverContent,
  PopoverDescription,
  PopoverHeader,
  PopoverTitle,
  PopoverTrigger,
} from '@/components/ui/popover'
import {
  APP_UPDATE_COMMAND,
  appCommitSha,
  appVersionLabel,
  formatAppVersion,
  formatCommitLabel,
  getAppCommitUrl,
  getUpstreamDevelopCompareUrl,
} from '@/lib/app-version'
import type { AppVersionFreshness, AppVersionFreshnessReason } from '@/lib/types'
import { cn } from '@/lib/utils'

type AppVersionBadgeProps = {
  className?: string
  currentSha?: string
  version?: string | null
}

const reasonLabels: Partial<Record<AppVersionFreshnessReason, string>> = {
  github_403: 'GitHub 返回 403，可能触发了匿名请求限流。',
  github_404: 'GitHub 返回 404，暂时找不到用于对比的提交。',
  github_429: 'GitHub 返回 429，请求过于频繁。',
  github_status: 'GitHub 返回非成功状态。',
  invalid_response: 'GitHub 响应格式不可用。',
  missing_current_sha: '当前构建没有可用提交号。',
  network_error: 'Gateway 暂时无法连接 GitHub。',
  untrusted_current_sha: '当前提交未在 Gateway 允许列表中，已跳过外部检查。',
}

export function AppVersionBadge({
  className,
  currentSha = appCommitSha,
  version = appVersionLabel,
}: AppVersionBadgeProps) {
  const label = formatAppVersion(version)
  const normalizedCurrentSha = currentSha.trim()
  const appCommitUrl = getAppCommitUrl(normalizedCurrentSha)
  const compareUrl = getUpstreamDevelopCompareUrl(normalizedCurrentSha)
  const [open, setOpen] = useState(false)
  const [freshness, setFreshness] = useState<AppVersionFreshness | null | undefined>()
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    setFreshness(undefined)
  }, [normalizedCurrentSha])

  useEffect(() => {
    if (!open || freshness !== undefined) return

    let active = true
    setLoading(true)
    void getCachedAppVersionFreshness(normalizedCurrentSha)
      .then((result) => {
        if (active) setFreshness(result)
      })
      .finally(() => {
        if (active) setLoading(false)
      })

    return () => {
      active = false
    }
  }, [freshness, normalizedCurrentSha, open])

  const status = getFreshnessStatus(freshness, loading)

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        aria-label={`前端版本 ${label}，查看构建状态`}
        className={cn(
          badgeVariants({ variant: 'outline' }),
          'cursor-pointer border-primary/25 bg-background/70 font-mono text-[0.68rem] text-muted-foreground hover:bg-muted hover:text-foreground',
          className,
        )}
        title={`前端版本 ${label}，查看构建状态`}
      >
        {label}
      </PopoverTrigger>
      <PopoverContent align="end" className="w-80 gap-3 text-xs">
        <PopoverHeader>
          <PopoverTitle className="flex items-center gap-2 text-sm">
            <GitBranch className="size-3.5 text-primary" />
            本地构建版本
          </PopoverTitle>
          <PopoverDescription>
            通过 Gateway 检查当前构建是否包含 upstream/develop 最新提交。
          </PopoverDescription>
        </PopoverHeader>

        <div className={cn('rounded-md border p-2', status.className)}>
          <div className="flex items-center gap-2 font-medium">
            <status.Icon className={cn('size-3.5', status.iconClassName)} />
            {status.title}
          </div>
          <p className="mt-1 text-muted-foreground">{status.description}</p>
        </div>

        <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-muted-foreground">
          <dt>当前版本</dt>
          <dd className="font-mono text-foreground">{label}</dd>
          <dt>当前提交</dt>
          <dd className="font-mono text-foreground">
            {appCommitUrl ? (
              <a
                className="underline-offset-4 hover:underline"
                href={appCommitUrl}
                rel="noreferrer"
                target="_blank"
              >
                {formatCommitLabel(normalizedCurrentSha)}
              </a>
            ) : (
              formatCommitLabel(normalizedCurrentSha)
            )}
          </dd>
          <dt>develop 最新</dt>
          <dd className="font-mono text-foreground">
            {freshness?.latestUrl && freshness.latestSha ? (
              <a
                className="underline-offset-4 hover:underline"
                href={freshness.latestUrl}
                rel="noreferrer"
                target="_blank"
              >
                {formatCommitLabel(freshness.latestSha)}
              </a>
            ) : (
              'unknown'
            )}
          </dd>
          <dt>检查时间</dt>
          <dd className="text-foreground">{formatCheckedAt(freshness?.checkedAt)}</dd>
        </dl>

        <div className="rounded-md border border-border bg-muted/40 p-2">
          <div className="font-medium text-foreground">同步 develop</div>
          <code className="mt-1 block break-all font-mono text-[0.68rem] text-foreground">
            {APP_UPDATE_COMMAND}
          </code>
        </div>

        <a
          className={cn(buttonVariants({ variant: 'outline', size: 'sm' }), 'w-full')}
          href={compareUrl}
          rel="noreferrer"
          target="_blank"
        >
          <ExternalLink className="size-3.5" />
          打开 GitHub 对比
        </a>
      </PopoverContent>
    </Popover>
  )
}

function getFreshnessStatus(freshness: AppVersionFreshness | null | undefined, loading: boolean) {
  if (loading && freshness === undefined) {
    return {
      Icon: Loader2,
      className: 'border-border bg-muted/40',
      description: '正在读取 Gateway 缓存或查询 upstream/develop。',
      iconClassName: 'animate-spin text-muted-foreground',
      title: '正在检查构建状态',
    }
  }

  if (freshness?.status === 'current') {
    return {
      Icon: CheckCircle2,
      className: 'border-emerald-500/30 bg-emerald-500/10',
      description: '当前构建已包含 upstream/develop 最新提交。',
      iconClassName: 'text-emerald-600',
      title: '已包含 develop 最新提交',
    }
  }

  if (freshness?.status === 'different') {
    return {
      Icon: AlertTriangle,
      className: 'border-amber-500/30 bg-amber-500/10',
      description: '当前构建尚未包含 upstream/develop 最新提交，请按需同步后重新构建。',
      iconClassName: 'text-amber-600',
      title: '当前构建落后 develop',
    }
  }

  return {
    Icon: AlertTriangle,
    className: 'border-border bg-muted/40',
    description:
      freshness?.reason && reasonLabels[freshness.reason]
        ? reasonLabels[freshness.reason]
        : 'Gateway 没有返回可用提交号，稍后会自动重试。',
    iconClassName: 'text-muted-foreground',
    title: '无法判断当前构建状态',
  }
}

function formatCheckedAt(value: string | undefined) {
  if (!value) return 'unknown'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return 'unknown'
  return new Intl.DateTimeFormat('zh-CN', {
    dateStyle: 'short',
    timeStyle: 'medium',
  }).format(date)
}
