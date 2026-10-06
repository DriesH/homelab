import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { CopyIcon, ExternalLinkIcon, Loader2Icon, RotateCwIcon, TriangleAlertIcon } from 'lucide-react'
import { toast } from 'sonner'

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

// On a phone, the buttons go below the description, so the text keeps its width.
const actionClass =
    'flex flex-wrap justify-end gap-2 max-sm:col-start-1 max-sm:row-span-1 max-sm:row-start-auto max-sm:justify-self-start max-sm:justify-start max-sm:pt-2'

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
            <CardHeader className="max-sm:has-data-[slot=card-action]:grid-cols-1">
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
                    <CardAction className={actionClass}>
                        <AppActions app={app} />
                    </CardAction>
                )}
                {!app.installed && !running && (
                    <CardAction className={actionClass}>
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
                            {link.address && link.url ? (
                                <AddressItem name={link.name} description={link.description} address={link.url} />
                            ) : link.url ? (
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
                    {app.installed && app.publicAddress && (
                        <li>
                            <AddressItem name="For friends" description="playit.gg" address={app.publicAddress} />
                        </li>
                    )}
                </ul>
                {app.installed && app.id === 'minecraft' && (
                    <p className="text-xs text-muted-foreground">
                        In Minecraft, go to Multiplayer &gt; Add Server and use one of the addresses above. Only players
                        on the whitelist can join: add your friends with Players.
                    </p>
                )}
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
    players: {
        running: 'Changing the players…',
        runningDetail: 'The server applies the change at once, without a restart.',
        failed: 'The players did not change',
    },
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
    const log = useRef<HTMLPreElement>(null)
    // An agent from before updates and removals sends no action.
    const text = operationText[operation.action ?? 'install']

    // Follow the log while it grows.
    useEffect(() => {
        log.current?.scrollTo({ top: log.current.scrollHeight })
    }, [operation.log])

    const logView = operation.log && (
        <pre
            ref={log}
            className="max-h-72 overflow-auto rounded-md bg-muted p-3 font-mono text-xs whitespace-pre-wrap text-foreground"
        >
            {operation.log}
        </pre>
    )

    if (operation.state === 'running') {
        return (
            <div className="flex flex-col gap-3">
                <Alert>
                    <Loader2Icon className="animate-spin" />
                    <AlertTitle>{text.running}</AlertTitle>
                    <AlertDescription>{text.runningDetail}</AlertDescription>
                </Alert>
                {logView}
            </div>
        )
    }

    if (operation.state === 'failed') {
        return (
            <Alert variant="destructive">
                <TriangleAlertIcon />
                <AlertTitle>{text.failed}</AlertTitle>
                <AlertDescription className="flex flex-col gap-2">
                    <span>
                        {operation.message}.
                        {operation.action === 'install' && ' If the install made a container, it removed it again.'}
                        {operation.action === 'vpn' && ' The old settings are back.'}
                        {rollback && ` ${rollback}`}
                    </span>
                    {logView && (
                        <details open>
                            <summary className="cursor-pointer">Log</summary>
                            <div className="mt-2">{logView}</div>
                        </details>
                    )}
                </AlertDescription>
            </Alert>
        )
    }

    return null
}

// AddressItem shows an address to type in a game, with a button to copy it.
function AddressItem({ name, description, address }: { name: string; description: string; address: string }) {
    async function copy() {
        try {
            await navigator.clipboard.writeText(address)
            toast.success(`Copied ${address}`)
        } catch {
            toast.error('Could not copy, select the address and copy it yourself')
        }
    }

    return (
        <div className="flex items-center justify-between gap-2 rounded-lg border px-3 py-2 text-sm">
            <span className="min-w-0">
                <span className="font-medium">{name}</span>
                <span className="text-muted-foreground"> · {description}</span>
                <span className="block truncate font-mono text-xs select-all">{address}</span>
            </span>
            <Button variant="ghost" size="icon-sm" aria-label={`Copy ${address}`} onClick={copy}>
                <CopyIcon />
            </Button>
        </div>
    )
}
