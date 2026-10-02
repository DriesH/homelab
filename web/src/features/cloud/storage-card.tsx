import { useState, type ChangeEvent, type FormEvent } from 'react'
import { useMutation } from '@tanstack/react-query'
import { Loader2Icon, PlugZapIcon } from 'lucide-react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Field } from '@/features/apps/form-parts'
import { api, type Cloud, type CloudProvider, type CloudSettings } from '@/lib/api'
import { useCloudMutation } from './use-cloud'

type ProviderInfo = { name: string; kind: 's3' | 'sftp'; note: string }

const providers: Record<CloudProvider, ProviderInfo> = {
    r2: { name: 'Cloudflare R2', kind: 's3', note: 'No fees to download, so streaming costs nothing extra.' },
    b2: { name: 'Backblaze B2', kind: 's3', note: 'Downloads are free up to 3 times what you store each month.' },
    hetzner: {
        name: 'Hetzner Object Storage',
        kind: 's3',
        note: 'The base price includes 1 TB to download each month.',
    },
    wasabi: {
        name: 'Wasabi',
        kind: 's3',
        note: 'No fees to download while you download less than you store. Files are billed for 90 days at least.',
    },
    aws: {
        name: 'Amazon S3',
        kind: 's3',
        note: 'Amazon charges for each GB you download, so every movie you watch from the cloud costs money.',
    },
    s3: { name: 'Another S3 storage', kind: 's3', note: 'Any storage with an S3 API, like MinIO.' },
    'storage-box': {
        name: 'Hetzner Storage Box',
        kind: 'sftp',
        note: 'Unlimited traffic. Homelab connects over SFTP on port 23.',
    },
    sftp: { name: 'An SFTP server', kind: 'sftp', note: 'Any server that you can reach with SFTP and a password.' },
}

const hetznerLocations = { fsn1: 'Falkenstein (fsn1)', nbg1: 'Nuremberg (nbg1)', hel1: 'Helsinki (hel1)' }

const defaultPath = 'homelab-media'

function initialSettings(cloud: Cloud): CloudSettings {
    if (!cloud.settings) {
        return { provider: 'r2', path: defaultPath }
    }
    // The secrets never come back from the server, so the form starts without them.
    const { provider, accountId, bucket, region, endpoint, accessKey, host, port, user, path } = cloud.settings

    return { provider, accountId, bucket, region, endpoint, accessKey, host, port, user, path }
}

export function StorageCard({ cloud, locked }: { cloud: Cloud; locked: boolean }) {
    const [settings, setSettings] = useState(() => initialSettings(cloud))
    const saved = cloud.settings?.provider === settings.provider ? cloud.settings : null
    const provider = providers[settings.provider]

    const set = (values: Partial<CloudSettings>) => setSettings((current) => ({ ...current, ...values }))
    const field = (key: 'accountId' | 'bucket' | 'region' | 'endpoint' | 'accessKey' | 'host' | 'user' | 'path') => ({
        id: `cloud-${key}`,
        value: settings[key] ?? '',
        disabled: locked,
        onChange: (event: ChangeEvent<HTMLInputElement>) => set({ [key]: event.target.value.trim() }),
    })

    const save = useCloudMutation(
        () => api.saveCloudSettings(settings),
        'Could not save the storage',
        () => {
            toast.success('Storage saved')
            set({ secretKey: '', password: '' })
        },
    )
    const test = useMutation({
        mutationFn: () => api.testCloud(settings),
        onSuccess: () => toast.success(`Homelab reached ${provider.name}`),
        onError: (error) => toast.error('The test failed', { description: error.message }),
    })

    function chooseProvider(next: CloudProvider) {
        setSettings({ provider: next, path: settings.path ?? defaultPath })
    }

    function handleSubmit(event: FormEvent) {
        event.preventDefault()
        save.mutate()
    }

    return (
        <Card>
            <form onSubmit={handleSubmit} className="flex flex-col gap-6">
                <CardHeader>
                    <CardTitle>Storage</CardTitle>
                    <CardDescription>
                        {locked
                            ? 'Turn cloud storage off first to change the storage.'
                            : 'Where the older media goes. Make a bucket or a folder for Homelab only, and keys that can only reach it.'}
                    </CardDescription>
                </CardHeader>
                <CardContent className="flex flex-col gap-4">
                    <Field id="cloud-provider" label="Provider" help={provider.note}>
                        <Select
                            items={Object.fromEntries(Object.entries(providers).map(([id, info]) => [id, info.name]))}
                            value={settings.provider}
                            onValueChange={(value) => value && chooseProvider(value as CloudProvider)}
                        >
                            <SelectTrigger id="cloud-provider" className="w-full" disabled={locked}>
                                <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                                {Object.entries(providers).map(([id, info]) => (
                                    <SelectItem key={id} value={id}>
                                        {info.name}
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                    </Field>

                    <div className="grid gap-4 sm:grid-cols-2">
                        {settings.provider === 'r2' && (
                            <Field
                                id="cloud-accountId"
                                label="Account ID"
                                help="On the R2 overview page of Cloudflare."
                            >
                                <Input {...field('accountId')} required />
                            </Field>
                        )}
                        {settings.provider === 'hetzner' && (
                            <Field id="cloud-region" label="Location">
                                <Select
                                    items={hetznerLocations}
                                    value={settings.region ?? null}
                                    onValueChange={(value) => value && set({ region: value })}
                                >
                                    <SelectTrigger id="cloud-region" className="w-full" disabled={locked}>
                                        <SelectValue placeholder="Choose the location of the bucket" />
                                    </SelectTrigger>
                                    <SelectContent>
                                        {Object.entries(hetznerLocations).map(([value, label]) => (
                                            <SelectItem key={value} value={value}>
                                                {label}
                                            </SelectItem>
                                        ))}
                                    </SelectContent>
                                </Select>
                            </Field>
                        )}
                        {(settings.provider === 'aws' ||
                            settings.provider === 'wasabi' ||
                            settings.provider === 's3') && (
                            <Field
                                id="cloud-region"
                                label="Region"
                                help={
                                    settings.provider === 's3' ? 'Leave it empty when the storage has none.' : undefined
                                }
                            >
                                <Input
                                    {...field('region')}
                                    required={settings.provider === 'aws'}
                                    placeholder={settings.provider === 'wasabi' ? 'us-east-1' : 'eu-central-1'}
                                />
                            </Field>
                        )}
                        {settings.provider === 's3' && (
                            <Field id="cloud-endpoint" label="Endpoint">
                                <Input {...field('endpoint')} required placeholder="https://s3.example.com" />
                            </Field>
                        )}

                        {provider.kind === 's3' ? (
                            <>
                                <Field id="cloud-bucket" label="Bucket">
                                    <Input {...field('bucket')} required />
                                </Field>
                                <Field
                                    id="cloud-accessKey"
                                    label={settings.provider === 'b2' ? 'Key ID' : 'Access key ID'}
                                >
                                    <Input {...field('accessKey')} required autoComplete="off" />
                                </Field>
                                <Field
                                    id="cloud-secretKey"
                                    label={settings.provider === 'b2' ? 'Application key' : 'Secret access key'}
                                >
                                    <Input
                                        id="cloud-secretKey"
                                        type="password"
                                        autoComplete="off"
                                        disabled={locked}
                                        required={!saved?.hasSecretKey}
                                        placeholder={saved?.hasSecretKey ? 'Saved. Type to replace it.' : undefined}
                                        value={settings.secretKey ?? ''}
                                        onChange={(event) => set({ secretKey: event.target.value.trim() })}
                                    />
                                </Field>
                            </>
                        ) : (
                            <>
                                <Field
                                    id="cloud-user"
                                    label="Username"
                                    help={settings.provider === 'storage-box' ? 'Like u123456.' : undefined}
                                >
                                    <Input {...field('user')} required autoComplete="off" />
                                </Field>
                                <Field id="cloud-password" label="Password">
                                    <Input
                                        id="cloud-password"
                                        type="password"
                                        autoComplete="off"
                                        disabled={locked}
                                        required={!saved?.hasPassword}
                                        placeholder={saved?.hasPassword ? 'Saved. Type to replace it.' : undefined}
                                        value={settings.password ?? ''}
                                        onChange={(event) => set({ password: event.target.value })}
                                    />
                                </Field>
                                <Field
                                    id="cloud-host"
                                    label="Server"
                                    help={
                                        settings.provider === 'storage-box'
                                            ? 'Leave it empty for <username>.your-storagebox.de.'
                                            : undefined
                                    }
                                >
                                    <Input {...field('host')} required={settings.provider === 'sftp'} />
                                </Field>
                                {settings.provider === 'sftp' && (
                                    <Field id="cloud-port" label="Port">
                                        <Input
                                            id="cloud-port"
                                            type="number"
                                            min={1}
                                            max={65535}
                                            placeholder="22"
                                            disabled={locked}
                                            value={settings.port ?? ''}
                                            onChange={(event) =>
                                                set({
                                                    port: event.target.value ? Number(event.target.value) : undefined,
                                                })
                                            }
                                        />
                                    </Field>
                                )}
                            </>
                        )}

                        <Field
                            id="cloud-path"
                            label="Folder"
                            help={
                                provider.kind === 's3'
                                    ? 'A folder in the bucket. Leave it empty to use the whole bucket.'
                                    : 'A folder in your home folder on the server.'
                            }
                        >
                            <Input {...field('path')} />
                        </Field>
                    </div>
                </CardContent>
                <CardFooter className="gap-2">
                    <Button type="submit" disabled={locked || save.isPending}>
                        {save.isPending && <Loader2Icon className="animate-spin" />}
                        Save
                    </Button>
                    <Button
                        type="button"
                        variant="outline"
                        disabled={locked || test.isPending}
                        onClick={() => test.mutate()}
                    >
                        {test.isPending ? <Loader2Icon className="animate-spin" /> : <PlugZapIcon />}
                        Test the connection
                    </Button>
                </CardFooter>
            </form>
        </Card>
    )
}
