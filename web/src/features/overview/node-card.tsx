import { Badge } from '@/components/ui/badge'
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import type { ProxmoxNode } from '@/lib/api'
import { formatBytes, formatUptime, percentage } from '@/lib/format'
import { UsageBar } from './usage-bar'
import { UsageCharts } from './usage-charts'

export function NodeCard({ node }: { node: ProxmoxNode }) {
    const cpu = Math.round(node.cpu * 100)

    return (
        <Card>
            <CardHeader>
                <CardTitle>{node.name}</CardTitle>
                <CardDescription>
                    {node.pveVersion || 'Proxmox VE'} · up {formatUptime(node.uptime)}
                </CardDescription>
                <CardAction>
                    <Badge variant={node.status === 'online' ? 'secondary' : 'destructive'}>{node.status}</Badge>
                </CardAction>
            </CardHeader>
            <CardContent className="flex flex-col gap-4">
                <UsageBar label="CPU" value={cpu} detail={`${cpu}% of ${node.maxCpu} cores`} />
                <UsageBar
                    label="Memory"
                    value={percentage(node.mem, node.maxMem)}
                    detail={`${formatBytes(node.mem)} / ${formatBytes(node.maxMem)}`}
                />
                <UsageBar
                    label="Root disk"
                    value={percentage(node.disk, node.maxDisk)}
                    detail={`${formatBytes(node.disk)} / ${formatBytes(node.maxDisk)}`}
                />
                <UsageCharts target={{ node: node.name }} />
            </CardContent>
        </Card>
    )
}
