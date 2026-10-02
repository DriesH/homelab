import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ExternalLinkIcon, Loader2Icon, RotateCwIcon, TriangleAlertIcon } from 'lucide-react'
import { toast } from 'sonner'

import { JobProgress } from '@/components/job-progress'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { api, type Apps, type CatalogApp } from '@/lib/api'
import { appsQuery } from '@/lib/queries'
import { formatDateTime } from '@/lib/format'
import { AppActions } from './app-actions'
import { InstallDialog } from './install-dialog'

export function AppsPage() {
    const { data, error } = useQuery(appsQuery)

    return (
        <div className="flex flex-col gap-6">
            <header>
                <h1 className="text-lg font-semibold">Apps</h1>
                <p className="text-sm text-muted-foreground">
                    Each app gets its own container on this host, with its own backups and updates.
                </p>
            </header>

            {error && (
                <Alert variant="destructive">
                    <TriangleAlertIcon />
                    <AlertTitle>Could not load the apps</AlertTitle>
                    <AlertDescription>{error.message}</AlertDescription>
                </Alert>
            )}
            {data?.error && (
                <Alert variant="destructive">
                    <TriangleAlertIcon />
                    <AlertTitle>The host agent did not answer</AlertTitle>
                    <AlertDescription>{data.error}</AlertDescription>
                </Alert>
            )}

            {!data && !error && <Skeleton className="h-48 w-full rounded-xl" />}
            {data?.apps.map((app) => (
                <AppCard key={app.id} app={app} defaults={data.defaults} />
            ))}
        </div>
    )
}

function AppCard({ app, defaults }: { app: CatalogApp; defaults: Apps['defaults'] }) {
    const queryClient = useQueryClient()
    const [installOpen, setInstallOpen] = useState(false)
    const operation = app.operation
    const running = operation?.state === 'running'
    const saved = app.saved

    const refresh = () => queryClient.invalidateQueries({ queryKey: appsQuery.queryKey })
    const retry = useMutation({
        mutationFn: () => api.retryApp(app.id),
        onSuccess: refresh,
        onError: (error) => toast.error(error.message),
    })
    const forget = useMutation({
        mutationFn: () => api.forgetAppAnswers(app.id),
        onSuccess: () => {
            refresh()
            toast.success('The answers are removed from the host')
        },
        onError: (error) => toast.error(error.message),
    })

    return (
        <Card>
            <CardHeader>
                <CardTitle className="flex flex-wrap items-center gap-2">
                    {app.name}
                    {app.installed && (
                        <Badge variant="outline" className="font-mono">
                            CT {app.vmid}
                        </Badge>
                    )}
                    {app.installed && (
                        <Badge variant={app.status === 'running' ? 'secondary' : 'outline'}>{app.status}</Badge>
                    )}
                    {app.updateAvailable && !running && <Badge>Update available</Badge>}
                </CardTitle>
                <CardDescription className="flex flex-col gap-1">
                    <span>{app.description}</span>
                    {app.hostUrl && (
                        <a href={app.hostUrl} target="_blank" rel="noreferrer" className="w-fit underline">
                            {app.hostUrl.replace('https://', '')}
                        </a>
                    )}
                </CardDescription>
                {app.installed && app.managed && !running && (
                    <CardAction className="flex flex-wrap justify-end gap-2">
                        <AppActions app={app} />
                    </CardAction>
                )}
                {!app.installed && !running && (
                    <CardAction className="flex flex-wrap justify-end gap-2">
                        {saved ? (
                            <>
                                <Button variant="outline" onClick={() => setInstallOpen(true)}>
                                    Change answers
                                </Button>
                                <Button disabled={retry.isPending} onClick={() => retry.mutate()}>
                                    {retry.isPending ? <Loader2Icon className="animate-spin" /> : <RotateCwIcon />}
                                    Try again
                                </Button>
                            </>
                        ) : (
                            <Button onClick={() => setInstallOpen(true)}>Install</Button>
                        )}
                    </CardAction>
                )}
            </CardHeader>

            <CardContent className="flex flex-col gap-4">
                {operation && <OperationProgress operation={operation} rollback={app.rollback} />}
                {app.installed && !app.managed && (
                    <p className="text-xs text-muted-foreground">
                        Homelab did not install this container, so it can't update or remove it here. Update it on the
                        Updates page.
                    </p>
                )}
                {saved && !running && (
                    <p className="text-xs text-muted-foreground">
                        Your answers stay on the host until {formatDateTime(saved.until)}, so you can try again without
                        typing them. The keys and passwords never leave the host.{' '}
                        <button
                            type="button"
                            className="underline"
                            disabled={forget.isPending}
                            onClick={() => forget.mutate()}
                        >
                            Remove them now
                        </button>
                    </p>
                )}
                <ul className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
                    {app.links.map((link) => (
                        <li key={link.name}>
                            {link.url ? (
                                <a
                                    href={link.url}
                                    target="_blank"
                                    rel="noreferrer"
                                    className="flex items-center justify-between gap-2 rounded-lg border px-3 py-2 text-sm hover:bg-muted/50"
                                >
                                    <span>
                                        <span className="font-medium">{link.name}</span>
                                        <span className="text-muted-foreground"> · {link.description}</span>
                                    </span>
                                    <ExternalLinkIcon className="size-4 text-muted-foreground" />
                                </a>
                            ) : (
                                <div className="rounded-lg border border-dashed px-3 py-2 text-sm text-muted-foreground">
                                    <span className="font-medium text-foreground">{link.name}</span> ·{' '}
                                    {link.description}
                                </div>
                            )}
                        </li>
                    ))}
                </ul>
                {app.installed && app.id === 'media' && (
                    <p className="text-xs text-muted-foreground">
                        Log in to Seerr with your Jellyfin account. If you gave no Jellyfin admin during the install,
                        finish the setup of Seerr first: until then, anyone on your network can open it. Log in to the
                        other apps with the username and password from the install. Add your indexers in Prowlarr. They
                        sync to Radarr and Sonarr.
                    </p>
                )}
            </CardContent>

            <InstallDialog
                app={app}
                defaults={defaults}
                saved={saved}
                open={installOpen}
                onOpenChange={setInstallOpen}
            />
        </Card>
    )
}

const operationText = {
    install: {
        running: 'Installing…',
        runningDetail: 'This takes 5 to 15 minutes, mostly to download the images. You can leave this page.',
        failed: 'The install failed',
    },
    update: {
        running: 'Updating…',
        runningDetail: 'This takes a few minutes. The app is offline while it restarts. You can leave this page.',
        failed: 'The update failed',
    },
    remove: { running: 'Removing…', runningDetail: 'You can leave this page.', failed: 'The removal failed' },
    vpn: {
        running: 'Changing the VPN…',
        runningDetail: 'Downloads stop for a minute while the VPN restarts. You can leave this page.',
        failed: 'The new VPN settings did not connect',
    },
}

function OperationProgress({
    operation,
    rollback,
}: {
    operation: NonNullable<CatalogApp['operation']>
    rollback?: string
}) {
    // An agent from before updates and removals sends no action.
    const text = operationText[operation.action ?? 'install']

    if (operation.state === 'running') {
        return <JobProgress state="running" title={text.running} detail={text.runningDetail} log={operation.log} />
    }

    if (operation.state === 'failed') {
        return (
            <JobProgress
                state="failed"
                title={text.failed}
                log={operation.log}
                detail={
                    <>
                        {operation.message}.
                        {operation.action === 'install' && ' If the install made a container, it removed it again.'}
                        {operation.action === 'vpn' && ' The old settings are back.'}
                        {rollback && ` ${rollback}`}
                    </>
                }
            />
        )
    }

    return null
}
