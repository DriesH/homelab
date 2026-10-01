import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ArrowUpCircleIcon, Loader2Icon, ShieldIcon, Trash2Icon } from 'lucide-react'
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
import { Button } from '@/components/ui/button'
import { api, type CatalogApp } from '@/lib/api'
import { appsQuery } from '@/lib/queries'
import { VpnDialog } from './vpn-dialog'

const updateText: Record<string, string> = {
    media: 'Homelab makes a snapshot, updates the packages, copies the stack of this Homelab release, and downloads the new images. The apps restart, so they are offline for a few minutes. Your settings in the apps stay.',
    jellyfin:
        'Homelab makes a snapshot, then updates Jellyfin and the other packages. Jellyfin restarts, so it is offline for a minute. Your settings, users and libraries stay.',
}

const removeText: Record<string, string> = {
    media: 'The downloads disk goes with it, so downloads that did not finish are lost. Your movies and series stay in the media folder, and Jellyfin keeps its libraries.',
    jellyfin:
        'Its settings, users and watch history go with it, and the Jellyfin page disconnects. Your movies and series stay in the media folder.',
}

// AppActions are the buttons of an app that Homelab installed: VPN (media stack only), Update and Remove.
export function AppActions({ app }: { app: CatalogApp }) {
    const queryClient = useQueryClient()
    const [confirm, setConfirm] = useState<'update' | 'remove' | null>(null)
    const [vpnOpen, setVpnOpen] = useState(false)

    const refresh = () => queryClient.invalidateQueries({ queryKey: appsQuery.queryKey })
    const update = useMutation({
        mutationFn: () => api.updateApp(app.id),
        onSuccess: refresh,
        onError: (error) => toast.error(`Could not update ${app.name}`, { description: error.message }),
    })
    const remove = useMutation({
        mutationFn: () => api.removeApp(app.id),
        onSuccess: refresh,
        onError: (error) => toast.error(`Could not remove ${app.name}`, { description: error.message }),
    })
    const busy = update.isPending || remove.isPending

    return (
        <>
            {app.id === 'media' && (
                <Button variant="outline" disabled={busy || app.status !== 'running'} onClick={() => setVpnOpen(true)}>
                    <ShieldIcon />
                    VPN
                </Button>
            )}
            <Button
                variant={app.updateAvailable ? 'default' : 'outline'}
                disabled={busy || app.status !== 'running'}
                onClick={() => setConfirm('update')}
            >
                {update.isPending ? <Loader2Icon className="animate-spin" /> : <ArrowUpCircleIcon />}
                Update
            </Button>
            <Button variant="destructive" disabled={busy} onClick={() => setConfirm('remove')}>
                {remove.isPending ? <Loader2Icon className="animate-spin" /> : <Trash2Icon />}
                Remove
            </Button>

            <VpnDialog app={app} open={vpnOpen} onOpenChange={setVpnOpen} />

            <AlertDialog open={confirm === 'update'} onOpenChange={(open) => !open && setConfirm(null)}>
                <AlertDialogContent>
                    <AlertDialogHeader>
                        <AlertDialogTitle>Update {app.name}?</AlertDialogTitle>
                        <AlertDialogDescription className="flex flex-col gap-2">
                            <span>{updateText[app.id]}</span>
                            {app.updateAvailable && (
                                <span>
                                    The stack in the container is from{' '}
                                    {app.version ? `Homelab ${app.version}` : 'an older Homelab release'}.
                                </span>
                            )}
                            <span>If the update fails, Homelab rolls the container back to the snapshot.</span>
                        </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                        <AlertDialogCancel>Cancel</AlertDialogCancel>
                        <AlertDialogAction
                            onClick={() => {
                                setConfirm(null)
                                update.mutate()
                            }}
                        >
                            Update
                        </AlertDialogAction>
                    </AlertDialogFooter>
                </AlertDialogContent>
            </AlertDialog>

            <AlertDialog open={confirm === 'remove'} onOpenChange={(open) => !open && setConfirm(null)}>
                <AlertDialogContent>
                    <AlertDialogHeader>
                        <AlertDialogTitle>Remove {app.name}?</AlertDialogTitle>
                        <AlertDialogDescription className="flex flex-col gap-2">
                            <span>
                                This removes container {app.vmid} with its disks and snapshots. {removeText[app.id]}
                            </span>
                            <span>
                                The backups of the container stay, so you can restore it on the Backups page. When no
                                other container uses the media folder, Homelab also removes its mount on the host. The
                                files stay.
                            </span>
                        </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                        <AlertDialogCancel>Cancel</AlertDialogCancel>
                        <AlertDialogAction
                            variant="destructive"
                            onClick={() => {
                                setConfirm(null)
                                remove.mutate()
                            }}
                        >
                            Remove
                        </AlertDialogAction>
                    </AlertDialogFooter>
                </AlertDialogContent>
            </AlertDialog>
        </>
    )
}
