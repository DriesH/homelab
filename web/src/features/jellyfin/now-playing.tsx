import { PauseIcon } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Card } from '@/components/ui/card'
import { Progress } from '@/components/ui/progress'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { jellyfinImage, type JellyfinNowPlaying } from '@/lib/api'

export function NowPlaying({ sessions }: { sessions: JellyfinNowPlaying[] }) {
    if (sessions.length === 0) {
        return <Card className="p-6 text-center text-sm text-muted-foreground">Nothing is playing.</Card>
    }

    return (
        <div className="grid gap-4 md:grid-cols-2">
            {sessions.map((session) => (
                <SessionCard key={`${session.user}-${session.device}-${session.itemId}`} session={session} />
            ))}
        </div>
    )
}

function SessionCard({ session }: { session: JellyfinNowPlaying }) {
    return (
        <Card className="relative gap-0 overflow-hidden py-0">
            <img
                src={jellyfinImage(session.itemId, 'Backdrop')}
                alt=""
                className="absolute inset-0 size-full object-cover opacity-30"
                onError={(event) => (event.currentTarget.style.display = 'none')}
            />
            <div className="relative flex flex-col gap-3 p-4">
                <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0">
                        <p className="truncate font-semibold">{session.title}</p>
                        <p className="truncate text-sm text-muted-foreground">{session.subtitle}</p>
                    </div>
                    <PlayMethod session={session} />
                </div>
                <div className="flex items-center gap-2 text-sm text-muted-foreground">
                    {session.paused && <PauseIcon className="size-4" aria-label="paused" />}
                    <span className="truncate">
                        {session.user} · {session.device}
                    </span>
                </div>
                <Progress value={session.progress} aria-label="Progress" />
            </div>
        </Card>
    )
}

function PlayMethod({ session }: { session: JellyfinNowPlaying }) {
    if (!session.transcode) {
        return <Badge variant="secondary">Direct play</Badge>
    }

    const badge = <Badge variant="destructive">Transcoding</Badge>
    if (session.reasons.length === 0) {
        return badge
    }

    return (
        <Tooltip>
            <TooltipTrigger render={<span />}>{badge}</TooltipTrigger>
            <TooltipContent>{session.reasons.join(', ')}</TooltipContent>
        </Tooltip>
    )
}
