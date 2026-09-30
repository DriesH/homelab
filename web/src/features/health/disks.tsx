import { Badge } from '@/components/ui/badge'
import { Card } from '@/components/ui/card'
import { UsageBar } from '@/features/overview/usage-bar'
import type { Disk, Share, Storage, ZFSPool } from '@/lib/api'
import { formatBytes, percentage } from '@/lib/format'

export function DiskList({ disks }: { disks: Disk[] }) {
    if (disks.length === 0) {
        return <Card className="p-6 text-center text-sm text-muted-foreground">No disks found yet.</Card>
    }

    const nodes = new Set(disks.map((disk) => disk.node))

    return (
        <Card className="gap-0 py-0">
            <ul className="divide-y">
                {disks.map((disk) => (
                    <li
                        key={disk.node + disk.devPath}
                        className="flex flex-wrap items-center gap-x-4 gap-y-1 px-4 py-3"
                    >
                        <div className="flex min-w-0 basis-full items-center gap-2 sm:flex-1 sm:basis-auto">
                            <span className="font-mono text-sm">{disk.devPath}</span>
                            <span className="truncate text-sm text-muted-foreground">{disk.model}</span>
                            {nodes.size > 1 && <Badge variant="outline">{disk.node}</Badge>}
                        </div>
                        <span className="text-sm text-muted-foreground uppercase">{disk.type}</span>
                        <span className="text-sm tabular-nums sm:w-16 sm:text-right">{formatBytes(disk.size)}</span>
                        {disk.wearout !== null && (
                            <span className="text-sm text-muted-foreground tabular-nums">{disk.wearout}% worn</span>
                        )}
                        <SmartBadge disk={disk} className="ml-auto sm:ml-0" />
                    </li>
                ))}
            </ul>
        </Card>
    )
}

function SmartBadge({ disk, className }: { disk: Disk; className?: string }) {
    if (disk.problem) {
        return (
            <Badge variant="destructive" className={className}>
                {disk.health || 'Problem'}
            </Badge>
        )
    }
    if (!disk.health || disk.health === 'UNKNOWN') {
        return (
            <Badge variant="outline" className={className}>
                No SMART
            </Badge>
        )
    }

    return (
        <Badge variant="secondary" className={className}>
            SMART {disk.health}
        </Badge>
    )
}

export function PoolList({ pools }: { pools: ZFSPool[] }) {
    return (
        <div className="grid gap-4 sm:grid-cols-2">
            {pools.map((pool) => (
                <Card key={pool.node + pool.name} className="gap-3 px-5 py-4">
                    <div className="flex items-center gap-2">
                        <span className="font-medium">{pool.name}</span>
                        <Badge variant={pool.problem ? 'destructive' : 'secondary'} className="ml-auto">
                            {pool.health}
                        </Badge>
                    </div>
                    <UsageBar
                        label={`${pool.frag}% fragmented`}
                        value={percentage(pool.alloc, pool.size)}
                        detail={`${formatBytes(pool.alloc)} / ${formatBytes(pool.size)}`}
                    />
                </Card>
            ))}
        </div>
    )
}

export function StorageList({ storage }: { storage: Storage[] }) {
    if (storage.length === 0) {
        return <Card className="p-6 text-center text-sm text-muted-foreground">No storage found yet.</Card>
    }

    return (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {storage.map((item) => (
                <Card key={item.node + item.name} className="gap-3 px-5 py-4">
                    {item.total > 0 ? (
                        <UsageBar
                            label={item.name}
                            value={percentage(item.used, item.total)}
                            detail={`${formatBytes(item.used)} / ${formatBytes(item.total)}`}
                        />
                    ) : (
                        <div className="flex items-center justify-between text-sm">
                            <span className="text-muted-foreground">{item.name}</span>
                            <span>{item.status}</span>
                        </div>
                    )}
                </Card>
            ))}
        </div>
    )
}

export function ShareList({ shares }: { shares: Share[] }) {
    return (
        <div className="grid gap-4 sm:grid-cols-2">
            {shares.map((share) => (
                <Card key={share.path} className="gap-3 px-5 py-4">
                    <div className="flex items-start gap-2">
                        <div className="flex min-w-0 flex-col">
                            <span className="truncate font-medium">{share.source}</span>
                            <span className="truncate font-mono text-xs text-muted-foreground">{share.path}</span>
                        </div>
                        <ShareBadge share={share} />
                    </div>
                    {share.mounted && share.size > 0 && (
                        <UsageBar
                            label={share.fsType.toUpperCase()}
                            value={percentage(share.used, share.size)}
                            detail={`${formatBytes(share.used)} / ${formatBytes(share.size)}`}
                        />
                    )}
                </Card>
            ))}
        </div>
    )
}

function ShareBadge({ share }: { share: Share }) {
    if (!share.mounted) {
        return (
            <Badge variant="destructive" className="ml-auto">
                Not mounted
            </Badge>
        )
    }
    if (share.error) {
        return (
            <Badge variant="destructive" className="ml-auto">
                {share.error}
            </Badge>
        )
    }

    return (
        <Badge variant="secondary" className="ml-auto">
            Mounted
        </Badge>
    )
}
