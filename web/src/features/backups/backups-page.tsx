import { useQuery } from '@tanstack/react-query'
import { CircleCheckIcon, CircleXIcon, Loader2Icon, TriangleAlertIcon } from 'lucide-react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Card } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import type { BackupRun } from '@/lib/api'
import { formatDateTime, formatRelative } from '@/lib/format'
import { backupsQuery } from '@/lib/queries'
import { GuestBackups } from './guest-backups'
import { JobCard } from './job-card'

export function BackupsPage() {
    const { data, error, isPending } = useQuery(backupsQuery)

    if (isPending) {
        return (
            <div className="flex flex-col gap-6">
                <Skeleton className="h-10 w-64" />
                <Skeleton className="h-64 w-full rounded-xl" />
            </div>
        )
    }

    if (error) {
        return (
            <Alert variant="destructive">
                <TriangleAlertIcon />
                <AlertTitle>Could not load backups</AlertTitle>
                <AlertDescription>{error.message}</AlertDescription>
            </Alert>
        )
    }

    const { job } = data

    return (
        <div className="flex flex-col gap-8">
            <header className="flex flex-wrap items-center justify-between gap-3">
                <div>
                    <h1 className="text-lg font-semibold">Backups</h1>
                    <p className="text-sm text-muted-foreground">
                        {!job.exists
                            ? 'No backup schedule yet'
                            : !job.enabled
                              ? 'Automatic backups are off'
                              : job.nextRun
                                ? `Next backup: ${formatDateTime(job.nextRun)} on ${job.storage}`
                                : `Backups go to ${job.storage}`}
                    </p>
                </div>
                {data.busy && (
                    <Badge variant="secondary">
                        <Loader2Icon className="animate-spin" />
                        {data.busy}…
                    </Badge>
                )}
            </header>

            <section className="flex flex-col gap-3">
                <h2 className="text-lg font-semibold">Containers and VMs</h2>
                <GuestBackups backups={data} />
            </section>

            <JobCard key={JSON.stringify(job)} backups={data} />

            <section className="flex flex-col gap-3">
                <h2 className="text-lg font-semibold">History</h2>
                <BackupHistory runs={data.history} />
            </section>
        </div>
    )
}

function BackupHistory({ runs }: { runs: BackupRun[] }) {
    if (runs.length === 0) {
        return (
            <Card className="p-6 text-center text-sm text-muted-foreground">
                Backups, restores and deletes from Homelab show up here. Failed scheduled backups are sent to Telegram.
            </Card>
        )
    }

    return (
        <Card className="gap-0 py-0">
            <ul className="divide-y">
                {runs.map((run) => (
                    <li key={run.id} className="flex items-center gap-3 px-4 py-3 text-sm">
                        {run.succeeded ? (
                            <CircleCheckIcon className="size-4 shrink-0 text-emerald-500" aria-label="succeeded" />
                        ) : (
                            <CircleXIcon className="size-4 shrink-0 text-destructive" aria-label="failed" />
                        )}
                        <div className="min-w-0 flex-1">
                            <span className="font-medium">{run.target}</span>
                            <p className="text-muted-foreground">{run.message}</p>
                        </div>
                        <span className="hidden text-muted-foreground sm:inline">{formatRelative(run.finishedAt)}</span>
                    </li>
                ))}
            </ul>
        </Card>
    )
}
