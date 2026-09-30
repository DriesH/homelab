import { createFileRoute } from '@tanstack/react-router'

import { TailscalePage } from '@/features/tailscale/tailscale-page'
import { tailscaleQuery } from '@/lib/queries'

export const Route = createFileRoute('/_app/tailscale')({
    loader: ({ context }) => context.queryClient.prefetchQuery(tailscaleQuery),
    component: TailscalePage,
})
