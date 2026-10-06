import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2Icon, PlusIcon, XIcon } from 'lucide-react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { api, type MinecraftPlayers } from '@/lib/api'
import { appsQuery } from '@/lib/queries'
import { names } from './minecraft-names'

type PlayersDialogProps = { open: boolean; onOpenChange: (open: boolean) => void }

export function PlayersDialog({ open, onOpenChange }: PlayersDialogProps) {
    const { data, error, isFetching } = useQuery({
        queryKey: ['apps', 'minecraft', 'players'],
        queryFn: api.minecraftPlayers,
        enabled: open,
        staleTime: 0,
    })

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="sm:max-w-md">
                <DialogHeader>
                    <DialogTitle>Players</DialogTitle>
                    <DialogDescription>
                        Only players on this list can join. Operators can also run commands, like /gamemode. The server
                        applies changes at once.
                    </DialogDescription>
                </DialogHeader>
                {error ? (
                    <p className="text-sm text-destructive">{error.message}</p>
                ) : data && !isFetching ? (
                    <PlayersForm current={data} onDone={() => onOpenChange(false)} />
                ) : (
                    <Skeleton className="h-48 w-full" />
                )}
            </DialogContent>
        </Dialog>
    )
}

function PlayersForm({ current, onDone }: { current: MinecraftPlayers; onDone: () => void }) {
    const queryClient = useQueryClient()
    // An operator that is not on the whitelist shows up on the list too.
    const [players, setPlayers] = useState(() =>
        [...new Set([...current.whitelist, ...current.operators])].map((name) => ({
            name,
            operator: current.operators.includes(name),
        })),
    )
    const [newNames, setNewNames] = useState('')

    const save = useMutation({
        mutationFn: () =>
            api.changeMinecraftPlayers({
                whitelist: players.map((player) => player.name),
                operators: players.filter((player) => player.operator).map((player) => player.name),
            }),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: appsQuery.queryKey })
            onDone()
        },
        onError: (error) => toast.error('Could not change the players', { description: error.message }),
    })

    function add(event: FormEvent) {
        event.preventDefault()
        const known = new Set(players.map((player) => player.name.toLowerCase()))
        const added = names(newNames).filter((name) => !known.has(name.toLowerCase()))
        setPlayers((list) => [...list, ...added.map((name) => ({ name, operator: false }))])
        setNewNames('')
    }

    function update(name: string, operator: boolean) {
        setPlayers((list) => list.map((player) => (player.name === name ? { ...player, operator } : player)))
    }

    function remove(name: string) {
        setPlayers((list) => list.filter((player) => player.name !== name))
    }

    return (
        <div className="flex flex-col gap-4">
            <form onSubmit={add} className="flex gap-2">
                <Input
                    aria-label="Minecraft name"
                    autoComplete="off"
                    placeholder="Minecraft name"
                    value={newNames}
                    onChange={(event) => setNewNames(event.target.value)}
                />
                <Button type="submit" variant="outline" disabled={!newNames.trim()}>
                    <PlusIcon />
                    Add
                </Button>
            </form>

            {players.length === 0 ? (
                <p className="rounded-lg border border-dashed p-4 text-center text-sm text-muted-foreground">
                    Nobody can join. Add yourself and your friends.
                </p>
            ) : (
                <ul className="flex max-h-72 flex-col divide-y overflow-y-auto rounded-lg border">
                    {players.map((player) => (
                        <li key={player.name} className="flex items-center gap-3 px-3 py-2 text-sm">
                            <span className="min-w-0 flex-1 truncate font-mono">{player.name}</span>
                            <label className="flex items-center gap-2 text-muted-foreground">
                                Operator
                                <Switch
                                    checked={player.operator}
                                    onCheckedChange={(checked) => update(player.name, checked)}
                                    aria-label={`${player.name} is an operator`}
                                />
                            </label>
                            <Button
                                variant="ghost"
                                size="icon-sm"
                                aria-label={`Remove ${player.name}`}
                                onClick={() => remove(player.name)}
                            >
                                <XIcon />
                            </Button>
                        </li>
                    ))}
                </ul>
            )}

            <DialogFooter>
                <Button disabled={save.isPending} onClick={() => save.mutate()}>
                    {save.isPending && <Loader2Icon className="animate-spin" />}
                    Save
                </Button>
            </DialogFooter>
        </div>
    )
}
