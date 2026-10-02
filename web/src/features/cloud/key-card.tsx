import { useState, type FormEvent } from 'react'
import { useMutation } from '@tanstack/react-query'
import { REGEXP_ONLY_DIGITS } from 'input-otp'
import { CheckIcon, CopyIcon, DownloadIcon, KeyRoundIcon, Loader2Icon, TriangleAlertIcon } from 'lucide-react'
import { toast } from 'sonner'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { InputOTP, InputOTPGroup, InputOTPSlot } from '@/components/ui/input-otp'
import { Field } from '@/features/apps/form-parts'
import { api, type Cloud } from '@/lib/api'
import { useCloudMutation } from './use-cloud'

const codeLength = 6

export function KeyCard({ cloud }: { cloud: Cloud }) {
    const [newKey, setNewKey] = useState<string | null>(null)
    const [revealOpen, setRevealOpen] = useState(false)

    const create = useCloudMutation(api.createCloudKey, 'Could not make the recovery key', ({ key }) => setNewKey(key))

    return (
        <Card>
            <CardHeader>
                <CardTitle>Recovery key</CardTitle>
                <CardDescription>
                    The key that encrypts your media in the cloud. Without it, nobody can read that media, not even you.
                    Keep it in your password manager, outside this server.
                </CardDescription>
                {cloud.key === 'confirmed' && (
                    <CardAction>
                        <Badge variant="outline">
                            <CheckIcon />
                            Saved
                        </Badge>
                    </CardAction>
                )}
            </CardHeader>
            <CardContent className="flex flex-col gap-4">
                {cloud.key === 'unconfirmed' && (
                    <Alert>
                        <TriangleAlertIcon />
                        <AlertTitle>The recovery key is not saved yet</AlertTitle>
                        <AlertDescription>Make a new one, save it, and type its last two groups.</AlertDescription>
                    </Alert>
                )}
                <div>
                    {cloud.key === 'confirmed' ? (
                        <Button variant="outline" onClick={() => setRevealOpen(true)}>
                            <KeyRoundIcon />
                            Show the recovery key
                        </Button>
                    ) : (
                        <Button disabled={create.isPending} onClick={() => create.mutate()}>
                            {create.isPending ? <Loader2Icon className="animate-spin" /> : <KeyRoundIcon />}
                            {cloud.key === 'none' ? 'Make the recovery key' : 'Make a new recovery key'}
                        </Button>
                    )}
                </div>
            </CardContent>

            {newKey && <NewKeyDialog keyText={newKey} onClose={() => setNewKey(null)} />}
            <RevealDialog open={revealOpen} onOpenChange={setRevealOpen} />
        </Card>
    )
}

// NewKeyDialog shows a new key once. The user types its last two groups to
// show that they saved it.
function NewKeyDialog({ keyText, onClose }: { keyText: string; onClose: () => void }) {
    const [groups, setGroups] = useState('')
    const confirm = useCloudMutation(
        () => api.confirmCloudKey(groups),
        'That did not match',
        () => {
            toast.success('Recovery key saved')
            onClose()
        },
    )

    function handleSubmit(event: FormEvent) {
        event.preventDefault()
        confirm.mutate()
    }

    return (
        <Dialog open onOpenChange={(open) => !open && onClose()}>
            <DialogContent className="sm:max-w-lg">
                <form onSubmit={handleSubmit} className="flex flex-col gap-4">
                    <DialogHeader>
                        <DialogTitle>Save your recovery key</DialogTitle>
                        <DialogDescription>
                            Put it in your password manager now. If this server and its backups are lost, this key is
                            the only way to read your media in the cloud.
                        </DialogDescription>
                    </DialogHeader>
                    <KeyBox keyText={keyText} />
                    <Field
                        id="key-groups"
                        label="Type the last two groups of the key"
                        help="So Homelab knows that you saved it. The groups are split by dashes."
                    >
                        <Input
                            id="key-groups"
                            autoComplete="off"
                            className="font-mono uppercase"
                            placeholder="XXXX-XXX"
                            value={groups}
                            onChange={(event) => setGroups(event.target.value)}
                            required
                        />
                    </Field>
                    <DialogFooter>
                        <Button type="submit" disabled={confirm.isPending}>
                            {confirm.isPending && <Loader2Icon className="animate-spin" />}I saved the key
                        </Button>
                    </DialogFooter>
                </form>
            </DialogContent>
        </Dialog>
    )
}

// RevealDialog shows the key again, after the password and an authenticator code.
function RevealDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
    const [password, setPassword] = useState('')
    const [code, setCode] = useState('')
    const reveal = useMutation({
        mutationFn: () => api.revealCloudKey(password, code),
        onError: (error) => {
            setCode('')
            toast.error('Could not show the key', { description: error.message })
        },
    })

    function close(next: boolean) {
        if (!next) {
            setPassword('')
            setCode('')
            reveal.reset()
        }
        onOpenChange(next)
    }

    function handleSubmit(event: FormEvent) {
        event.preventDefault()
        reveal.mutate()
    }

    return (
        <Dialog open={open} onOpenChange={close}>
            <DialogContent className="sm:max-w-lg">
                <DialogHeader>
                    <DialogTitle>Recovery key</DialogTitle>
                    <DialogDescription>
                        {reveal.data
                            ? 'When Telegram is set up on the Updates page, Homelab sends a message each time the key is shown.'
                            : 'The key opens all your media in the cloud, so Homelab asks for your password and a code first.'}
                    </DialogDescription>
                </DialogHeader>
                {reveal.data ? (
                    <KeyBox keyText={reveal.data.key} />
                ) : (
                    <form onSubmit={handleSubmit} className="flex flex-col gap-4">
                        <Field id="reveal-password" label="Your password">
                            <Input
                                id="reveal-password"
                                type="password"
                                autoComplete="current-password"
                                value={password}
                                onChange={(event) => setPassword(event.target.value)}
                                required
                            />
                        </Field>
                        <div className="flex flex-col gap-2">
                            <span className="text-sm font-medium">Authenticator code</span>
                            <InputOTP
                                maxLength={codeLength}
                                pattern={REGEXP_ONLY_DIGITS}
                                inputMode="numeric"
                                autoComplete="one-time-code"
                                aria-label="Authenticator code"
                                value={code}
                                onChange={setCode}
                            >
                                <InputOTPGroup className="gap-2">
                                    {Array.from({ length: codeLength }, (_, index) => (
                                        <InputOTPSlot
                                            key={index}
                                            index={index}
                                            className="size-10 rounded-md border first:rounded-md last:rounded-md"
                                        />
                                    ))}
                                </InputOTPGroup>
                            </InputOTP>
                        </div>
                        <DialogFooter>
                            <Button type="submit" disabled={reveal.isPending || code.length !== codeLength}>
                                {reveal.isPending && <Loader2Icon className="animate-spin" />}
                                Show the key
                            </Button>
                        </DialogFooter>
                    </form>
                )}
            </DialogContent>
        </Dialog>
    )
}

function KeyBox({ keyText }: { keyText: string }) {
    async function copy() {
        try {
            await navigator.clipboard.writeText(keyText)
            toast.success('Key copied')
        } catch {
            toast.error('Could not copy the key, select it and copy it yourself')
        }
    }

    function download() {
        const blob = new Blob([`Homelab recovery key for the cloud storage\n\n${keyText}\n`], { type: 'text/plain' })
        const url = URL.createObjectURL(blob)
        const link = document.createElement('a')
        link.href = url
        link.download = 'homelab-recovery-key.txt'
        link.click()
        URL.revokeObjectURL(url)
    }

    return (
        <div className="flex flex-col gap-3">
            <code className="rounded-md bg-muted p-3 text-center font-mono text-sm break-all select-all">
                {keyText}
            </code>
            <div className="flex gap-2">
                <Button type="button" variant="outline" size="sm" onClick={copy}>
                    <CopyIcon />
                    Copy
                </Button>
                <Button type="button" variant="outline" size="sm" onClick={download}>
                    <DownloadIcon />
                    Download
                </Button>
            </div>
        </div>
    )
}
