import { createFileRoute, redirect } from '@tanstack/react-router'

import { AppLayout } from '@/features/layout/app-layout'
import { sessionQuery } from '@/lib/queries'

export const Route = createFileRoute('/_app')({
    beforeLoad: async ({ context, location }) => {
        const session = await context.queryClient.ensureQueryData(sessionQuery).catch(() => null)

        if (!session) {
            throw redirect({ to: '/login', search: { redirect: location.href } })
        }

        return { session }
    },
    component: AppLayout,
})
