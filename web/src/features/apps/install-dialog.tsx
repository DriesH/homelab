import { useState, type FormEvent } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Loader2Icon } from 'lucide-react'
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
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { api, type Apps, type CatalogApp, type MediaStackAnswers, type SavedAnswers } from '@/lib/api'
import { appsQuery } from '@/lib/queries'
import { Field, Section } from './form-parts'
import { JellyfinForm } from './jellyfin-form'
import { MinecraftForm } from './minecraft-form'
import { keepSaved } from './keep-saved'
import { MediaSourceFields } from './media-source'

type InstallDialogProps = {
    app: CatalogApp
    defaults: Apps['defaults']
    // saved are the answers of the last failed install. Their secrets stay on the host.
    saved: SavedAnswers | null
    open: boolean
    onOpenChange: (open: boolean) => void
}

const descriptions: Record<string, string> = {
    jellyfin: 'Homelab makes a new container for Jellyfin on this host, with read access to your movies and series.',
    media: 'Homelab makes a new container for it on this host. Nothing else changes, except that Jellyfin gets read access to the media folder.',
    minecraft: 'Homelab makes a new container on this host with a Paper server: the Minecraft Java server, but faster.',
}

export function InstallDialog({ app, defaults, saved, open, onOpenChange }: InstallDialogProps) {
    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="max-h-[90svh] overflow-y-auto sm:max-w-xl">
                <DialogHeader>
                    <DialogTitle>Install {app.id === 'media' ? `the ${app.name.toLowerCase()}` : app.name}</DialogTitle>
                    <DialogDescription>{descriptions[app.id]}</DialogDescription>
                </DialogHeader>
                {open &&
                    (app.id === 'jellyfin' ? (
                        <JellyfinForm defaults={defaults} saved={saved} onDone={() => onOpenChange(false)} />
                    ) : app.id === 'minecraft' ? (
                        <MinecraftForm defaults={defaults} saved={saved} onDone={() => onOpenChange(false)} />
                    ) : (
                        <MediaStackForm
                            app={app}
                            defaults={defaults}
                            saved={saved}
                            onDone={() => onOpenChange(false)}
                        />
                    ))}
            </DialogContent>
        </Dialog>
    )
}

function MediaStackForm({
    app,
    defaults,
    saved,
    onDone,
}: {
    app: CatalogApp
    defaults: Apps['defaults']
    saved: SavedAnswers | null
    onDone: () => void
}) {
    const queryClient = useQueryClient()
    const [answers, setAnswers] = useState<MediaStackAnswers>(
        () =>
            saved?.answers ?? {
                nasServer: '',
                nasExport: '',
                mediaFolder: '',
                moviesFolder: defaults.moviesFolder,
                seriesFolder: defaults.seriesFolder,
                wireguardPrivateKey: '',
                vpnCountries: defaults.vpnCountries,
                subtitleLanguages: defaults.subtitleLanguages,
                username: defaults.username,
                password: '',
                jellyfinApiKey: '',
                jellyfinAdminUsername: '',
                jellyfinAdminPassword: '',
                openSubtitlesUsername: '',
                openSubtitlesPassword: '',
                restartJellyfin: true,
                storage: defaults.storage,
                downloadsSize: defaults.downloadsSize,
            },
    )

    const install = useMutation({
        mutationFn: () =>
            api.installApp(
                app.id,
                // Media that is already mounted is used as it is.
                defaults.mediaShare ? { ...answers, nasServer: '', nasExport: '', mediaFolder: '' } : answers,
                saved !== null,
            ),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: appsQuery.queryKey })
            toast.success('The install started')
            onDone()
        },
        onError: (error) => toast.error(error.message),
    })

    function set<K extends keyof MediaStackAnswers>(key: K, value: MediaStackAnswers[K]) {
        setAnswers((current) => ({ ...current, [key]: value }))
    }

    function handleSubmit(event: FormEvent) {
        event.preventDefault()
        install.mutate()
    }

    const storages = Object.fromEntries(defaults.storages.map((name) => [name, name]))
    const shortPassword = answers.password.length > 0 && answers.password.length < 12

    return (
        <form className="flex flex-col gap-6" onSubmit={handleSubmit}>
            <MediaSourceFields
                id="media"
                defaults={defaults}
                value={answers}
                onChange={(value) => setAnswers((current) => ({ ...current, ...value }))}
                help="Where your movies and series are. Each gets its own folder in it, and its own library in Jellyfin."
            >
                {(path) => (
                    <div className="grid gap-4 sm:grid-cols-2">
                        <Field id="movies-folder" label="Movies folder" help={`${path}/${answers.moviesFolder}`}>
                            <Input
                                id="movies-folder"
                                value={answers.moviesFolder}
                                onChange={(event) => set('moviesFolder', event.target.value)}
                                required
                            />
                        </Field>
                        <Field id="series-folder" label="Series folder" help={`${path}/${answers.seriesFolder}`}>
                            <Input
                                id="series-folder"
                                value={answers.seriesFolder}
                                onChange={(event) => set('seriesFolder', event.target.value)}
                                required
                            />
                        </Field>
                    </div>
                )}
            </MediaSourceFields>

            <Section
                title="ProtonVPN"
                help="qBittorrent, Prowlarr and FlareSolverr only use the internet through the VPN."
            >
                <Field
                    id="wireguard-key"
                    label="WireGuard private key"
                    help={
                        <>
                            Make one at{' '}
                            <a
                                className="underline"
                                href="https://account.proton.me/u/0/vpn/WireGuard"
                                target="_blank"
                                rel="noreferrer"
                            >
                                account.proton.me
                            </a>
                            . Turn on NAT-PMP (port forwarding).
                        </>
                    }
                >
                    <Input
                        id="wireguard-key"
                        type="password"
                        autoComplete="off"
                        value={answers.wireguardPrivateKey}
                        onChange={(event) => set('wireguardPrivateKey', event.target.value.trim())}
                        placeholder={saved ? keepSaved : undefined}
                        required={!saved}
                    />
                </Field>
                <Field id="vpn-countries" label="Server countries" help="Separate them with commas.">
                    <Input
                        id="vpn-countries"
                        value={answers.vpnCountries}
                        onChange={(event) => set('vpnCountries', event.target.value)}
                        required
                    />
                </Field>
            </Section>

            <Section title="Login" help="One login for Radarr, Sonarr, Prowlarr, Bazarr and qBittorrent.">
                <Field id="arr-username" label="Username">
                    <Input
                        id="arr-username"
                        autoComplete="off"
                        value={answers.username}
                        onChange={(event) => set('username', event.target.value.trim())}
                        required
                    />
                </Field>
                <Field
                    id="arr-password"
                    label="Password"
                    help={shortPassword ? undefined : 'At least 12 characters.'}
                    error={shortPassword ? 'The password needs at least 12 characters.' : undefined}
                >
                    <Input
                        id="arr-password"
                        type="password"
                        autoComplete="new-password"
                        value={answers.password}
                        onChange={(event) => set('password', event.target.value)}
                        aria-invalid={shortPassword}
                        placeholder={saved ? keepSaved : undefined}
                        required={!saved}
                    />
                </Field>
            </Section>

            {defaults.jellyfinVmid ? (
                <Section
                    title="Jellyfin"
                    help={`Found in container ${defaults.jellyfinVmid}. It gets read access to the media folder.`}
                >
                    <Field
                        id="jellyfin-key"
                        label="API key (optional)"
                        help="With a key, the install adds the Movies and Series libraries. Make one in Jellyfin: Dashboard > API Keys."
                    >
                        <Input
                            id="jellyfin-key"
                            type="password"
                            autoComplete="off"
                            value={answers.jellyfinApiKey}
                            onChange={(event) => set('jellyfinApiKey', event.target.value.trim())}
                            placeholder={saved?.hasJellyfinApiKey ? keepSaved : undefined}
                        />
                    </Field>
                    <div className="grid gap-4 sm:grid-cols-2">
                        <Field
                            id="jellyfin-admin"
                            label="Admin username (optional)"
                            help="With a Jellyfin admin, Homelab also sets up Seerr. You log in to Seerr with this account."
                        >
                            <Input
                                id="jellyfin-admin"
                                autoComplete="off"
                                value={answers.jellyfinAdminUsername}
                                onChange={(event) => set('jellyfinAdminUsername', event.target.value.trim())}
                            />
                        </Field>
                        <Field id="jellyfin-admin-password" label="Admin password">
                            <Input
                                id="jellyfin-admin-password"
                                type="password"
                                autoComplete="off"
                                value={answers.jellyfinAdminPassword}
                                onChange={(event) => set('jellyfinAdminPassword', event.target.value)}
                                placeholder={
                                    saved?.hasJellyfinAdminPassword &&
                                    answers.jellyfinAdminUsername === saved.answers.jellyfinAdminUsername
                                        ? keepSaved
                                        : undefined
                                }
                                disabled={!answers.jellyfinAdminUsername}
                            />
                        </Field>
                    </div>
                    <label className="flex items-center justify-between gap-4 text-sm">
                        <span>
                            Restart Jellyfin
                            <span className="block text-xs text-muted-foreground">
                                Jellyfin only sees the media folder after a restart.
                            </span>
                        </span>
                        <Switch
                            checked={answers.restartJellyfin}
                            onCheckedChange={(checked) => set('restartJellyfin', checked)}
                        />
                    </label>
                </Section>
            ) : null}

            <Section title="Container">
                <div className="grid gap-4 sm:grid-cols-2">
                    <Field id="storage" label="Storage">
                        <Select
                            items={storages}
                            value={answers.storage}
                            onValueChange={(value) => value && set('storage', value)}
                        >
                            <SelectTrigger id="storage" className="w-full">
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
                    <Field id="downloads-size" label="Downloads disk (GB)">
                        <Input
                            id="downloads-size"
                            type="number"
                            min={10}
                            max={10000}
                            value={answers.downloadsSize}
                            onChange={(event) => set('downloadsSize', Number(event.target.value))}
                            required
                        />
                    </Field>
                </div>
            </Section>

            <Section title="Subtitles" help="Bazarr adds subtitles to every new movie and episode.">
                <Field id="subtitles" label="Languages" help="2-letter codes, like en,nl.">
                    <Input
                        id="subtitles"
                        value={answers.subtitleLanguages}
                        onChange={(event) => set('subtitleLanguages', event.target.value.trim())}
                        required
                    />
                </Field>
                <div className="grid gap-4 sm:grid-cols-2">
                    <Field
                        id="opensubtitles-username"
                        label="OpenSubtitles.com username (optional)"
                        help={
                            <>
                                Finds many more subtitles. Make a free account at{' '}
                                <a
                                    className="underline"
                                    href="https://www.opensubtitles.com"
                                    target="_blank"
                                    rel="noreferrer"
                                >
                                    opensubtitles.com
                                </a>
                                , not .org.
                            </>
                        }
                    >
                        <Input
                            id="opensubtitles-username"
                            autoComplete="off"
                            value={answers.openSubtitlesUsername}
                            onChange={(event) => set('openSubtitlesUsername', event.target.value.trim())}
                        />
                    </Field>
                    <Field id="opensubtitles-password" label="OpenSubtitles.com password">
                        <Input
                            id="opensubtitles-password"
                            type="password"
                            autoComplete="off"
                            value={answers.openSubtitlesPassword}
                            onChange={(event) => set('openSubtitlesPassword', event.target.value)}
                            placeholder={
                                saved?.hasOpenSubtitlesPassword &&
                                answers.openSubtitlesUsername === saved.answers.openSubtitlesUsername
                                    ? keepSaved
                                    : undefined
                            }
                            disabled={!answers.openSubtitlesUsername}
                        />
                    </Field>
                </div>
            </Section>

            <DialogFooter>
                <Button type="button" variant="outline" onClick={onDone}>
                    Cancel
                </Button>
                <Button type="submit" disabled={install.isPending || shortPassword || (!saved && !answers.password)}>
                    {install.isPending && <Loader2Icon className="animate-spin" />}
                    Install
                </Button>
            </DialogFooter>
        </form>
    )
}
