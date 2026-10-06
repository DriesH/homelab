import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { CheckIcon, CloudIcon, CloudOffIcon, Loader2Icon, TriangleAlertIcon } from 'lucide-react'

import { JobProgress } from '@/components/job-progress'
import {
    AlertDialog,
    AlertDialogAction,
    AlertDialogCancel,
    AlertDialogContent,
    AlertDialogDescription,
    AlertDialogFooter,
    AlertDialogHeader,
    AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { api, type Cloud, type CloudJob } from '@/lib/api'
import { cloudQuery } from '@/lib/queries'
import { KeyCard } from './key-card'
import { RuleCard } from './rule-card'
import { StorageCard } from './storage-card'
import { useCloudMutation } from './use-cloud'

const jobText = {
    enable: {
        running: 'Turning cloud storage on…',
        runningDetail:
            'Jellyfin and the media stack restart, so they are offline for a minute. You can leave this page.',
        failed: 'Cloud storage did not turn on',
    },
    disable: {
        running: 'Bringing the media back…',
        runningDetail: 'Homelab copies everything back from the cloud. This can take hours. You can leave this page.',
        failed: 'Cloud storage did not turn off',
    },
}

export function CloudPage() {
    const { data, error } = useQuery(cloudQuery)
    const job = data?.host?.job
    const running = job?.state === 'running'

    return (
        <div className="flex flex-col gap-6">
            <header className="flex flex-wrap items-start justify-between gap-2">
                <div>
                    <h1 className="text-lg font-semibold">Cloud storage</h1>
                    <p className="text-sm text-muted-foreground">
                        Keep new media on your own disk, and move older media to encrypted cloud storage. Jellyfin and
                        the apps still see one media folder.
                    </p>
                </div>
                {data?.host && <StatusBadge enabled={data.host.enabled} job={job} />}
            </header>

            {error && (
                <Alert variant="destructive">
                    <TriangleAlertIcon />
                    <AlertTitle>Could not load the cloud storage</AlertTitle>
                    <AlertDescription>{error.message}</AlertDescription>
                </Alert>
            )}
            {data?.hostError && (
                <Alert variant="destructive">
                    <TriangleAlertIcon />
                    <AlertTitle>The host agent did not answer</AlertTitle>
                    <AlertDescription>{data.hostError}</AlertDescription>
                </Alert>
            )}

            {job && (job.state === 'running' || job.state === 'failed') && (
                <JobProgress
                    state={job.state}
                    title={job.state === 'running' ? jobText[job.action].running : jobText[job.action].failed}
                    detail={job.state === 'running' ? jobText[job.action].runningDetail : `${job.message}.`}
                    log={job.log}
                />
            )}

            {!data ? (
                <Skeleton className="h-96 w-full" />
            ) : (
                <>
                    {data.host?.enabled && <EnabledCard cloud={data} busy={running} />}
                    <div className="grid gap-4 lg:grid-cols-2">
                        <StorageCard
                            key={data.settings?.provider ?? 'new'}
                            cloud={data}
                            locked={Boolean(data.host?.enabled) || running}
                        />
                        <div className="flex flex-col gap-4">
                            <KeyCard cloud={data} />
                            <RuleCard rule={data.rule} />
                        </div>
                    </div>
                    {!data.host?.enabled && <EnableCard cloud={data} busy={running} />}
                </>
            )}
        </div>
    )
}

function StatusBadge({ enabled, job }: { enabled: boolean; job?: CloudJob }) {
    if (job?.state === 'running') {
        return (
            <Badge variant="outline">
                <Loader2Icon className="animate-spin" />
                {job.action === 'enable' ? 'Turning on' : 'Turning off'}
            </Badge>
        )
    }

    return enabled ? (
        <Badge>
            <CloudIcon />
            On
        </Badge>
    ) : (
        <Badge variant="outline">
            <CloudOffIcon />
            Off
        </Badge>
    )
}

function EnableCard({ cloud, busy }: { cloud: Cloud; busy: boolean }) {
    const [confirm, setConfirm] = useState(false)
    const enable = useCloudMutation(api.enableCloud, 'Could not turn cloud storage on')
    const steps = [
        { done: Boolean(cloud.settings), text: 'Save the storage' },
        { done: cloud.key === 'confirmed', text: 'Make and save the recovery key' },
    ]
    const ready = steps.every((step) => step.done)

    return (
        <Card>
            <CardHeader>
                <CardTitle>Turn on</CardTitle>
                <CardDescription>
                    When the storage and the recovery key are ready, turn cloud storage on.
                </CardDescription>
            </CardHeader>
            <CardContent>
                <ul className="flex flex-col gap-1 text-sm">
                    {steps.map((step) => (
                        <li key={step.text} className="flex items-center gap-2">
                            <CheckIcon
                                className={step.done ? 'size-4 text-primary' : 'size-4 text-muted-foreground/40'}
                            />
                            <span className={step.done ? '' : 'text-muted-foreground'}>{step.text}</span>
                        </li>
                    ))}
                </ul>
            </CardContent>
            <CardFooter>
                <Button disabled={!ready || busy || enable.isPending} onClick={() => setConfirm(true)}>
                    {enable.isPending ? <Loader2Icon className="animate-spin" /> : <CloudIcon />}
                    Turn cloud storage on
                </Button>
            </CardFooter>

            <AlertDialog open={confirm} onOpenChange={setConfirm}>
                <AlertDialogContent>
                    <AlertDialogHeader>
                        <AlertDialogTitle>Turn cloud storage on?</AlertDialogTitle>
                        <AlertDialogDescription className="flex flex-col gap-2">
                            <span>
                                Homelab installs rclone and mergerfs on the host, and joins your media folder with{' '}
                                {cloud.label ?? 'the cloud storage'}.
                            </span>
                            <span>
                                Jellyfin and the media stack restart, so they are offline for a minute. Nothing moves to
                                the cloud yet: that starts with the first nightly run.
                            </span>
                        </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                        <AlertDialogCancel>Cancel</AlertDialogCancel>
                        <AlertDialogAction
                            onClick={() => {
                                setConfirm(false)
                                enable.mutate()
                            }}
                        >
                            Turn on
                        </AlertDialogAction>
                    </AlertDialogFooter>
                </AlertDialogContent>
            </AlertDialog>
        </Card>
    )
}

function EnabledCard({ cloud, busy }: { cloud: Cloud; busy: boolean }) {
    const [confirm, setConfirm] = useState(false)
    const disable = useCloudMutation(api.disableCloud, 'Could not turn cloud storage off')

    return (
        <Card>
            <CardHeader>
                <CardTitle>Cloud storage is on</CardTitle>
                <CardDescription>
                    Older media is in <span className="font-medium text-foreground">{cloud.host?.label}</span>,
                    encrypted with your recovery key.
                </CardDescription>
            </CardHeader>
            <CardFooter>
                <Button variant="outline" disabled={busy || disable.isPending} onClick={() => setConfirm(true)}>
                    {disable.isPending ? <Loader2Icon className="animate-spin" /> : <CloudOffIcon />}
                    Turn off
                </Button>
            </CardFooter>

            <AlertDialog open={confirm} onOpenChange={setConfirm}>
                <AlertDialogContent>
                    <AlertDialogHeader>
                        <AlertDialogTitle>Turn cloud storage off?</AlertDialogTitle>
                        <AlertDialogDescription className="flex flex-col gap-2">
                            <span>
                                Homelab first copies all media back from the cloud. Your local disk or share needs room
                                for all of it, and Homelab checks that before it starts. This can take hours.
                            </span>
                            <span>
                                When all media is back, Jellyfin and the media stack restart, and Homelab empties the
                                cloud storage.
                            </span>
                        </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                        <AlertDialogCancel>Cancel</AlertDialogCancel>
                        <AlertDialogAction
                            onClick={() => {
                                setConfirm(false)
                                disable.mutate()
                            }}
                        >
                            Bring the media back
                        </AlertDialogAction>
                    </AlertDialogFooter>
                </AlertDialogContent>
            </AlertDialog>
        </Card>
    )
}
