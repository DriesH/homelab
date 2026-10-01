import { useQuery } from '@tanstack/react-query'
import { AlertTriangleIcon } from 'lucide-react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { overviewQuery } from '@/lib/queries'
import { cn } from '@/lib/utils'
import { GuestList } from './guest-list'
import { NodeCard } from './node-card'

export function OverviewPage() {
    const { data, error, isPending } = useQuery(overviewQuery)

    if (isPending) {
        return (
            <div className="flex flex-col gap-6">
                <Skeleton className="h-44 w-full rounded-xl" />
                <Skeleton className="h-64 w-full rounded-xl" />
            </div>
        )
    }

    if (error) {
        return (
            <Alert variant="destructive">
                <AlertTriangleIcon />
                <AlertTitle>Could not load your server</AlertTitle>
                <AlertDescription>{error.message}</AlertDescription>
            </Alert>
        )
    }

    return (
        <div className="flex flex-col gap-8">
            <section className="flex flex-col gap-3">
                <div className="flex items-center justify-between gap-2">
                    <h2 className="text-lg font-semibold">Server</h2>
                    <Badge variant={data.agent.connected ? 'secondary' : 'destructive'}>
                        Host agent {data.agent.connected ? 'connected' : 'offline'}
                    </Badge>
                </div>
                <div className={cn('grid gap-4', data.nodes.length > 1 && 'md:grid-cols-2')}>
                    {data.nodes.map((node) => (
                        <NodeCard key={node.name} node={node} />
                    ))}
                </div>
            </section>

            <section className="flex flex-col gap-3">
                <h2 className="text-lg font-semibold">Containers and VMs</h2>
                <GuestList guests={data.guests} />
            </section>
        </div>
    )
}
