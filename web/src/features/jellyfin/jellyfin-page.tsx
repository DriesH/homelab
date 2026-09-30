import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertTriangleIcon, ExternalLinkIcon } from 'lucide-react'
import { toast } from 'sonner'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardAction, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { api, jellyfinImage, type Jellyfin } from '@/lib/api'
import { jellyfinQuery } from '@/lib/queries'
import { JellyfinSettings } from './jellyfin-settings'
import { NowPlaying } from './now-playing'

export function JellyfinPage() {
    const { data, isPending, error } = useQuery(jellyfinQuery)

    if (isPending) {
        return (
            <div className="flex flex-col gap-6">
                <Skeleton className="h-10 w-64" />
                <Skeleton className="h-28 w-full rounded-xl" />
                <Skeleton className="h-64 w-full rounded-xl" />
            </div>
        )
    }

    if (error) {
        return (
            <Alert variant="destructive">
                <AlertTriangleIcon />
                <AlertTitle>Could not load Jellyfin</AlertTitle>
                <AlertDescription>{error.message}</AlertDescription>
            </Alert>
        )
    }

    if (!data.configured) {
        return (
            <div className="flex flex-col gap-6">
                <h1 className="text-lg font-semibold">Jellyfin</h1>
                <JellyfinSettings jellyfin={data} />
            </div>
        )
    }

    return (
        <div className="flex flex-col gap-8">
            <header className="flex flex-wrap items-center justify-between gap-3">
                <div>
                    <h1 className="text-lg font-semibold">Jellyfin</h1>
                    <p className="text-sm text-muted-foreground">
                        {data.serverName ? `${data.serverName} · version ${data.version}` : data.url}
                    </p>
                </div>
                <Button variant="outline" render={<a href={data.url} target="_blank" rel="noreferrer" />}>
                    Open Jellyfin
                    <ExternalLinkIcon />
                </Button>
            </header>

            {data.error ? (
                <Alert variant="destructive">
                    <AlertTriangleIcon />
                    <AlertTitle>Jellyfin is not reachable</AlertTitle>
                    <AlertDescription>{data.error}</AlertDescription>
                </Alert>
            ) : (
                <>
                    <div className="grid grid-cols-3 gap-4">
                        <Stat label="Movies" value={data.movies} />
                        <Stat label="Shows" value={data.series} />
                        <Stat label="Episodes" value={data.episodes} />
                    </div>

                    <section className="flex flex-col gap-3">
                        <h2 className="text-lg font-semibold">Now playing</h2>
                        <NowPlaying sessions={data.nowPlaying} />
                    </section>

                    <ThemeCard jellyfin={data} />

                    <section className="flex flex-col gap-3">
                        <h2 className="text-lg font-semibold">Recently added</h2>
                        <RecentlyAdded items={data.recent} />
                    </section>
                </>
            )}

            <JellyfinSettings jellyfin={data} />
        </div>
    )
}

function Stat({ label, value }: { label: string; value: number }) {
    return (
        <Card className="gap-1 px-5 py-4">
            <span className="text-sm text-muted-foreground">{label}</span>
            <span className="text-2xl font-semibold tabular-nums">{value.toLocaleString()}</span>
        </Card>
    )
}

function ThemeCard({ jellyfin }: { jellyfin: Jellyfin }) {
    const queryClient = useQueryClient()
    const theme = useMutation({
        mutationFn: api.setJellyfinTheme,
        onSuccess(_, enabled) {
            toast.success(enabled ? 'Netflix theme on. Reload Jellyfin to see it.' : 'Netflix theme off')
            queryClient.invalidateQueries({ queryKey: jellyfinQuery.queryKey })
        },
        onError: (error) => toast.error(error.message),
    })

    return (
        <Card>
            <CardHeader>
                <CardTitle>Netflix theme</CardTitle>
                <CardDescription>
                    A dark, Netflix-style look for the Jellyfin web client. The TV and phone apps do not use custom
                    themes. Your own custom CSS in Jellyfin stays.
                </CardDescription>
                <CardAction>
                    <Switch
                        checked={jellyfin.themeEnabled}
                        disabled={theme.isPending}
                        onCheckedChange={(enabled) => theme.mutate(enabled)}
                        aria-label="Netflix theme"
                    />
                </CardAction>
            </CardHeader>
        </Card>
    )
}

function RecentlyAdded({ items }: { items: Jellyfin['recent'] }) {
    if (items.length === 0) {
        return <Card className="p-6 text-center text-sm text-muted-foreground">No movies or shows yet.</Card>
    }

    return (
        <ul className="flex gap-4 overflow-x-auto pb-2">
            {items.map((item) => (
                <li key={item.id} className="w-32 shrink-0">
                    <img
                        src={jellyfinImage(item.id, 'Primary')}
                        alt=""
                        loading="lazy"
                        className="aspect-2/3 w-full rounded-md bg-muted object-cover"
                    />
                    <p className="mt-2 truncate text-sm font-medium">{item.name}</p>
                    <p className="text-xs text-muted-foreground">
                        {item.type === 'Series' ? 'Show' : 'Movie'}
                        {item.year ? ` · ${item.year}` : ''}
                    </p>
                </li>
            ))}
        </ul>
    )
}
