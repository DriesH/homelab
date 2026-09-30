import { createFileRoute } from '@tanstack/react-router'

import { UpdatesPage } from '@/features/updates/updates-page'
import { updatesQuery } from '@/lib/queries'

export const Route = createFileRoute('/_app/updates')({
    loader: ({ context }) => context.queryClient.prefetchQuery(updatesQuery),
    component: UpdatesPage,
})
