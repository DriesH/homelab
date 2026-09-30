import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertTriangleIcon, CircleCheckIcon, RefreshCwIcon } from 'lucide-react'
import { toast } from 'sonner'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { api, type Health } from '@/lib/api'
import { formatRelative } from '@/lib/format'
import { healthQuery } from '@/lib/queries'
import { DiskList, PoolList, StorageList } from './disks'
import { ServiceChecks } from './service-checks'

export function HealthPage() {
    const { data, error, isPending } = useQuery(healthQuery)
    const queryClient = useQueryClient()

    const refresh = useMutation({
        mutationFn: api.refreshHealth,
        onSuccess: (health) => queryClient.setQueryData(healthQuery.queryKey, health),
        onError: (error) => toast.error(error.message),
    })

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
                <AlertTitle>Could not load health</AlertTitle>
                <AlertDescription>{error.message}</AlertDescription>
            </Alert>
        )
    }

    return (
        <div className="flex flex-col gap-8">
            <header className="flex flex-wrap items-center justify-between gap-3">
                <div>
                    <h1 className="text-lg font-semibold">Health</h1>
                    <p className="text-sm text-muted-foreground">
                        {data.checkedAt ? `Disks checked ${formatRelative(data.checkedAt)}` : 'Checking disks…'}
                        {' · '}Alerts go to the Telegram chat from the Updates page.
                    </p>
                </div>
                <Button variant="outline" disabled={refresh.isPending} onClick={() => refresh.mutate()}>
                    <RefreshCwIcon className={refresh.isPending ? 'animate-spin' : undefined} />
                    Check now
                </Button>
            </header>

            <Problems health={data} />

            <section className="flex flex-col gap-3">
                <h2 className="text-lg font-semibold">Services</h2>
                <ServiceChecks services={data.services} />
            </section>

            <section className="flex flex-col gap-3">
                <h2 className="text-lg font-semibold">Disks</h2>
                <DiskList disks={data.disks} />
            </section>

            {data.pools.length > 0 && (
                <section className="flex flex-col gap-3">
                    <h2 className="text-lg font-semibold">ZFS pools</h2>
                    <PoolList pools={data.pools} />
                </section>
            )}

            <section className="flex flex-col gap-3">
                <h2 className="text-lg font-semibold">Storage</h2>
                <StorageList storage={data.storage} />
            </section>
        </div>
    )
}

function Problems({ health }: { health: Health }) {
    const problems = [
        ...health.services
            .filter((service) => service.status === 'down')
            .map((service) => `${service.name} is down: ${service.error}`),
        ...[...health.disks, ...health.pools, ...health.storage].flatMap((item) =>
            item.problem ? [item.problem] : [],
        ),
        ...health.errors,
    ]

    if (problems.length === 0) {
        if (!health.checkedAt) {
            return null
        }

        return (
            <Alert>
                <CircleCheckIcon className="text-emerald-600 dark:text-emerald-400" />
                <AlertTitle>Everything looks healthy</AlertTitle>
            </Alert>
        )
    }

    return (
        <Alert variant="destructive">
            <AlertTriangleIcon />
            <AlertTitle>{problems.length === 1 ? '1 problem' : `${problems.length} problems`}</AlertTitle>
            <AlertDescription>
                <ul className="list-disc pl-4">
                    {problems.map((problem) => (
                        <li key={problem}>{problem}</li>
                    ))}
                </ul>
            </AlertDescription>
        </Alert>
    )
}
