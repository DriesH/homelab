import { useState } from 'react'
import { ChevronRightIcon } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Switch } from '@/components/ui/switch'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { api, type GuestUpdates, type Updates } from '@/lib/api'
import { formatRelative } from '@/lib/format'
import { cn } from '@/lib/utils'
import { PackageList } from './package-list'
import { settingsFrom, useSaveUpdateSettings, useStartUpdate } from './use-updates'

export function ContainerUpdates({ updates }: { updates: Updates }) {
    if (updates.guests.length === 0) {
        return <Card className="p-6 text-center text-sm text-muted-foreground">No containers yet.</Card>
    }

    return (
        <Card className="gap-0 py-0">
            <ul className="divide-y">
                {updates.guests.map((guest) => (
                    <ContainerRow key={guest.vmid} guest={guest} updates={updates} />
                ))}
            </ul>
        </Card>
    )
}

function ContainerRow({ guest, updates }: { guest: GuestUpdates; updates: Updates }) {
    const [open, setOpen] = useState(false)
    const start = useStartUpdate()
    const save = useSaveUpdateSettings()
    const count = guest.packages.length + guest.images.length
    const running = guest.status === 'running'

    function setAutoUpdate(enabled: boolean) {
        const excluded = settingsFrom(updates).excluded.filter((vmid) => vmid !== guest.vmid)
        save.mutate(settingsFrom(updates, { excluded: enabled ? excluded : [...excluded, guest.vmid] }))
    }

    return (
        <li className="flex flex-col gap-3 px-4 py-3">
            <div className="flex items-center gap-4">
                <button
                    type="button"
                    className="flex min-w-0 flex-1 items-center gap-2 text-left disabled:cursor-default"
                    onClick={() => setOpen(!open)}
                    disabled={count === 0}
                    aria-expanded={open}
                >
                    <ChevronRightIcon
                        className={cn(
                            'size-4 shrink-0 text-muted-foreground transition-transform',
                            open && 'rotate-90',
                            count === 0 && 'invisible',
                        )}
                    />
                    <span className="truncate font-medium">{guest.name}</span>
                    <Badge variant="outline" className="font-mono">
                        LXC {guest.vmid}
                    </Badge>
                    <GuestState guest={guest} count={count} />
                </button>

                <AutoUpdateSwitch guest={guest} disabled={save.isPending} onChange={setAutoUpdate} />

                <Button
                    variant="outline"
                    size="sm"
                    disabled={Boolean(updates.busy) || !running}
                    onClick={() => start.mutate(() => api.updateGuest(guest.vmid))}
                >
                    Update
                </Button>
            </div>
            {open && <PackageList target={guest} />}
        </li>
    )
}

function GuestState({ guest, count }: { guest: GuestUpdates; count: number }) {
    if (guest.status !== 'running') {
        return <span className="text-sm text-muted-foreground">stopped</span>
    }
    if (guest.error) {
        return <span className="truncate text-sm text-destructive">{guest.error}</span>
    }
    if (!guest.checkedAt) {
        return <span className="text-sm text-muted-foreground">not checked yet</span>
    }
    if (count > 0) {
        return <Badge>{count} updates</Badge>
    }

    return <span className="text-sm text-muted-foreground">up to date · {formatRelative(guest.checkedAt)}</span>
}

type AutoUpdateSwitchProps = {
    guest: GuestUpdates
    disabled: boolean
    onChange: (enabled: boolean) => void
}

function AutoUpdateSwitch({ guest, disabled, onChange }: AutoUpdateSwitchProps) {
    const control = (
        <label className="flex items-center gap-2 text-sm text-muted-foreground">
            <span className="hidden sm:inline">Auto</span>
            <Switch
                checked={guest.autoUpdate}
                disabled={disabled || guest.self}
                onCheckedChange={onChange}
                aria-label={`Update ${guest.name} automatically`}
            />
        </label>
    )

    if (!guest.self) {
        return control
    }

    return (
        <Tooltip>
            <TooltipTrigger render={<span />}>{control}</TooltipTrigger>
            <TooltipContent>This is the manager itself. Update it by hand.</TooltipContent>
        </Tooltip>
    )
}
