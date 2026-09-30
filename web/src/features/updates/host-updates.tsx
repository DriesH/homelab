import { useState } from 'react'
import { RotateCwIcon, TriangleAlertIcon } from 'lucide-react'

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
import { Button } from '@/components/ui/button'
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { api, type UpdateTarget } from '@/lib/api'
import { formatRelative } from '@/lib/format'
import { PackageList } from './package-list'
import { useStartUpdate } from './use-updates'

export function HostUpdates({ host, busy }: { host: UpdateTarget; busy: boolean }) {
    const start = useStartUpdate()
    const [confirmOpen, setConfirmOpen] = useState(false)
    const count = host.packages.length

    return (
        <Card>
            <CardHeader>
                <CardTitle>Proxmox host</CardTitle>
                <CardDescription>
                    {host.checkedAt
                        ? `${count === 0 ? 'Up to date' : `${count} updates`} · checked ${formatRelative(host.checkedAt)}`
                        : 'Not checked yet'}
                </CardDescription>
                <CardAction>
                    <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
                        <AlertDialogTrigger render={<Button disabled={busy || count === 0} />}>
                            Install updates
                        </AlertDialogTrigger>
                        <AlertDialogContent>
                            <AlertDialogHeader>
                                <AlertDialogTitle>Update the Proxmox host?</AlertDialogTitle>
                                <AlertDialogDescription>
                                    This installs {count} updates with apt. The host has no snapshots, so a bad update
                                    can not be rolled back automatically. Containers keep running.
                                </AlertDialogDescription>
                            </AlertDialogHeader>
                            <AlertDialogFooter>
                                <AlertDialogCancel>Cancel</AlertDialogCancel>
                                <AlertDialogAction
                                    onClick={() => {
                                        setConfirmOpen(false)
                                        start.mutate(api.updateHost)
                                    }}
                                >
                                    Install
                                </AlertDialogAction>
                            </AlertDialogFooter>
                        </AlertDialogContent>
                    </AlertDialog>
                </CardAction>
            </CardHeader>
            <CardContent className="flex flex-col gap-3">
                {host.rebootRequired && (
                    <Alert>
                        <RotateCwIcon />
                        <AlertTitle>Reboot needed</AlertTitle>
                        <AlertDescription>A new kernel is installed. Reboot the host to use it.</AlertDescription>
                    </Alert>
                )}
                {host.error && (
                    <Alert variant="destructive">
                        <TriangleAlertIcon />
                        <AlertTitle>The last check failed</AlertTitle>
                        <AlertDescription>{host.error}</AlertDescription>
                    </Alert>
                )}
                <PackageList target={host} />
            </CardContent>
        </Card>
    )
}
