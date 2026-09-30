import { useQuery } from '@tanstack/react-query'
import { AlertTriangleIcon, Loader2Icon } from 'lucide-react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { api } from '@/lib/api'
import { formatDateTime } from '@/lib/format'
import { updatesQuery } from '@/lib/queries'
import { SelfUpdateCard, SelfUpdateSourceCard } from '@/features/self-update/self-update-card'
import { ContainerUpdates } from './container-updates'
import { HostUpdates } from './host-updates'
import { ScheduleCard, TelegramCard } from './settings-cards'
import { UpdateHistory } from './update-history'
import { useStartUpdate } from './use-updates'

export function UpdatesPage() {
    const { data, error, isPending } = useQuery(updatesQuery)
    const start = useStartUpdate()

    if (isPending) {
        return (
            <div className="flex flex-col gap-6">
                <Skeleton className="h-10 w-64" />
                <Skeleton className="h-40 w-full rounded-xl" />
                <Skeleton className="h-64 w-full rounded-xl" />
            </div>
        )
    }

    if (error) {
        return (
            <Alert variant="destructive">
                <AlertTriangleIcon />
                <AlertTitle>Could not load updates</AlertTitle>
                <AlertDescription>{error.message}</AlertDescription>
            </Alert>
        )
    }

    const busy = Boolean(data.busy)

    return (
        <div className="flex flex-col gap-8">
            <header className="flex flex-wrap items-center justify-between gap-3">
                <div>
                    <h1 className="text-lg font-semibold">Updates</h1>
                    <p className="text-sm text-muted-foreground">
                        {data.nextRun
                            ? `Next automatic run: ${formatDateTime(data.nextRun)}`
                            : 'Automatic updates are off'}
                    </p>
                </div>
                <div className="flex items-center gap-3">
                    {busy && (
                        <Badge variant="secondary">
                            <Loader2Icon className="animate-spin" />
                            {data.busy}…
                        </Badge>
                    )}
                    <Button disabled={busy || start.isPending} onClick={() => start.mutate(api.checkUpdates)}>
                        Check now
                    </Button>
                </div>
            </header>

            <SelfUpdateCard />

            <HostUpdates host={data.host} busy={busy} />

            <section className="flex flex-col gap-3">
                <h2 className="text-lg font-semibold">Containers</h2>
                <ContainerUpdates updates={data} />
            </section>

            <div className="grid gap-4 lg:grid-cols-2">
                <ScheduleCard updates={data} />
                <TelegramCard updates={data} />
                <SelfUpdateSourceCard />
            </div>

            <section className="flex flex-col gap-3">
                <h2 className="text-lg font-semibold">History</h2>
                <UpdateHistory runs={data.history} />
            </section>
        </div>
    )
}
