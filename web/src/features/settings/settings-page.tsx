import { useRef, useState, type ChangeEvent } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { DownloadIcon, Loader2Icon, UploadIcon } from 'lucide-react'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog'
import { api, type SettingsChange, type SettingsImport } from '@/lib/api'
import { cn } from '@/lib/utils'
import { DataBackupCard } from './data-backup-card'

const sectionTitles: Record<SettingsChange['section'], string> = {
    selfUpdate: 'Homelab updates',
    notifications: 'Telegram',
    updates: 'Update schedule',
    health: 'Health checks',
    backups: 'Backup job',
    tailscale: 'Tailscale',
    jellyfin: 'Jellyfin',
}

const statusBadges: Record<SettingsChange['status'], { label: string; className?: string }> = {
    unchanged: { label: 'No change', className: 'text-muted-foreground' },
    changed: { label: 'Changes', className: 'border-transparent bg-primary text-primary-foreground' },
    applied: { label: 'Saved', className: 'border-transparent bg-emerald-600 text-white' },
    skipped: { label: 'Skipped', className: 'text-amber-600 dark:text-amber-400' },
    failed: { label: 'Failed', className: 'border-transparent bg-destructive/10 text-destructive' },
}

export function SettingsPage() {
    return (
        <div className="flex flex-col gap-6">
            <header>
                <h1 className="text-lg font-semibold">Settings</h1>
                <p className="text-sm text-muted-foreground">Settings that belong to Homelab itself.</p>
            </header>

            <SettingsFileCard />
            <DataBackupCard />
        </div>
    )
}

function SettingsFileCard() {
    const queryClient = useQueryClient()
    const input = useRef<HTMLInputElement>(null)
    const [file, setFile] = useState<{ name: string; yaml: string } | null>(null)
    const [result, setResult] = useState<SettingsImport | null>(null)
    // The file and result stay after closing, so the dialog doesn't change while it fades out.
    const [open, setOpen] = useState(false)

    const preview = useMutation({
        mutationFn: (yaml: string) => api.importSettings(yaml, false),
        onSuccess: (imported) => {
            setResult(imported)
            setOpen(true)
        },
        onError: (error) => toast.error(error.message),
    })

    const apply = useMutation({
        mutationFn: (yaml: string) => api.importSettings(yaml, true),
        onSuccess: (imported) => {
            setResult(imported)
            queryClient.invalidateQueries()
        },
        onError: (error) => toast.error(error.message),
    })

    async function handleFile(event: ChangeEvent<HTMLInputElement>) {
        const selected = event.target.files?.[0]
        event.target.value = ''
        if (!selected) {
            return
        }

        const yaml = await selected.text()
        setFile({ name: selected.name, yaml })
        preview.mutate(yaml)
    }

    function close() {
        setOpen(false)
    }

    const changes = result?.changes ?? []
    const hasChanges = changes.some((change) => change.status === 'changed')

    return (
        <Card>
            <CardHeader>
                <CardTitle>Settings file</CardTitle>
                <CardDescription>
                    Download all settings as homelab.yaml, to keep a copy or to move to a new server. Tokens, passwords
                    and API keys are not in the file.
                </CardDescription>
            </CardHeader>
            <CardContent className="text-sm text-muted-foreground">
                An import only changes the sections that are in the file. Homelab shows the changes before it saves
                them.
            </CardContent>
            <CardFooter className="flex flex-wrap gap-2">
                <Button render={<a href={api.settingsExportUrl} download="homelab.yaml" />}>
                    <DownloadIcon />
                    Download homelab.yaml
                </Button>
                <Button variant="outline" disabled={preview.isPending} onClick={() => input.current?.click()}>
                    {preview.isPending ? <Loader2Icon className="animate-spin" /> : <UploadIcon />}
                    Import a file
                </Button>
                <input
                    ref={input}
                    type="file"
                    accept=".yaml,.yml,application/yaml,text/yaml"
                    className="hidden"
                    onChange={handleFile}
                />
            </CardFooter>

            <Dialog open={open} onOpenChange={(open) => !open && close()}>
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle>{result?.applied ? 'Import finished' : `Import ${file?.name}`}</DialogTitle>
                        <DialogDescription>
                            {result?.applied
                                ? 'These are the results for each section.'
                                : hasChanges
                                  ? 'These sections change when you apply the file.'
                                  : 'The file has the same settings as Homelab. There is nothing to apply.'}
                        </DialogDescription>
                    </DialogHeader>

                    <ul className="divide-y rounded-lg border">
                        {changes.map((change) => {
                            const badge = statusBadges[change.status]
                            return (
                                <li key={change.section} className="flex flex-col gap-1 px-3 py-2">
                                    <div className="flex items-center justify-between gap-3 text-sm">
                                        <span className="font-medium">{sectionTitles[change.section]}</span>
                                        <Badge variant="outline" className={cn(badge.className)}>
                                            {badge.label}
                                        </Badge>
                                    </div>
                                    {change.message && (
                                        <p className="text-xs text-muted-foreground">{change.message}</p>
                                    )}
                                </li>
                            )
                        })}
                    </ul>

                    <DialogFooter>
                        {result?.applied ? (
                            <Button onClick={close}>Close</Button>
                        ) : (
                            <>
                                <Button variant="outline" onClick={close}>
                                    Cancel
                                </Button>
                                <Button
                                    disabled={!hasChanges || apply.isPending}
                                    onClick={() => file && apply.mutate(file.yaml)}
                                >
                                    {apply.isPending && <Loader2Icon className="animate-spin" />}
                                    Apply changes
                                </Button>
                            </>
                        )}
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </Card>
    )
}
