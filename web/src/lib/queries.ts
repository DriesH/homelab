import { keepPreviousData, queryOptions } from '@tanstack/react-query'

import { api, type Timeframe, type UsageTarget } from '@/lib/api'

export const sessionQuery = queryOptions({
    queryKey: ['session'],
    queryFn: api.me,
    retry: false,
    staleTime: Infinity,
})

export const overviewQuery = queryOptions({
    queryKey: ['overview'],
    queryFn: api.overview,
    refetchInterval: 5000,
})

export function usageQuery(target: UsageTarget, timeframe: Timeframe) {
    return queryOptions({
        queryKey: ['usage', target.node, target.type, target.vmid, timeframe],
        queryFn: () => api.usage(target, timeframe),
        select: (data) => data.points,
        // Proxmox adds a point every minute, and every 30 minutes or more for the longer timeframes.
        refetchInterval: timeframe === 'hour' || timeframe === 'day' ? 60_000 : 30 * 60_000,
        placeholderData: keepPreviousData,
    })
}

export const updatesQuery = queryOptions({
    queryKey: ['updates'],
    queryFn: api.updates,
    // Poll fast while an update runs, so progress shows up quickly.
    refetchInterval: (query) => (query.state.data?.busy ? 2000 : 15000),
})

export function updateRunQuery(id: string) {
    return queryOptions({
        queryKey: ['updates', 'runs', id],
        queryFn: () => api.updateRun(id),
    })
}

export const jellyfinQuery = queryOptions({
    queryKey: ['jellyfin'],
    queryFn: api.jellyfin,
    refetchInterval: 10000,
})

export const healthQuery = queryOptions({
    queryKey: ['health'],
    queryFn: api.health,
    refetchInterval: 15000,
})

export const selfUpdateQuery = queryOptions({
    queryKey: ['self-update'],
    queryFn: api.selfUpdate,
    // Poll fast while an upgrade runs, also while the manager restarts, so the page sees the new version quickly.
    refetchInterval: (query) =>
        query.state.data?.installing || query.state.data?.upgrade?.state === 'running' ? 2000 : 60000,
    retry: false,
})

export const tailscaleQuery = queryOptions({
    queryKey: ['tailscale'],
    queryFn: api.tailscale,
    // Poll fast during a login, so the page sees the login link and the result quickly.
    refetchInterval: (query) => (query.state.data?.connecting ? 2000 : 15000),
})

export const backupsQuery = queryOptions({
    queryKey: ['backups'],
    queryFn: api.backups,
    refetchInterval: (query) => (query.state.data?.busy ? 3000 : 30000),
})

export const appsQuery = queryOptions({
    queryKey: ['apps'],
    queryFn: api.apps,
    // Poll fast during an install, for the live log.
    refetchInterval: (query) => (query.state.data?.apps.some((app) => app.install?.state === 'running') ? 2000 : 30000),
})
