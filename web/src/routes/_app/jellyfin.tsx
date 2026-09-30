import { createFileRoute } from '@tanstack/react-router'

import { JellyfinPage } from '@/features/jellyfin/jellyfin-page'
import { jellyfinQuery } from '@/lib/queries'

export const Route = createFileRoute('/_app/jellyfin')({
    loader: ({ context }) => context.queryClient.prefetchQuery(jellyfinQuery),
    component: JellyfinPage,
})
