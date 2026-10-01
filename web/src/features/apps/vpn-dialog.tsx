import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2Icon } from 'lucide-react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { api, type CatalogApp } from '@/lib/api'
import { appsQuery } from '@/lib/queries'
import { Field } from './form-parts'

type VpnDialogProps = { app: CatalogApp; open: boolean; onOpenChange: (open: boolean) => void }

export function VpnDialog({ app, open, onOpenChange }: VpnDialogProps) {
    const { data, error, isFetching } = useQuery({
        queryKey: ['apps', app.id, 'vpn'],
        queryFn: () => api.vpnSettings(app.id),
        enabled: open,
        staleTime: 0,
    })

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="sm:max-w-md">
                <DialogHeader>
                    <DialogTitle>VPN settings</DialogTitle>
                    <DialogDescription>
                        qBittorrent, Prowlarr and FlareSolverr restart with the new settings, so downloads stop for a
                        minute. If the VPN does not connect, the old settings come back.
                    </DialogDescription>
                </DialogHeader>
                {error ? (
                    <p className="text-sm text-destructive">{error.message}</p>
                ) : data && !isFetching ? (
                    <VpnForm app={app} countries={data.countries} onDone={() => onOpenChange(false)} />
                ) : (
                    <Skeleton className="h-40 w-full" />
                )}
            </DialogContent>
        </Dialog>
    )
}

function VpnForm({ app, countries: current, onDone }: { app: CatalogApp; countries: string; onDone: () => void }) {
    const queryClient = useQueryClient()
    const [countries, setCountries] = useState(current)
    const [key, setKey] = useState('')

    const change = useMutation({
        mutationFn: () => api.changeVpn(app.id, { countries: countries.trim(), wireguardPrivateKey: key }),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: appsQuery.queryKey })
            onDone()
        },
        onError: (error) => toast.error('Could not change the VPN settings', { description: error.message }),
    })

    const submit = (event: FormEvent) => {
        event.preventDefault()
        change.mutate()
    }

    return (
        <form onSubmit={submit} className="flex flex-col gap-4">
            <Field id="vpn-countries" label="Server countries" help="Separate them with commas.">
                <Input
                    id="vpn-countries"
                    value={countries}
                    onChange={(event) => setCountries(event.target.value)}
                    required
                />
            </Field>
            <Field
                id="vpn-key"
                label="WireGuard private key"
                help={
                    <>
                        Leave it empty to keep the current key. For a new key, go to{' '}
                        <a
                            className="underline"
                            href="https://account.proton.me/u/0/vpn/WireGuard"
                            target="_blank"
                            rel="noreferrer"
                        >
                            account.proton.me
                        </a>{' '}
                        and turn on NAT-PMP (port forwarding).
                    </>
                }
            >
                <Input
                    id="vpn-key"
                    type="password"
                    autoComplete="off"
                    value={key}
                    onChange={(event) => setKey(event.target.value.trim())}
                    placeholder="Keep the current key"
                />
            </Field>
            <DialogFooter>
                <Button type="submit" disabled={change.isPending}>
                    {change.isPending && <Loader2Icon className="animate-spin" />}
                    Save and restart the VPN
                </Button>
            </DialogFooter>
        </form>
    )
}
