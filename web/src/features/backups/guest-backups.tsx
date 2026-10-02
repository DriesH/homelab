import { useState } from 'react'
import { ChevronRightIcon, LockIcon } from 'lucide-react'

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
import { Switch } from '@/components/ui/switch'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { api, type Backups, type GuestBackup } from '@/lib/api'
import { formatBytes, formatRelative } from '@/lib/format'
import { cn } from '@/lib/utils'
import { jobSettings, useBackupAction, useSaveBackupJob } from './use-backups'

type Guest = Backups['guests'][number]

type Confirm = { kind: 'restore' | 'delete'; guest: Guest; backup: GuestBackup }

const dateFormat = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' })

export function GuestBackups({ backups }: { backups: Backups }) {
    const [confirm, setConfirm] = useState<Confirm | null>(null)
    const action = useBackupAction()

    if (backups.guests.length === 0) {
        return <Card className="p-6 text-center text-sm text-muted-foreground">No containers or VMs yet.</Card>
    }

    function runConfirmed() {
        if (!confirm) {
            return
        }
        const { kind, guest, backup } = confirm
        setConfirm(null)
        action.mutate(() =>
            kind === 'restore' ? api.restoreBackup(guest.vmid, backup.volid) : api.deleteBackup(backup.volid),
        )
    }

    return (
        <>
            <Card className="gap-0 py-0">
                <ul className="divide-y">
                    {backups.guests.map((guest) => (
                        <GuestRow
                            key={guest.vmid}
                            guest={guest}
                            backups={backups}
                            onBackUp={() => action.mutate(() => api.backUpGuest(guest.vmid))}
                            onConfirm={setConfirm}
                        />
                    ))}
                </ul>
            </Card>

            <AlertDialog open={confirm !== null} onOpenChange={(open) => !open && setConfirm(null)}>
                <AlertDialogContent>
                    {confirm && (
                        <>
                            <AlertDialogHeader>
                                <AlertDialogTitle>
                                    {confirm.kind === 'restore'
                                        ? `Restore ${confirm.guest.name}?`
                                        : 'Delete this backup?'}
                                </AlertDialogTitle>
                                <AlertDialogDescription>
                                    {confirm.kind === 'restore'
                                        ? `This replaces ${confirm.guest.name} with the backup of ${dateFormat.format(new Date(confirm.backup.createdAt))}. Everything that changed after that backup is lost. ${confirm.guest.status === 'running' ? 'The guest is shut down first and started again afterwards.' : ''}`
                                        : `The backup of ${confirm.guest.name} from ${dateFormat.format(new Date(confirm.backup.createdAt))} is deleted from ${confirm.backup.storage}. This can not be undone.`}
                                </AlertDialogDescription>
                            </AlertDialogHeader>
                            <AlertDialogFooter>
                                <AlertDialogCancel>Cancel</AlertDialogCancel>
                                <AlertDialogAction variant="destructive" onClick={runConfirmed}>
                                    {confirm.kind === 'restore' ? 'Restore' : 'Delete'}
                                </AlertDialogAction>
                            </AlertDialogFooter>
                        </>
                    )}
                </AlertDialogContent>
            </AlertDialog>
        </>
    )
}

type GuestRowProps = {
    guest: Guest
    backups: Backups
    onBackUp: () => void
    onConfirm: (confirm: Confirm) => void
}

function GuestRow({ guest, backups, onBackUp, onConfirm }: GuestRowProps) {
    const [open, setOpen] = useState(false)
    const save = useSaveBackupJob()
    const latest = guest.backups[0]
    const busy = Boolean(backups.busy)

    function setIncluded(included: boolean) {
        const exclude = backups.job.exclude.filter((vmid) => vmid !== guest.vmid)
        save.mutate(jobSettings(backups.job, { exclude: included ? exclude : [...exclude, guest.vmid] }))
    }

    return (
        <li className="flex flex-col gap-3 px-4 py-3">
            <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:gap-4">
                <button
                    type="button"
                    className="flex min-w-0 items-center gap-2 text-left disabled:cursor-default sm:flex-1"
                    onClick={() => setOpen(!open)}
                    disabled={guest.backups.length === 0}
                    aria-expanded={open}
                >
                    <ChevronRightIcon
                        className={cn(
                            'size-4 shrink-0 text-muted-foreground transition-transform',
                            open && 'rotate-90',
                            guest.backups.length === 0 && 'invisible',
                        )}
                    />
                    <span className="truncate font-medium">{guest.name}</span>
                    <Badge variant="outline" className="font-mono">
                        {guest.type === 'lxc' ? 'LXC' : 'VM'} {guest.vmid}
                    </Badge>
                    <span className="truncate text-sm text-muted-foreground">
                        {latest
                            ? `${formatRelative(latest.createdAt)} · ${guest.backups.length} ${guest.backups.length === 1 ? 'backup' : 'backups'}`
                            : 'no backups'}
                    </span>
                </button>

                <div className="flex items-center justify-between gap-4 pl-6 sm:pl-0">
                    <IncludedSwitch
                        guest={guest}
                        jobActive={backups.job.exists && backups.job.enabled}
                        disabled={save.isPending}
                        onChange={setIncluded}
                    />
                    <Button variant="outline" size="sm" disabled={busy} onClick={onBackUp}>
                        Back up now
                    </Button>
                </div>
            </div>

            {open && (
                <ul className="flex flex-col divide-y rounded-lg border text-sm">
                    {guest.backups.map((backup) => (
                        <li key={backup.volid} className="flex flex-wrap items-center gap-x-4 gap-y-1 px-3 py-2">
                            <span className="flex min-w-0 flex-1 items-center gap-2">
                                {dateFormat.format(new Date(backup.createdAt))}
                                {backup.protected && (
                                    <LockIcon className="size-3.5 text-muted-foreground" aria-label="protected" />
                                )}
                            </span>
                            <span className="text-muted-foreground">{backup.storage}</span>
                            <span className="w-16 text-right text-muted-foreground tabular-nums">
                                {formatBytes(backup.size)}
                            </span>
                            <RestoreButton
                                guest={guest}
                                disabled={busy}
                                onClick={() => onConfirm({ kind: 'restore', guest, backup })}
                            />
                            <Button
                                variant="ghost"
                                size="sm"
                                disabled={busy || backup.protected}
                                onClick={() => onConfirm({ kind: 'delete', guest, backup })}
                            >
                                Delete
                            </Button>
                        </li>
                    ))}
                </ul>
            )}
        </li>
    )
}

function RestoreButton({ guest, disabled, onClick }: { guest: Guest; disabled: boolean; onClick: () => void }) {
    const button = (
        <Button variant="outline" size="sm" disabled={disabled || guest.self} onClick={onClick}>
            Restore
        </Button>
    )

    if (!guest.self) {
        return button
    }

    return (
        <Tooltip>
            <TooltipTrigger render={<span />}>{button}</TooltipTrigger>
            <TooltipContent>
                Homelab runs in this container. To restore it, run <code>homelab-restore</code> on the Proxmox host.
            </TooltipContent>
        </Tooltip>
    )
}

type IncludedSwitchProps = {
    guest: Guest
    jobActive: boolean
    disabled: boolean
    onChange: (included: boolean) => void
}

function IncludedSwitch({ guest, jobActive, disabled, onChange }: IncludedSwitchProps) {
    const control = (
        <label className="flex items-center gap-2 text-sm text-muted-foreground">
            Scheduled
            <Switch
                checked={guest.included}
                disabled={disabled || !jobActive}
                onCheckedChange={onChange}
                aria-label={`Back up ${guest.name} on the schedule`}
            />
        </label>
    )

    if (jobActive) {
        return control
    }

    return (
        <Tooltip>
            <TooltipTrigger render={<span />}>{control}</TooltipTrigger>
            <TooltipContent>Turn on the schedule first.</TooltipContent>
        </Tooltip>
    )
}
