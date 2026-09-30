import { useState, type FormEvent, type ReactNode } from 'react'
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
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { api, type Apps, type CatalogApp, type MediaStackAnswers, type SavedAnswers } from '@/lib/api'
import { appsQuery } from '@/lib/queries'

type InstallDialogProps = {
    app: CatalogApp
    defaults: Apps['defaults']
    // saved are the answers of the last failed install. Their secrets stay on the host.
    saved: SavedAnswers | null
    open: boolean
    onOpenChange: (open: boolean) => void
}

export function InstallDialog({ app, defaults, saved, open, onOpenChange }: InstallDialogProps) {
    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="max-h-[90svh] overflow-y-auto sm:max-w-xl">
                <DialogHeader>
                    <DialogTitle>Install the {app.name.toLowerCase()}</DialogTitle>
                    <DialogDescription>
                        Homelab makes a new container for it on this host. Nothing else changes, except that Jellyfin
                        gets read access to the media folder.
                    </DialogDescription>
                </DialogHeader>
                {open && (
                    <MediaStackForm app={app} defaults={defaults} saved={saved} onDone={() => onOpenChange(false)} />
                )}
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
                restartJellyfin: true,
                storage: defaults.storage,
                downloadsSize: defaults.downloadsSize,
            },
    )

    const install = useMutation({
        mutationFn: () => api.installApp(app.id, answers, saved !== null),
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
            <Section
                title="NAS"
                help="The NFS share with your movies and series. Each gets its own folder in it, and its own library in Jellyfin."
            >
                <Field id="nas-server" label="Address">
                    <Input
                        id="nas-server"
                        placeholder="192.168.1.5"
                        value={answers.nasServer}
                        onChange={(event) => set('nasServer', event.target.value.trim())}
                        required
                    />
                </Field>
                <Field id="nas-export" label="NFS export path" help="UGOS shows it on the NFS page.">
                    <Input
                        id="nas-export"
                        placeholder="/volume1/media"
                        value={answers.nasExport}
                        onChange={(event) => set('nasExport', event.target.value.trim())}
                        required
                    />
                </Field>
                <div className="grid gap-4 sm:grid-cols-2">
                    <Field
                        id="movies-folder"
                        label="Movies folder"
                        help={folderHelp(answers.nasExport, answers.moviesFolder)}
                    >
                        <Input
                            id="movies-folder"
                            value={answers.moviesFolder}
                            onChange={(event) => set('moviesFolder', event.target.value)}
                            required
                        />
                    </Field>
                    <Field
                        id="series-folder"
                        label="Series folder"
                        help={folderHelp(answers.nasExport, answers.seriesFolder)}
                    >
                        <Input
                            id="series-folder"
                            value={answers.seriesFolder}
                            onChange={(event) => set('seriesFolder', event.target.value)}
                            required
                        />
                    </Field>
                </div>
            </Section>

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
                <Field id="subtitles" label="Subtitle languages" help="2-letter codes, like en,nl.">
                    <Input
                        id="subtitles"
                        value={answers.subtitleLanguages}
                        onChange={(event) => set('subtitleLanguages', event.target.value.trim())}
                        required
                    />
                </Field>
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

// folderHelp shows the full path on the NAS, so it's clear where the files go.
function folderHelp(nasExport: string, folder: string) {
    return `${nasExport || '/volume1/media'}/${folder.trim() || '…'}`
}

const keepSaved = 'Saved on the host. Leave empty to keep it.'

function Section({ title, help, children }: { title: string; help?: string; children: ReactNode }) {
    return (
        <fieldset className="flex flex-col gap-4">
            <div>
                <legend className="text-sm font-medium">{title}</legend>
                {help && <p className="text-xs text-muted-foreground">{help}</p>}
            </div>
            {children}
        </fieldset>
    )
}

type FieldProps = { id: string; label: string; help?: ReactNode; error?: string; children: ReactNode }

function Field({ id, label, help, error, children }: FieldProps) {
    return (
        <div className="flex flex-col gap-2">
            <Label htmlFor={id}>{label}</Label>
            {children}
            {error ? (
                <p className="text-xs text-destructive">{error}</p>
            ) : (
                help && <p className="text-xs text-muted-foreground">{help}</p>
            )}
        </div>
    )
}
