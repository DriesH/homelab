import { useState, type FormEvent } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { EllipsisIcon, PencilIcon, PlusIcon, Trash2Icon } from 'lucide-react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { api, type CheckInput, type CheckKind, type ServiceCheck } from '@/lib/api'
import { formatRelative } from '@/lib/format'
import { healthQuery } from '@/lib/queries'
import { cn } from '@/lib/utils'

const kinds: Record<CheckKind, string> = { http: 'Web address (HTTP)', tcp: 'TCP port' }

const placeholders: Record<CheckKind, string> = { http: 'http://192.168.1.20:8096', tcp: '192.168.1.20:22' }

export function ServiceChecks({ services }: { services: ServiceCheck[] }) {
    const [editing, setEditing] = useState<ServiceCheck | 'new' | null>(null)

    return (
        <>
            {services.length === 0 ? (
                <Card className="items-center gap-3 p-6 text-center text-sm text-muted-foreground">
                    Add the apps you want to watch, like Jellyfin or Radarr. You get an alert when one stops answering.
                    <Button variant="outline" size="sm" onClick={() => setEditing('new')}>
                        <PlusIcon />
                        Add service
                    </Button>
                </Card>
            ) : (
                <Card className="gap-0 py-0">
                    <ul className="divide-y">
                        {services.map((service) => (
                            <ServiceRow key={service.id} service={service} onEdit={() => setEditing(service)} />
                        ))}
                    </ul>
                    <div className="border-t px-4 py-3">
                        <Button variant="ghost" size="sm" onClick={() => setEditing('new')}>
                            <PlusIcon />
                            Add service
                        </Button>
                    </div>
                </Card>
            )}

            <Dialog open={editing !== null} onOpenChange={(open) => !open && setEditing(null)}>
                <DialogContent>
                    {editing !== null && (
                        <CheckForm
                            key={editing === 'new' ? 'new' : editing.id}
                            check={editing === 'new' ? null : editing}
                            onDone={() => setEditing(null)}
                        />
                    )}
                </DialogContent>
            </Dialog>
        </>
    )
}

function ServiceRow({ service, onEdit }: { service: ServiceCheck; onEdit: () => void }) {
    const queryClient = useQueryClient()
    const remove = useMutation({
        mutationFn: () => api.deleteCheck(service.id),
        onSuccess() {
            toast.success(`Removed ${service.name}`)
            queryClient.invalidateQueries({ queryKey: healthQuery.queryKey })
        },
        onError: (error) => toast.error(error.message),
    })

    return (
        // On phones the state moves under the name.
        <li className="grid grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-x-3 gap-y-1 px-4 py-3 sm:grid-cols-[auto_minmax(0,1fr)_auto_auto]">
            <StatusDot status={service.status} />
            <div className="flex min-w-0 flex-col">
                <span className="truncate font-medium">{service.name}</span>
                <span className="truncate font-mono text-xs text-muted-foreground">{service.target}</span>
            </div>
            <div className="col-start-2 row-start-2 min-w-0 sm:col-start-3 sm:row-start-1">
                <ServiceState service={service} />
            </div>
            <DropdownMenu>
                <DropdownMenuTrigger
                    render={
                        <Button variant="ghost" size="icon-sm" className="col-start-3 row-start-1 sm:col-start-4" />
                    }
                    aria-label={`Actions for ${service.name}`}
                >
                    <EllipsisIcon />
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                    <DropdownMenuItem onClick={onEdit}>
                        <PencilIcon /> Edit
                    </DropdownMenuItem>
                    <DropdownMenuItem variant="destructive" disabled={remove.isPending} onClick={() => remove.mutate()}>
                        <Trash2Icon /> Remove
                    </DropdownMenuItem>
                </DropdownMenuContent>
            </DropdownMenu>
        </li>
    )
}

function StatusDot({ status }: { status: ServiceCheck['status'] }) {
    return (
        <span
            className={cn(
                'size-2.5 shrink-0 rounded-full',
                status === 'up' && 'bg-emerald-500',
                status === 'down' && 'bg-destructive',
                status === 'pending' && 'bg-muted-foreground/40',
            )}
            aria-label={status}
        />
    )
}

function ServiceState({ service }: { service: ServiceCheck }) {
    if (service.status === 'pending') {
        return <span className="text-sm text-muted-foreground">checking…</span>
    }
    if (service.status === 'down') {
        return (
            <span className="block truncate text-sm text-destructive sm:max-w-60">
                down{service.since ? ` ${formatRelative(service.since)}` : ''} · {service.error}
            </span>
        )
    }

    return <span className="text-sm text-muted-foreground tabular-nums">{service.latencyMs} ms</span>
}

function CheckForm({ check, onDone }: { check: ServiceCheck | null; onDone: () => void }) {
    const queryClient = useQueryClient()
    const [name, setName] = useState(check?.name ?? '')
    const [kind, setKind] = useState<CheckKind>(check?.kind ?? 'http')
    const [target, setTarget] = useState(check?.target ?? '')

    const save = useMutation({
        async mutationFn(input: CheckInput) {
            if (check) {
                await api.updateCheck(check.id, input)
            } else {
                await api.addCheck(input)
            }
        },
        onSuccess() {
            toast.success(check ? 'Service saved' : `Watching ${name}`)
            queryClient.invalidateQueries({ queryKey: healthQuery.queryKey })
            onDone()
        },
        onError: (error) => toast.error(error.message),
    })

    function handleSubmit(event: FormEvent) {
        event.preventDefault()
        save.mutate({ name, kind, target })
    }

    return (
        <form onSubmit={handleSubmit} className="flex flex-col gap-4">
            <DialogHeader>
                <DialogTitle>{check ? `Edit ${check.name}` : 'Add service'}</DialogTitle>
            </DialogHeader>

            <div className="flex flex-col gap-2">
                <Label htmlFor="check-name">Name</Label>
                <Input
                    id="check-name"
                    required
                    maxLength={64}
                    placeholder="Jellyfin"
                    value={name}
                    onChange={(event) => setName(event.target.value)}
                />
            </div>

            <div className="flex flex-col gap-2">
                <Label>Check</Label>
                <Select items={kinds} value={kind} onValueChange={(value) => value && setKind(value)}>
                    <SelectTrigger className="w-full">
                        <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                        {Object.entries(kinds).map(([value, label]) => (
                            <SelectItem key={value} value={value}>
                                {label}
                            </SelectItem>
                        ))}
                    </SelectContent>
                </Select>
            </div>

            <div className="flex flex-col gap-2">
                <Label htmlFor="check-target">{kind === 'http' ? 'URL' : 'Address and port'}</Label>
                <Input
                    id="check-target"
                    required
                    placeholder={placeholders[kind]}
                    value={target}
                    onChange={(event) => setTarget(event.target.value)}
                    className="font-mono"
                />
                <p className="text-xs text-muted-foreground">
                    {kind === 'http'
                        ? 'Up means the app answers without a server error. A login page counts as up.'
                        : 'Up means the port accepts a connection.'}
                </p>
            </div>

            <DialogFooter>
                <Button type="button" variant="outline" onClick={onDone}>
                    Cancel
                </Button>
                <Button type="submit" disabled={save.isPending}>
                    {save.isPending ? 'Saving…' : 'Save'}
                </Button>
            </DialogFooter>
        </form>
    )
}
