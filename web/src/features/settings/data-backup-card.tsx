import { useEffect, useState, type FormEvent } from 'react'
import { useMutation } from '@tanstack/react-query'
import { DownloadIcon, Loader2Icon, TriangleAlertIcon } from 'lucide-react'
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
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { api } from '@/lib/api'

const minPassphrase = 12

export function DataBackupCard() {
    return (
        <Card>
            <CardHeader>
                <CardTitle>Data backup</CardTitle>
                <CardDescription>
                    An encrypted copy of everything Homelab keeps: your account, tokens, settings, history and the
                    certificate your devices trust. The Proxmox backups already have it. Use this file when those are
                    gone, or to move Homelab to a new server.
                </CardDescription>
            </CardHeader>
            <CardContent className="grid gap-8 lg:grid-cols-2">
                <DownloadForm />
                <RestoreForm />
            </CardContent>
        </Card>
    )
}

function DownloadForm() {
    const [password, setPassword] = useState('')
    const [passphrase, setPassphrase] = useState('')
    const [repeat, setRepeat] = useState('')

    const download = useMutation({
        mutationFn: () => api.downloadDataBackup(password, passphrase),
        onSuccess: ({ blob, name }) => {
            const url = URL.createObjectURL(blob)
            const link = document.createElement('a')
            link.href = url
            link.download = name
            link.click()
            URL.revokeObjectURL(url)
            setPassword('')
            setPassphrase('')
            setRepeat('')
            toast.success('Backup downloaded. Keep the passphrase somewhere safe.')
        },
        onError: (error) => toast.error(error.message),
    })

    const tooShort = passphrase.length > 0 && passphrase.length < minPassphrase
    const mismatch = repeat.length > 0 && repeat !== passphrase

    function handleSubmit(event: FormEvent) {
        event.preventDefault()
        download.mutate()
    }

    return (
        <form className="flex flex-col gap-4" onSubmit={handleSubmit}>
            <h3 className="text-sm font-medium">Download a backup</h3>
            <div className="flex flex-col gap-2">
                <Label htmlFor="backup-password">Your password</Label>
                <Input
                    id="backup-password"
                    type="password"
                    autoComplete="current-password"
                    value={password}
                    onChange={(event) => setPassword(event.target.value)}
                    required
                />
            </div>
            <div className="flex flex-col gap-2">
                <Label htmlFor="backup-passphrase">Passphrase for the file</Label>
                <Input
                    id="backup-passphrase"
                    type="password"
                    autoComplete="new-password"
                    value={passphrase}
                    onChange={(event) => setPassphrase(event.target.value)}
                    aria-invalid={tooShort}
                    aria-describedby="backup-passphrase-help"
                    required
                />
                <p id="backup-passphrase-help" className="text-xs text-muted-foreground">
                    At least {minPassphrase} characters. Without it, nobody can open the file, not even you.
                </p>
            </div>
            <div className="flex flex-col gap-2">
                <Label htmlFor="backup-repeat">Repeat the passphrase</Label>
                <Input
                    id="backup-repeat"
                    type="password"
                    autoComplete="new-password"
                    value={repeat}
                    onChange={(event) => setRepeat(event.target.value)}
                    aria-invalid={mismatch}
                    required
                />
                {mismatch && <p className="text-xs text-destructive">The passphrases are not the same.</p>}
            </div>
            <Button
                type="submit"
                className="w-fit"
                disabled={download.isPending || !password || passphrase.length < minPassphrase || repeat !== passphrase}
            >
                {download.isPending ? <Loader2Icon className="animate-spin" /> : <DownloadIcon />}
                Download backup
            </Button>
        </form>
    )
}

function RestoreForm() {
    const [file, setFile] = useState<File | null>(null)
    const [password, setPassword] = useState('')
    const [passphrase, setPassphrase] = useState('')
    const [confirmOpen, setConfirmOpen] = useState(false)

    const restore = useMutation({
        mutationFn: () => api.restoreDataBackup(file!, password, passphrase),
        onSuccess: () => setConfirmOpen(false),
        onError: (error) => {
            setConfirmOpen(false)
            toast.error(error.message)
        },
    })

    if (restore.isSuccess) {
        return <Restarting />
    }

    function handleSubmit(event: FormEvent) {
        event.preventDefault()
        setConfirmOpen(true)
    }

    return (
        <form className="flex flex-col gap-4" onSubmit={handleSubmit}>
            <h3 className="text-sm font-medium">Restore a backup</h3>
            <div className="flex flex-col gap-2">
                <Label htmlFor="restore-file">Backup file</Label>
                <Input
                    id="restore-file"
                    type="file"
                    accept=".hlbackup"
                    onChange={(event) => setFile(event.target.files?.[0] ?? null)}
                    required
                />
            </div>
            <div className="flex flex-col gap-2">
                <Label htmlFor="restore-password">Your password</Label>
                <Input
                    id="restore-password"
                    type="password"
                    autoComplete="current-password"
                    value={password}
                    onChange={(event) => setPassword(event.target.value)}
                    required
                />
            </div>
            <div className="flex flex-col gap-2">
                <Label htmlFor="restore-passphrase">Passphrase of the file</Label>
                <Input
                    id="restore-passphrase"
                    type="password"
                    autoComplete="off"
                    value={passphrase}
                    onChange={(event) => setPassphrase(event.target.value)}
                    required
                />
            </div>
            <Button type="submit" variant="outline" className="w-fit" disabled={!file || !password || !passphrase}>
                Restore…
            </Button>

            <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
                <AlertDialogContent>
                    <AlertDialogHeader>
                        <AlertDialogTitle>Replace all Homelab data?</AlertDialogTitle>
                        <AlertDialogDescription>
                            Homelab replaces its account, tokens, settings and history with the ones in {file?.name} and
                            restarts. Then you log in with the account from the backup. Your containers do not change.
                        </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                        <AlertDialogCancel disabled={restore.isPending}>Cancel</AlertDialogCancel>
                        <AlertDialogAction
                            variant="destructive"
                            disabled={restore.isPending}
                            onClick={(event) => {
                                event.preventDefault()
                                restore.mutate()
                            }}
                        >
                            {restore.isPending && <Loader2Icon className="animate-spin" />}
                            Restore and restart
                        </AlertDialogAction>
                    </AlertDialogFooter>
                </AlertDialogContent>
            </AlertDialog>
        </form>
    )
}

// Restarting waits until the manager answers again, then opens the login page.
function Restarting() {
    const [failed, setFailed] = useState(false)

    useEffect(() => {
        let stopped = false
        const started = Date.now()

        async function poll() {
            // The manager answers for about a second before it stops.
            await new Promise((resolve) => setTimeout(resolve, 3000))
            while (!stopped) {
                const answered = await fetch('/api/auth/me', { credentials: 'same-origin' }).then(
                    (response) => response.status < 500,
                    () => false,
                )
                if (answered) {
                    window.location.assign('/login')
                    return
                }
                if (Date.now() - started > 90_000) {
                    setFailed(true)
                    return
                }
                await new Promise((resolve) => setTimeout(resolve, 2000))
            }
        }

        poll()
        return () => {
            stopped = true
        }
    }, [])

    if (failed) {
        return (
            <Alert variant="destructive">
                <TriangleAlertIcon />
                <AlertTitle>Homelab did not come back</AlertTitle>
                <AlertDescription>
                    Look at its log on the Proxmox host: pct exec &lt;id&gt; -- journalctl -u homelab
                </AlertDescription>
            </Alert>
        )
    }

    return (
        <Alert>
            <Loader2Icon className="animate-spin" />
            <AlertTitle>Restored. Homelab restarts…</AlertTitle>
            <AlertDescription>The login page opens when it runs again.</AlertDescription>
        </Alert>
    )
}
