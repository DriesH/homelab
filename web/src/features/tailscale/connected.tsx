import { useState, type FormEvent } from 'react'
import { ExternalLinkIcon, TriangleAlertIcon } from 'lucide-react'

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
import { Button, buttonVariants } from '@/components/ui/button'
import { Card, CardAction, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { api, type Tailscale } from '@/lib/api'
import { formatRelative } from '@/lib/format'
import { cn } from '@/lib/utils'
import { useTailscaleMutation } from './use-tailscale-mutation'

const adminConsole = 'https://login.tailscale.com/admin/machines'

export function ConnectedCard({ tailscale }: { tailscale: Tailscale }) {
    const [confirmOpen, setConfirmOpen] = useState(false)
    const logout = useTailscaleMutation(api.logoutTailscale, () => 'Disconnected from Tailscale')

    return (
        <Card>
            <CardHeader>
                <CardTitle className="flex items-center gap-2">
                    <span className="size-2.5 rounded-full bg-emerald-500" aria-hidden />
                    Connected as {tailscale.name}
                </CardTitle>
                <CardDescription>
                    {tailscale.tailnet ? `Tailnet ${tailscale.tailnet}` : 'Connected to your tailnet'}
                </CardDescription>
                <CardAction>
                    <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
                        <AlertDialogTrigger render={<Button variant="outline" disabled={logout.isPending} />}>
                            Disconnect
                        </AlertDialogTrigger>
                        <AlertDialogContent>
                            <AlertDialogHeader>
                                <AlertDialogTitle>Disconnect from Tailscale?</AlertDialogTitle>
                                <AlertDialogDescription>
                                    Homelab leaves your tailnet. You can then only reach it from your home network.
                                    Connecting again needs a new login.
                                </AlertDialogDescription>
                            </AlertDialogHeader>
                            <AlertDialogFooter>
                                <AlertDialogCancel>Cancel</AlertDialogCancel>
                                <AlertDialogAction
                                    onClick={() => {
                                        setConfirmOpen(false)
                                        logout.mutate(undefined)
                                    }}
                                >
                                    Disconnect
                                </AlertDialogAction>
                            </AlertDialogFooter>
                        </AlertDialogContent>
                    </AlertDialog>
                </CardAction>
            </CardHeader>
            <CardContent className="flex flex-col gap-4">
                <dl className="grid gap-x-6 gap-y-2 text-sm sm:grid-cols-[auto_1fr]">
                    <dt className="text-muted-foreground">Name</dt>
                    <dd className="font-mono break-all">{tailscale.dnsName}</dd>
                    <dt className="text-muted-foreground">Addresses</dt>
                    <dd className="font-mono break-all">{tailscale.ips.join(', ')}</dd>
                </dl>
                {tailscale.authUrl && (
                    <Alert>
                        <TriangleAlertIcon />
                        <AlertTitle>Finish the login</AlertTitle>
                        <AlertDescription className="flex flex-col items-start gap-3">
                            <span>
                                Tailscale waits for you to log in again, for example after "Use tag:homelab". Until
                                then, Homelab keeps its old key.
                            </span>
                            <a
                                href={tailscale.authUrl}
                                target="_blank"
                                rel="noreferrer"
                                className={buttonVariants({ size: 'sm' })}
                            >
                                Open the Tailscale login
                                <ExternalLinkIcon />
                            </a>
                        </AlertDescription>
                    </Alert>
                )}
                {tailscale.health.length > 0 && (
                    <Alert>
                        <TriangleAlertIcon />
                        <AlertTitle>Tailscale reports</AlertTitle>
                        <AlertDescription>
                            <ul className="list-disc pl-4">
                                {tailscale.health.map((message) => (
                                    <li key={message}>{message}</li>
                                ))}
                            </ul>
                        </AlertDescription>
                    </Alert>
                )}
            </CardContent>
        </Card>
    )
}

export function ServeCard({ tailscale }: { tailscale: Tailscale }) {
    const serve = useTailscaleMutation(api.setTailscaleServe, (enabled) =>
        enabled ? 'Homelab is now on your tailnet' : 'Homelab is off your tailnet',
    )

    return (
        <Card>
            <CardHeader>
                <CardTitle>Open Homelab from anywhere</CardTitle>
                <CardDescription>
                    Tailscale Serve gives Homelab an HTTPS address with a trusted certificate. Only devices in your
                    tailnet can open it.
                </CardDescription>
                <CardAction>
                    <Switch
                        checked={tailscale.serving}
                        disabled={serve.isPending}
                        onCheckedChange={(enabled) => serve.mutate(enabled)}
                        aria-label="Open Homelab from anywhere"
                    />
                </CardAction>
            </CardHeader>
            {tailscale.serving && tailscale.serveUrl && (
                <CardContent>
                    <a
                        href={tailscale.serveUrl}
                        target="_blank"
                        rel="noreferrer"
                        className="inline-flex items-center gap-1 font-mono text-sm break-all underline-offset-4 hover:underline"
                    >
                        {tailscale.serveUrl}
                        <ExternalLinkIcon className="size-3.5 shrink-0" />
                    </a>
                </CardContent>
            )}
        </Card>
    )
}

export function SubnetCard({ tailscale }: { tailscale: Tailscale }) {
    const [shareSubnet, setShareSubnet] = useState(tailscale.settings.shareSubnet)
    const [subnet, setSubnet] = useState(tailscale.settings.subnet || tailscale.suggestedSubnet || '')
    const save = useTailscaleMutation(api.saveTailscaleSettings, (settings) =>
        settings.shareSubnet ? 'Home network shared' : 'Home network no longer shared',
    )

    function handleSubmit(event: FormEvent) {
        event.preventDefault()
        save.mutate({ shareSubnet, subnet })
    }

    const shared = tailscale.settings.shareSubnet

    return (
        <Card>
            <form onSubmit={handleSubmit} className="flex flex-col gap-6">
                <CardHeader>
                    <CardTitle>Share your home network</CardTitle>
                    <CardDescription>
                        Your devices in the tailnet can then reach Jellyfin, the NAS and other devices at home by their
                        normal address.
                    </CardDescription>
                    <CardAction>
                        <Switch
                            checked={shareSubnet}
                            onCheckedChange={setShareSubnet}
                            aria-label="Share your home network"
                        />
                    </CardAction>
                </CardHeader>
                <CardContent className="flex flex-col gap-3">
                    <div className="flex flex-col gap-2">
                        <Label htmlFor="subnet">Home network</Label>
                        <Input
                            id="subnet"
                            className="font-mono"
                            placeholder="192.168.1.0/24"
                            disabled={!shareSubnet}
                            value={subnet}
                            onChange={(event) => setSubnet(event.target.value)}
                        />
                    </div>
                    {shared &&
                        (tailscale.subnetApproved ? (
                            <Badge variant="secondary">Approved in Tailscale</Badge>
                        ) : (
                            <p className="text-sm text-muted-foreground">
                                Approve the route in the{' '}
                                <a href={adminConsole} target="_blank" rel="noreferrer" className="underline">
                                    Tailscale admin console
                                </a>
                                : open Homelab, then Edit route settings.
                            </p>
                        ))}
                </CardContent>
                <CardFooter>
                    <Button type="submit" disabled={save.isPending}>
                        Save
                    </Button>
                </CardFooter>
            </form>
        </Card>
    )
}

export function DeviceList({ peers }: { peers: Tailscale['peers'] }) {
    if (peers.length === 0) {
        return (
            <Card className="p-6 text-center text-sm text-muted-foreground">
                No other devices yet. Install Tailscale on your phone or laptop and log in with the same account.
            </Card>
        )
    }

    return (
        <Card className="gap-0 py-0">
            <ul className="divide-y">
                {peers.map((peer) => (
                    <li key={peer.dnsName || peer.name} className="flex items-center gap-3 px-4 py-3">
                        <span
                            className={cn(
                                'size-2.5 shrink-0 rounded-full',
                                peer.online ? 'bg-emerald-500' : 'bg-muted-foreground/40',
                            )}
                            aria-label={peer.online ? 'online' : 'offline'}
                        />
                        <div className="flex min-w-0 flex-1 flex-col">
                            <span className="truncate font-medium">{peer.name}</span>
                            <span className="truncate font-mono text-xs text-muted-foreground">{peer.ips[0]}</span>
                        </div>
                        <span className="text-sm text-muted-foreground">{peer.os}</span>
                        <span className="hidden w-32 text-right text-sm text-muted-foreground sm:inline">
                            {peer.online ? 'online' : peer.lastSeen ? formatRelative(peer.lastSeen) : 'offline'}
                        </span>
                    </li>
                ))}
            </ul>
        </Card>
    )
}
