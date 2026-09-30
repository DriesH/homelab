import { useState, type FormEvent } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ExternalLinkIcon, Loader2Icon, TriangleAlertIcon } from 'lucide-react'
import { toast } from 'sonner'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button, buttonVariants } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { api, type Tailscale } from '@/lib/api'
import { tailscaleQuery } from '@/lib/queries'

export function ConnectCard({ tailscale }: { tailscale: Tailscale }) {
    const queryClient = useQueryClient()
    const [authKey, setAuthKey] = useState('')

    const connect = useMutation({
        mutationFn: api.connectTailscale,
        onSuccess() {
            setAuthKey('')
            queryClient.invalidateQueries({ queryKey: tailscaleQuery.queryKey })
        },
        onError: (error) => toast.error(error.message),
    })

    function connectWithKey(event: FormEvent) {
        event.preventDefault()
        connect.mutate(authKey)
    }

    const waiting = tailscale.connecting || connect.isPending

    return (
        <Card>
            <CardHeader>
                <CardTitle>Connect to Tailscale</CardTitle>
                <CardDescription>
                    Homelab joins your tailnet as a device. Then all your devices with Tailscale can reach it.
                </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-6">
                {tailscale.error && !waiting && (
                    <Alert variant="destructive">
                        <TriangleAlertIcon />
                        <AlertTitle>The last login failed</AlertTitle>
                        <AlertDescription>{tailscale.error}</AlertDescription>
                    </Alert>
                )}

                {waiting && tailscale.authUrl ? (
                    <div className="flex flex-col items-start gap-3">
                        <p className="text-sm">
                            Log in to Tailscale and approve this device. This page updates by itself.
                        </p>
                        <a href={tailscale.authUrl} target="_blank" rel="noreferrer" className={buttonVariants()}>
                            Open the Tailscale login
                            <ExternalLinkIcon />
                        </a>
                    </div>
                ) : waiting ? (
                    <p className="flex items-center gap-2 text-sm text-muted-foreground">
                        <Loader2Icon className="size-4 animate-spin" />
                        Connecting…
                    </p>
                ) : (
                    <Button className="self-start" onClick={() => connect.mutate('')}>
                        Connect with your account
                    </Button>
                )}

                <form onSubmit={connectWithKey} className="flex flex-col gap-2 border-t pt-6">
                    <Label htmlFor="auth-key">Or use an auth key</Label>
                    <div className="flex gap-2">
                        <Input
                            id="auth-key"
                            type="password"
                            autoComplete="off"
                            placeholder="tskey-auth-…"
                            value={authKey}
                            onChange={(event) => setAuthKey(event.target.value)}
                        />
                        <Button type="submit" variant="outline" disabled={waiting || !authKey}>
                            Connect
                        </Button>
                    </div>
                    <p className="text-xs text-muted-foreground">
                        Create one in the Tailscale admin console under Settings, then Keys.
                    </p>
                </form>
            </CardContent>
        </Card>
    )
}
