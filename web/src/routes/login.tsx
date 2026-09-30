import { createFileRoute, redirect } from '@tanstack/react-router'

import { LoginPage } from '@/features/auth/login-page'
import { sessionQuery } from '@/lib/queries'

type LoginSearch = { redirect?: string }

export const Route = createFileRoute('/login')({
    validateSearch: (search: Record<string, unknown>): LoginSearch => ({
        redirect: typeof search.redirect === 'string' ? search.redirect : undefined,
    }),
    beforeLoad: async ({ context }) => {
        const session = await context.queryClient.ensureQueryData(sessionQuery).catch(() => null)

        if (session) {
            throw redirect({ to: '/' })
        }
    },
    component: LoginPage,
})
