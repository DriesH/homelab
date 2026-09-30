import { createFileRoute } from '@tanstack/react-router'

import { HealthPage } from '@/features/health/health-page'
import { healthQuery } from '@/lib/queries'

export const Route = createFileRoute('/_app/health')({
    loader: ({ context }) => context.queryClient.prefetchQuery(healthQuery),
    component: HealthPage,
})
