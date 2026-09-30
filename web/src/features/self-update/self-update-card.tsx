import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ExternalLinkIcon, Loader2Icon, TriangleAlertIcon } from 'lucide-react'
import { toast } from 'sonner'

import {
    AlertDialog,
    AlertDialogAction,
    AlertDialogCancel,
    AlertDialogContent,
    AlertDialogDescription,
    AlertDialogFooter,
    AlertDialogHeader,
    AlertDialogTitle,
    AlertDialogTrigger,
} from '@/components/ui/alert-dialog'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardAction, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { api, type SelfUpdate } from '@/lib/api'
import { formatRelative } from '@/lib/format'
import { selfUpdateQuery } from '@/lib/queries'

export function SelfUpdateCard() {
    const { data, error } = useQuery(selfUpdateQuery)
    const queryClient = useQueryClient()

    const check = useMutation({
        mutationFn: api.checkSelfUpdate,
        onSuccess: (view) => queryClient.setQueryData(selfUpdateQuery.queryKey, view),
        onError: (error) => toast.error(error.message),
    })

    if (!data) {
        if (error) {
            return null
        }
        return <Skeleton className="h-40 w-full rounded-xl" />
    }

    const upgrading = data.installing || data.upgrade?.state === 'running'

    return (
        <Card>
            <CardHeader>
                <CardTitle className="flex items-center gap-2">
                    Homelab
                    <Badge variant="outline" className="font-mono">
                        {data.version}
                    </Badge>
                </CardTitle>
                <CardDescription>
                    {data.checkedAt ? `Checked ${formatRelative(data.checkedAt)}` : 'Not checked yet'}
                    {data.autoInstall ? ' · installs new versions automatically' : ''}
                </CardDescription>
                <CardAction>
                    <Button
                        variant="outline"
                        disabled={check.isPending || upgrading}
                        onClick={() => check.mutate()}
                        aria-label="Check for a new Homelab version"
                    >
                        Check now
                    </Button>
                </CardAction>
            </CardHeader>
            <CardContent className="flex flex-col gap-4">
                <UpgradeState view={data} />
                {data.error && (
                    <Alert variant="destructive">
                        <TriangleAlertIcon />
                        <AlertTitle>Could not check for a new version</AlertTitle>
                        <AlertDescription>{data.error}</AlertDescription>
                    </Alert>
                )}
                {data.updateAvailable && data.latest && !upgrading && <NewVersion view={data} />}
                {!data.updateAvailable && data.latest && !data.error && !upgrading && (
                    <p className="text-sm text-muted-foreground">You have the latest version.</p>
                )}
            </CardContent>
        </Card>
    )
}

function UpgradeState({ view }: { view: SelfUpdate }) {
    const { upgrade } = view

    if (view.installing) {
        return (
            <Alert>
                <Loader2Icon className="animate-spin" />
                <AlertTitle>Downloading {view.latest?.version}…</AlertTitle>
            </Alert>
        )
    }
    if (upgrade?.state === 'running') {
        const restarted = upgrade.version === view.version

        return (
            <Alert>
                <Loader2Icon className="animate-spin" />
                <AlertTitle>
                    {restarted ? `Making sure that ${upgrade.version} works…` : `Installing ${upgrade.version}…`}
                </AlertTitle>
                <AlertDescription>
                    {restarted
                        ? 'If it does not start correctly, the previous version comes back.'
                        : 'The manager restarts. This page reloads when the new version runs.'}
                </AlertDescription>
            </Alert>
        )
    }
    // Only show a failure for a version that is still newer than the running one.
    if (upgrade?.state === 'failed' && upgrade.version !== view.version && view.updateAvailable) {
        return (
            <Alert variant="destructive">
                <TriangleAlertIcon />
                <AlertTitle>Installing {upgrade.version} failed</AlertTitle>
                <AlertDescription className="flex flex-col gap-2">
                    <span>{upgrade.message}</span>
                    {upgrade.log && (
                        <details>
                            <summary className="cursor-pointer">Show log</summary>
                            <pre className="mt-2 max-h-64 overflow-auto rounded-md bg-muted p-3 font-mono text-xs whitespace-pre-wrap text-foreground">
                                {upgrade.log}
                            </pre>
                        </details>
                    )}
                </AlertDescription>
            </Alert>
        )
    }

    return null
}

function NewVersion({ view }: { view: SelfUpdate }) {
    const queryClient = useQueryClient()
    const [confirmOpen, setConfirmOpen] = useState(false)
    const latest = view.latest!

    const install = useMutation({
        mutationFn: api.installSelfUpdate,
        onMutate() {
            queryClient.setQueryData(selfUpdateQuery.queryKey, { ...view, installing: true })
        },
        onSettled: () => queryClient.invalidateQueries({ queryKey: selfUpdateQuery.queryKey }),
        onError: (error) => toast.error(error.message),
    })

    return (
        <div className="flex flex-col gap-3 rounded-lg border p-4">
            <div className="flex flex-wrap items-center gap-3">
                <Badge>{latest.version} is available</Badge>
                <a
                    href={latest.url}
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
                >
                    Release notes
                    <ExternalLinkIcon className="size-3.5" />
                </a>
                <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
                    <AlertDialogTrigger render={<Button className="ml-auto" disabled={install.isPending} />}>
                        Install {latest.version}
                    </AlertDialogTrigger>
                    <AlertDialogContent>
                        <AlertDialogHeader>
                            <AlertDialogTitle>Install {latest.version}?</AlertDialogTitle>
                            <AlertDialogDescription>
                                The manager and the host agent restart, so this page is gone for a moment. If the new
                                version does not start, the previous version comes back.
                            </AlertDialogDescription>
                        </AlertDialogHeader>
                        <AlertDialogFooter>
                            <AlertDialogCancel>Cancel</AlertDialogCancel>
                            <AlertDialogAction
                                onClick={() => {
                                    setConfirmOpen(false)
                                    install.mutate()
                                }}
                            >
                                Install
                            </AlertDialogAction>
                        </AlertDialogFooter>
                    </AlertDialogContent>
                </AlertDialog>
            </div>
            {latest.notes && <ReleaseNotes notes={latest.notes} />}
        </div>
    )
}

export function SelfUpdateSourceCard() {
    const { data } = useQuery(selfUpdateQuery)

    if (!data) {
        return null
    }

    return <SourceForm key={data.repo + data.tokenSet} view={data} />
}

function SourceForm({ view }: { view: SelfUpdate }) {
    const queryClient = useQueryClient()
    const [repo, setRepo] = useState(view.repo)
    const [token, setToken] = useState('')
    const [autoInstall, setAutoInstall] = useState(view.autoInstall)

    const save = useMutation({
        mutationFn: api.saveSelfUpdateSettings,
        onSuccess(_, settings) {
            setToken('')
            toast.success(settings.clearToken ? 'Token removed' : 'Update settings saved')
            queryClient.invalidateQueries({ queryKey: selfUpdateQuery.queryKey })
        },
        onError: (error) => toast.error(error.message),
    })

    function handleSubmit(event: FormEvent) {
        event.preventDefault()
        save.mutate({ repo, token, clearToken: false, autoInstall })
    }

    return (
        <Card>
            <form onSubmit={handleSubmit} className="flex flex-col gap-6">
                <CardHeader>
                    <CardTitle>Homelab updates</CardTitle>
                    <CardDescription>
                        New versions come from the releases of this GitHub repo. A private repo needs a fine-grained
                        token with read-only access to Contents of this repo only.
                    </CardDescription>
                </CardHeader>
                <CardContent className="flex flex-col gap-4">
                    <div className="flex flex-col gap-2">
                        <Label htmlFor="update-repo">GitHub repo</Label>
                        <Input
                            id="update-repo"
                            required
                            placeholder="owner/homelab"
                            value={repo}
                            onChange={(event) => setRepo(event.target.value)}
                        />
                    </div>
                    <div className="flex flex-col gap-2">
                        <Label htmlFor="update-token">Token</Label>
                        <Input
                            id="update-token"
                            type="password"
                            autoComplete="off"
                            placeholder={view.tokenSet ? 'Saved. Type to replace it.' : 'Only for a private repo'}
                            value={token}
                            onChange={(event) => setToken(event.target.value)}
                        />
                    </div>
                    <label className="flex items-center justify-between gap-4 text-sm">
                        <span className="flex flex-col">
                            <span className="font-medium">Install new versions automatically</span>
                            <span className="text-muted-foreground">
                                When this is off, you get a message and install with the button.
                            </span>
                        </span>
                        <Switch checked={autoInstall} onCheckedChange={setAutoInstall} />
                    </label>
                </CardContent>
                <CardFooter className="gap-2">
                    <Button type="submit" disabled={save.isPending}>
                        Save
                    </Button>
                    {view.tokenSet && (
                        <Button
                            type="button"
                            variant="outline"
                            disabled={save.isPending}
                            onClick={() => save.mutate({ repo: view.repo, token: '', clearToken: true, autoInstall })}
                        >
                            Remove token
                        </Button>
                    )}
                </CardFooter>
            </form>
        </Card>
    )
}

// ReleaseNotes shows the headings and list items of GitHub's generated notes
// without a Markdown library. Everything is rendered as text.
function ReleaseNotes({ notes }: { notes: string }) {
    const lines = notes.split('\n').filter((line) => line.trim() !== '')

    return (
        <div className="flex max-h-48 flex-col gap-1 overflow-auto text-sm text-muted-foreground">
            {lines.map((line, index) => {
                const heading = line.match(/^#+\s+(.*)/)
                if (heading) {
                    return (
                        <p key={index} className="font-medium text-foreground">
                            {heading[1]}
                        </p>
                    )
                }

                const item = line.match(/^\s*[-*]\s+(.*)/)
                if (item) {
                    return (
                        <p key={index} className="pl-3 before:mr-2 before:content-['•']">
                            {item[1]}
                        </p>
                    )
                }

                return <p key={index}>{line}</p>
            })}
        </div>
    )
}
