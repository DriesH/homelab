import { createFileRoute } from '@tanstack/react-router'

import { AppsPage } from '@/features/apps/apps-page'
import { appsQuery } from '@/lib/queries'

export const Route = createFileRoute('/_app/apps')({
    loader: ({ context }) => context.queryClient.prefetchQuery(appsQuery),
    component: AppsPage,
})
