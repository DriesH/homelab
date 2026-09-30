import { createFileRoute } from '@tanstack/react-router'

import { LogsPage } from '@/features/logs/logs-page'
import { overviewQuery } from '@/lib/queries'

export const Route = createFileRoute('/_app/logs')({
    loader: ({ context }) => context.queryClient.prefetchQuery(overviewQuery),
    component: LogsPage,
})
