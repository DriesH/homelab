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
