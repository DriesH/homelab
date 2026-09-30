import { createFileRoute } from '@tanstack/react-router'

import { BackupsPage } from '@/features/backups/backups-page'
import { backupsQuery } from '@/lib/queries'

export const Route = createFileRoute('/_app/backups')({
    loader: ({ context }) => context.queryClient.prefetchQuery(backupsQuery),
    component: BackupsPage,
})
