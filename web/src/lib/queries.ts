import { queryOptions } from '@tanstack/react-query'

import { api } from '@/lib/api'

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
