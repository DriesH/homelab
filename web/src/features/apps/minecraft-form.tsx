import { useState, type FormEvent } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Loader2Icon } from 'lucide-react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { DialogFooter } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { api, type Apps, type MinecraftAnswers, type SavedAnswers } from '@/lib/api'
import { appsQuery } from '@/lib/queries'
import { Field, Section } from './form-parts'
import { keepSaved } from './keep-saved'
import { names } from './minecraft-names'

const memories = { '2': '2 GB · 2 to 4 players', '4': '4 GB · up to 10 players', '6': '6 GB', '8': '8 GB' }
const difficulties = { peaceful: 'Peaceful', easy: 'Easy', normal: 'Normal', hard: 'Hard' }
const modes = { survival: 'Survival', creative: 'Creative', adventure: 'Adventure' }

type MinecraftFormProps = {
    defaults: Apps['defaults']
    saved: SavedAnswers | null
    onDone: () => void
}

export function MinecraftForm({ defaults, saved, onDone }: MinecraftFormProps) {
    const queryClient = useQueryClient()
    const [answers, setAnswers] = useState<MinecraftAnswers>(
        () => saved?.minecraft ?? { ...defaults.minecraft, storage: defaults.storage },
    )
    const [friends, setFriends] = useState(() => answers.whitelist.join('\n'))

    const install = useMutation({
        mutationFn: () => api.installMinecraft({ ...answers, whitelist: names(friends) }, saved !== null),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: appsQuery.queryKey })
            toast.success('The install started')
            onDone()
        },
        onError: (error) => toast.error(error.message),
    })

    function set<K extends keyof MinecraftAnswers>(key: K, value: MinecraftAnswers[K]) {
        setAnswers((current) => ({ ...current, [key]: value }))
    }

    function handleSubmit(event: FormEvent) {
        event.preventDefault()
        install.mutate()
    }

    const storages = Object.fromEntries(defaults.storages.map((name) => [name, name]))
    const keySaved = Boolean(saved?.hasPlayitSecretKey)

    return (
        <form className="flex flex-col gap-6" onSubmit={handleSubmit}>
            <Section title="Players" help="Only players on the whitelist can join. Use Java profile names.">
                <Field
                    id="mc-operator"
                    label="Your Minecraft name"
                    help="You become the operator, so you can run commands."
                >
                    <Input
                        id="mc-operator"
                        autoComplete="off"
                        value={answers.operator}
                        onChange={(event) => set('operator', event.target.value.trim())}
                        required
                    />
                </Field>
                <Field
                    id="mc-friends"
                    label="Friends"
                    help="One name per line. You can change the list later with Players."
                >
                    <Textarea
                        id="mc-friends"
                        rows={3}
                        value={friends}
                        onChange={(event) => setFriends(event.target.value)}
                    />
                </Field>
            </Section>

            <Section title="World">
                <Field id="mc-motd" label="Message of the day" help="Players see it in their server list.">
                    <Input
                        id="mc-motd"
                        maxLength={59}
                        value={answers.motd}
                        onChange={(event) => set('motd', event.target.value)}
                        required
                    />
                </Field>
                <div className="grid gap-4 sm:grid-cols-2">
                    <Field id="mc-difficulty" label="Difficulty">
                        <Select
                            items={difficulties}
                            value={answers.difficulty}
                            onValueChange={(value) =>
                                value && set('difficulty', value as MinecraftAnswers['difficulty'])
                            }
                        >
                            <SelectTrigger id="mc-difficulty" className="w-full">
                                <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                                {Object.entries(difficulties).map(([value, label]) => (
                                    <SelectItem key={value} value={value}>
                                        {label}
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                    </Field>
                    <Field id="mc-mode" label="Game mode">
                        <Select
                            items={modes}
                            value={answers.mode}
                            onValueChange={(value) => value && set('mode', value as MinecraftAnswers['mode'])}
                        >
                            <SelectTrigger id="mc-mode" className="w-full">
                                <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                                {Object.entries(modes).map(([value, label]) => (
                                    <SelectItem key={value} value={value}>
                                        {label}
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                    </Field>
                    <Field id="mc-players" label="Most players at once">
                        <Input
                            id="mc-players"
                            type="number"
                            min={1}
                            max={100}
                            value={answers.maxPlayers}
                            onChange={(event) => set('maxPlayers', Number(event.target.value))}
                            required
                        />
                    </Field>
                    <Field id="mc-view" label="View distance" help="In chunks. Lower uses less memory.">
                        <Input
                            id="mc-view"
                            type="number"
                            min={3}
                            max={32}
                            value={answers.viewDistance}
                            onChange={(event) => set('viewDistance', Number(event.target.value))}
                            required
                        />
                    </Field>
                </div>
                <Field id="mc-seed" label="Seed (optional)" help="Leave it empty for a random world.">
                    <Input
                        id="mc-seed"
                        value={answers.seed}
                        onChange={(event) => set('seed', event.target.value.trim())}
                    />
                </Field>
            </Section>

            <Section
                title="Friends outside your network"
                help="With playit.gg, friends join without open ports on your router. They install nothing."
            >
                <ol className="list-decimal space-y-1 pl-5 text-xs text-muted-foreground">
                    <li>
                        On{' '}
                        <a
                            className="underline"
                            href="https://playit.gg/account/agents"
                            target="_blank"
                            rel="noreferrer"
                        >
                            playit.gg
                        </a>
                        , add an agent of the type Docker, and copy its secret key.
                    </li>
                    <li>Add a tunnel of the type Minecraft Java, to the local address localhost:25565.</li>
                    <li>Copy the address of the tunnel, like name.joinmc.link. Your friends use it.</li>
                </ol>
                <div className="grid gap-4 sm:grid-cols-2">
                    <Field id="mc-playit" label="Secret key (optional)">
                        <Input
                            id="mc-playit"
                            type="password"
                            autoComplete="off"
                            value={answers.playitSecretKey}
                            onChange={(event) => set('playitSecretKey', event.target.value.trim())}
                            placeholder={keySaved ? keepSaved : undefined}
                        />
                    </Field>
                    <Field id="mc-address" label="Address for friends (optional)" help="Homelab shows it on this page.">
                        <Input
                            id="mc-address"
                            placeholder="name.joinmc.link"
                            value={answers.publicAddress}
                            onChange={(event) => set('publicAddress', event.target.value.trim())}
                        />
                    </Field>
                </div>
            </Section>

            <Section title="Container">
                <div className="grid gap-4 sm:grid-cols-2">
                    <Field id="mc-memory" label="Memory for the server" help="The container gets a bit more.">
                        <Select
                            items={memories}
                            value={String(answers.memory)}
                            onValueChange={(value) => value && set('memory', Number(value))}
                        >
                            <SelectTrigger id="mc-memory" className="w-full">
                                <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                                {Object.entries(memories).map(([value, label]) => (
                                    <SelectItem key={value} value={value}>
                                        {label}
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                    </Field>
                    <Field id="mc-world" label="World disk (GB)" help="Proxmox backs it up with the container.">
                        <Input
                            id="mc-world"
                            type="number"
                            min={5}
                            max={2000}
                            value={answers.worldSize}
                            onChange={(event) => set('worldSize', Number(event.target.value))}
                            required
                        />
                    </Field>
                </div>
                <Field id="mc-storage" label="Storage">
                    <Select
                        items={storages}
                        value={answers.storage}
                        onValueChange={(value) => value && set('storage', value)}
                    >
                        <SelectTrigger id="mc-storage" className="w-full">
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                            {defaults.storages.map((name) => (
                                <SelectItem key={name} value={name}>
                                    {name}
                                </SelectItem>
                            ))}
                        </SelectContent>
                    </Select>
                </Field>
            </Section>

            <label className="flex items-start gap-3 text-sm">
                <Checkbox
                    className="mt-0.5"
                    checked={answers.acceptEula}
                    onCheckedChange={(checked) => set('acceptEula', checked)}
                />
                <span>
                    I agree to the{' '}
                    <a className="underline" href="https://aka.ms/MinecraftEULA" target="_blank" rel="noreferrer">
                        Minecraft EULA
                    </a>
                    . The server can't run without it.
                </span>
            </label>

            <DialogFooter>
                <Button type="button" variant="outline" onClick={onDone}>
                    Cancel
                </Button>
                <Button type="submit" disabled={install.isPending || !answers.acceptEula}>
                    {install.isPending && <Loader2Icon className="animate-spin" />}
                    Install
                </Button>
            </DialogFooter>
        </form>
    )
}
