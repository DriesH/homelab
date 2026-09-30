import { createFileRoute } from '@tanstack/react-router'

import { ConsolePage } from '@/features/console/console-page'
import { overviewQuery } from '@/lib/queries'

export const Route = createFileRoute('/_app/console/$vmid')({
    loader: ({ context }) => context.queryClient.prefetchQuery(overviewQuery),
    component: ConsolePage,
})
