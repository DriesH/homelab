import { createFileRoute } from '@tanstack/react-router'

import { OverviewPage } from '@/features/overview/overview-page'
import { overviewQuery } from '@/lib/queries'

export const Route = createFileRoute('/_app/')({
    loader: ({ context }) => context.queryClient.prefetchQuery(overviewQuery),
    component: OverviewPage,
})
