import { useQuery } from '@tanstack/react-query'
import { TriangleAlertIcon } from 'lucide-react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import { tailscaleQuery } from '@/lib/queries'
import { ConnectCard } from './connect-card'
import { ConnectedCard, DeviceList, ServeCard, SubnetCard } from './connected'
import { ServicesCard } from './services-card'

export function TailscalePage() {
    const { data, error, isPending } = useQuery(tailscaleQuery)

    if (isPending) {
        return (
            <div className="flex flex-col gap-6">
                <Skeleton className="h-10 w-64" />
                <Skeleton className="h-40 w-full rounded-xl" />
            </div>
        )
    }

    if (error) {
        return (
            <Alert variant="destructive">
                <TriangleAlertIcon />
                <AlertTitle>Could not load Tailscale</AlertTitle>
                <AlertDescription>{error.message}</AlertDescription>
            </Alert>
        )
    }

    const running = data.state === 'Running'

    return (
        <div className="flex flex-col gap-8">
            <header>
                <h1 className="text-lg font-semibold">Tailscale</h1>
                <p className="text-sm text-muted-foreground">
                    Open Homelab and your home network from anywhere, without opening ports on your router.
                </p>
            </header>

            {!data.installed ? (
                <Alert>
                    <TriangleAlertIcon />
                    <AlertTitle>Tailscale is not installed</AlertTitle>
                    <AlertDescription>
                        Install the newest Homelab version from the Updates page. The update installs Tailscale in the
                        manager container.
                    </AlertDescription>
                </Alert>
            ) : running ? (
                <>
                    <ConnectedCard tailscale={data} />
                    <div className="grid gap-4 lg:grid-cols-2">
                        <ServeCard tailscale={data} />
                        <SubnetCard key={data.settings.subnet} tailscale={data} />
                    </div>
                    <ServicesCard tailscale={data} />
                    <section className="flex flex-col gap-3">
                        <h2 className="text-lg font-semibold">Devices</h2>
                        <DeviceList peers={data.peers} />
                    </section>
                </>
            ) : (
                <ConnectCard tailscale={data} />
            )}
        </div>
    )
}
