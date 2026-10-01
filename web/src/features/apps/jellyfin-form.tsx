import { useState, type FormEvent } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Loader2Icon } from 'lucide-react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { DialogFooter } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { api, type Apps, type JellyfinAnswers, type SavedAnswers } from '@/lib/api'
import { appsQuery } from '@/lib/queries'
import { Field, Section } from './form-parts'
import { keepSaved } from './keep-saved'

type JellyfinFormProps = {
    defaults: Apps['defaults']
    saved: SavedAnswers | null
    onDone: () => void
}

export function JellyfinForm({ defaults, saved, onDone }: JellyfinFormProps) {
    const queryClient = useQueryClient()
    // The share of the media stack is used as it is.
    const shareMounted = Boolean(defaults.mediaShare)
    const [answers, setAnswers] = useState<JellyfinAnswers>(
        () =>
            saved?.jellyfin ?? {
                nasServer: '',
                nasExport: '',
                moviesFolder: defaults.moviesFolder,
                seriesFolder: defaults.seriesFolder,
                adminUsername: 'admin',
                adminPassword: '',
                theme: true,
                storage: defaults.storage,
            },
    )

    const install = useMutation({
        mutationFn: () =>
            api.installJellyfin(shareMounted ? { ...answers, nasServer: '', nasExport: '' } : answers, saved !== null),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: appsQuery.queryKey })
            toast.success('The install started')
            onDone()
        },
        onError: (error) => toast.error(error.message),
    })

    function set<K extends keyof JellyfinAnswers>(key: K, value: JellyfinAnswers[K]) {
        setAnswers((current) => ({ ...current, [key]: value }))
    }

    function handleSubmit(event: FormEvent) {
        event.preventDefault()
        install.mutate()
    }

    const storages = Object.fromEntries(defaults.storages.map((name) => [name, name]))
    const passwordSaved = saved?.hasJellyfinAdminPassword && answers.adminUsername === saved.jellyfin.adminUsername
    const share = shareMounted ? defaults.mediaShare : `${answers.nasServer || 'NAS'}:${answers.nasExport || '/…'}`

    return (
        <form className="flex flex-col gap-6" onSubmit={handleSubmit}>
            <Section
                title="NAS"
                help="Jellyfin reads your movies and series from the NFS share, and never writes to it."
            >
                {shareMounted ? (
                    <p className="text-sm">
                        Uses the share that is already mounted: <span className="font-mono">{defaults.mediaShare}</span>
                    </p>
                ) : (
                    <>
                        <Field id="jf-nas-server" label="Address">
                            <Input
                                id="jf-nas-server"
                                placeholder="192.168.1.5"
                                value={answers.nasServer}
                                onChange={(event) => set('nasServer', event.target.value.trim())}
                                required
                            />
                        </Field>
                        <Field id="jf-nas-export" label="NFS export path" help="UGOS shows it on the NFS page.">
                            <Input
                                id="jf-nas-export"
                                placeholder="/volume1/media"
                                value={answers.nasExport}
                                onChange={(event) => set('nasExport', event.target.value.trim())}
                                required
                            />
                        </Field>
                    </>
                )}
                <div className="grid gap-4 sm:grid-cols-2">
                    <Field id="jf-movies" label="Movies folder" help={`${share}/${answers.moviesFolder}`}>
                        <Input
                            id="jf-movies"
                            value={answers.moviesFolder}
                            onChange={(event) => set('moviesFolder', event.target.value)}
                            required
                        />
                    </Field>
                    <Field id="jf-series" label="Series folder" help={`${share}/${answers.seriesFolder}`}>
                        <Input
                            id="jf-series"
                            value={answers.seriesFolder}
                            onChange={(event) => set('seriesFolder', event.target.value)}
                            required
                        />
                    </Field>
                </div>
            </Section>

            <Section title="Admin account" help="Homelab finishes the setup wizard of Jellyfin with this account.">
                <div className="grid gap-4 sm:grid-cols-2">
                    <Field id="jf-admin" label="Username">
                        <Input
                            id="jf-admin"
                            autoComplete="off"
                            value={answers.adminUsername}
                            onChange={(event) => set('adminUsername', event.target.value.trim())}
                            required
                        />
                    </Field>
                    <Field id="jf-password" label="Password">
                        <Input
                            id="jf-password"
                            type="password"
                            autoComplete="new-password"
                            value={answers.adminPassword}
                            onChange={(event) => set('adminPassword', event.target.value)}
                            placeholder={passwordSaved ? keepSaved : undefined}
                            required={!passwordSaved}
                        />
                    </Field>
                </div>
            </Section>

            <Section title="Container">
                <Field id="jf-storage" label="Storage">
                    <Select
                        items={storages}
                        value={answers.storage}
                        onValueChange={(value) => value && set('storage', value)}
                    >
                        <SelectTrigger id="jf-storage" className="w-full">
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
                <label className="flex items-center justify-between gap-4 text-sm">
                    <span>
                        Netflix theme
                        <span className="block text-xs text-muted-foreground">
                            For the web page of Jellyfin. You can change it later on the Jellyfin page of Homelab.
                        </span>
                    </span>
                    <Switch checked={answers.theme} onCheckedChange={(checked) => set('theme', checked)} />
                </label>
                <p className="text-xs text-muted-foreground">
                    When the host has a GPU (Intel or AMD), Jellyfin gets it for hardware transcoding.
                </p>
            </Section>

            <DialogFooter>
                <Button type="button" variant="outline" onClick={onDone}>
                    Cancel
                </Button>
                <Button type="submit" disabled={install.isPending}>
                    {install.isPending && <Loader2Icon className="animate-spin" />}
                    Install
                </Button>
            </DialogFooter>
        </form>
    )
}
