import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import {
    ChartLineIcon,
    EllipsisVerticalIcon,
    PlayIcon,
    PowerIcon,
    RotateCwIcon,
    SquareIcon,
    TerminalIcon,
} from 'lucide-react'
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
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuSeparator,
    DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { api, type Guest, type GuestAction } from '@/lib/api'
import { formatBytes, formatUptime } from '@/lib/format'
import { overviewQuery } from '@/lib/queries'
import { cn } from '@/lib/utils'
import { UsageCharts } from './usage-charts'

const actionLabels: Record<GuestAction, string> = {
    start: 'Start',
    shutdown: 'Shut down',
    reboot: 'Reboot',
    stop: 'Force stop',
}

export function GuestList({ guests }: { guests: Guest[] }) {
    if (guests.length === 0) {
        return <Card className="p-6 text-center text-sm text-muted-foreground">No containers or VMs yet.</Card>
    }

    return (
        <Card className="gap-0 py-0">
            <ul className="divide-y">
                {guests.map((guest) => (
                    <GuestRow key={guest.id} guest={guest} />
                ))}
            </ul>
        </Card>
    )
}

function GuestRow({ guest }: { guest: Guest }) {
    const queryClient = useQueryClient()
    const [confirmStop, setConfirmStop] = useState(false)
    const [showHistory, setShowHistory] = useState(false)
    const running = guest.status === 'running'

    const action = useMutation({
        mutationFn: (guestAction: GuestAction) => api.guestAction(guest, guestAction),
        onSuccess(_, guestAction) {
            toast.success(`${actionLabels[guestAction]} sent to ${guest.name}`)
            // Proxmox runs the action as a task, so give it a moment before refreshing.
            setTimeout(() => queryClient.invalidateQueries({ queryKey: overviewQuery.queryKey }), 2000)
        },
        onError(error, guestAction) {
            toast.error(`${actionLabels[guestAction]} failed for ${guest.name}`, { description: error.message })
        },
    })

    return (
        <li className="flex items-center gap-4 px-4 py-3">
            <span
                className={cn('size-2 shrink-0 rounded-full', running ? 'bg-emerald-500' : 'bg-muted-foreground/40')}
                aria-label={guest.status}
            />

            <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                    <span className="truncate font-medium">{guest.name}</span>
                    <Badge variant="outline" className="font-mono">
                        {guest.type === 'lxc' ? 'LXC' : 'VM'} {guest.vmid}
                    </Badge>
                </div>
                <p className="text-sm text-muted-foreground">
                    {running ? `up ${formatUptime(guest.uptime)}` : guest.status}
                </p>
            </div>

            {running && (
                <dl className="hidden gap-6 text-right text-sm tabular-nums sm:flex">
                    <div>
                        <dt className="text-muted-foreground">CPU</dt>
                        <dd>{Math.round(guest.cpu * 100)}%</dd>
                    </div>
                    <div>
                        <dt className="text-muted-foreground">Memory</dt>
                        <dd>
                            {formatBytes(guest.mem)} / {formatBytes(guest.maxMem)}
                        </dd>
                    </div>
                </dl>
            )}

            <DropdownMenu>
                <DropdownMenuTrigger
                    render={<Button variant="ghost" size="icon" disabled={action.isPending} />}
                    aria-label={`Actions for ${guest.name}`}
                >
                    <EllipsisVerticalIcon />
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="w-40">
                    <DropdownMenuItem onClick={() => setShowHistory(true)}>
                        <ChartLineIcon /> History
                    </DropdownMenuItem>
                    <DropdownMenuSeparator />
                    {running ? (
                        <>
                            {guest.type === 'lxc' && (
                                <>
                                    <DropdownMenuItem
                                        render={<Link to="/console/$vmid" params={{ vmid: String(guest.vmid) }} />}
                                    >
                                        <TerminalIcon /> Console
                                    </DropdownMenuItem>
                                    <DropdownMenuSeparator />
                                </>
                            )}
                            <DropdownMenuItem onClick={() => action.mutate('reboot')}>
                                <RotateCwIcon /> Reboot
                            </DropdownMenuItem>
                            <DropdownMenuItem onClick={() => action.mutate('shutdown')}>
                                <PowerIcon /> Shut down
                            </DropdownMenuItem>
                            <DropdownMenuSeparator />
                            <DropdownMenuItem variant="destructive" onClick={() => setConfirmStop(true)}>
                                <SquareIcon /> Force stop
                            </DropdownMenuItem>
                        </>
                    ) : (
                        <DropdownMenuItem onClick={() => action.mutate('start')}>
                            <PlayIcon /> Start
                        </DropdownMenuItem>
                    )}
                </DropdownMenuContent>
            </DropdownMenu>

            <Dialog open={showHistory} onOpenChange={setShowHistory}>
                <DialogContent className="sm:max-w-2xl">
                    <DialogHeader>
                        <DialogTitle>{guest.name}</DialogTitle>
                        <DialogDescription>
                            {guest.type === 'lxc' ? 'Container' : 'VM'} {guest.vmid}. CPU is the share of its{' '}
                            {guest.maxCpu} cores.
                        </DialogDescription>
                    </DialogHeader>
                    {showHistory && <UsageCharts target={guest} />}
                </DialogContent>
            </Dialog>

            <AlertDialog open={confirmStop} onOpenChange={setConfirmStop}>
                <AlertDialogContent>
                    <AlertDialogHeader>
                        <AlertDialogTitle>Force stop {guest.name}?</AlertDialogTitle>
                        <AlertDialogDescription>
                            This is like pulling the power cable. Unsaved data can be lost. Use "Shut down" when you
                            can.
                        </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                        <AlertDialogCancel>Cancel</AlertDialogCancel>
                        <AlertDialogAction
                            variant="destructive"
                            onClick={() => {
                                setConfirmStop(false)
                                action.mutate('stop')
                            }}
                        >
                            Force stop
                        </AlertDialogAction>
                    </AlertDialogFooter>
                </AlertDialogContent>
            </AlertDialog>
        </li>
    )
}
