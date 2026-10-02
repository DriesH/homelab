import { createFileRoute } from '@tanstack/react-router'

import { CloudPage } from '@/features/cloud/cloud-page'
import { cloudQuery } from '@/lib/queries'

export const Route = createFileRoute('/_app/cloud')({
    loader: ({ context }) => context.queryClient.prefetchQuery(cloudQuery),
    component: CloudPage,
})
