import { useState, type FormEvent } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { api, type Jellyfin } from '@/lib/api'
import { jellyfinQuery } from '@/lib/queries'

export function JellyfinSettings({ jellyfin }: { jellyfin: Jellyfin }) {
    const queryClient = useQueryClient()
    const [url, setUrl] = useState(jellyfin.url)
    const [apiKey, setApiKey] = useState('')

    const save = useMutation({
        mutationFn: api.saveJellyfinSettings,
        onSuccess() {
            setApiKey('')
            toast.success('Connected to Jellyfin')
            queryClient.invalidateQueries({ queryKey: jellyfinQuery.queryKey })
        },
        onError: (error) => toast.error(error.message),
    })

    function handleSubmit(event: FormEvent) {
        event.preventDefault()
        save.mutate({ url, apiKey })
    }

    return (
        <Card>
            <form onSubmit={handleSubmit} className="flex flex-col gap-6">
                <CardHeader>
                    <CardTitle>Connection</CardTitle>
                    <CardDescription>
                        Create an API key in Jellyfin: Dashboard, then API Keys. The key stays on this server.
                    </CardDescription>
                </CardHeader>
                <CardContent className="grid gap-4 sm:grid-cols-2">
                    <div className="flex flex-col gap-2">
                        <Label htmlFor="jellyfin-url">Jellyfin URL</Label>
                        <Input
                            id="jellyfin-url"
                            required
                            placeholder="http://192.168.1.20:8096"
                            value={url}
                            onChange={(event) => setUrl(event.target.value)}
                        />
                    </div>
                    <div className="flex flex-col gap-2">
                        <Label htmlFor="jellyfin-key">API key</Label>
                        <Input
                            id="jellyfin-key"
                            type="password"
                            autoComplete="off"
                            required={!jellyfin.configured}
                            placeholder={jellyfin.configured ? 'Saved. Type to replace it.' : ''}
                            value={apiKey}
                            onChange={(event) => setApiKey(event.target.value)}
                        />
                    </div>
                </CardContent>
                <CardFooter>
                    <Button type="submit" disabled={save.isPending}>
                        {save.isPending ? 'Connecting…' : 'Save'}
                    </Button>
                </CardFooter>
            </form>
        </Card>
    )
}
