import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { CircleCheckIcon, CircleXIcon, Undo2Icon } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import type { UpdateRun } from '@/lib/api'
import { formatRelative } from '@/lib/format'
import { updateRunQuery } from '@/lib/queries'

const statusIcons = {
    succeeded: <CircleCheckIcon className="size-4 text-emerald-500" aria-label="succeeded" />,
    failed: <CircleXIcon className="size-4 text-destructive" aria-label="failed" />,
    'rolled-back': <Undo2Icon className="size-4 text-amber-500" aria-label="rolled back" />,
}

export function UpdateHistory({ runs }: { runs: UpdateRun[] }) {
    const [openRun, setOpenRun] = useState<UpdateRun | null>(null)

    if (runs.length === 0) {
        return <Card className="p-6 text-center text-sm text-muted-foreground">Nothing has run yet.</Card>
    }

    return (
        <>
            <Card className="gap-0 py-0">
                <ul className="divide-y">
                    {runs.map((run) => (
                        <li key={run.id} className="flex items-center gap-3 px-4 py-3 text-sm">
                            {statusIcons[run.status]}
                            <div className="min-w-0 flex-1">
                                <div className="flex items-center gap-2">
                                    <span className="font-medium">{run.kind === 'check' ? 'Check' : run.target}</span>
                                    {run.scheduled && <Badge variant="secondary">scheduled</Badge>}
                                </div>
                                <p className="text-muted-foreground">{run.message}</p>
                            </div>
                            <span className="hidden text-muted-foreground sm:inline">
                                {formatRelative(run.finishedAt)}
                            </span>
                            {run.kind !== 'check' && (
                                <Button variant="ghost" size="sm" onClick={() => setOpenRun(run)}>
                                    Log
                                </Button>
                            )}
                        </li>
                    ))}
                </ul>
            </Card>

            <Dialog open={openRun !== null} onOpenChange={(open) => !open && setOpenRun(null)}>
                <DialogContent className="sm:max-w-3xl">{openRun && <RunLog run={openRun} />}</DialogContent>
            </Dialog>
        </>
    )
}

function RunLog({ run }: { run: UpdateRun }) {
    const { data, isPending, error } = useQuery(updateRunQuery(run.id))

    return (
        <>
            <DialogHeader>
                <DialogTitle>{run.target}</DialogTitle>
                <DialogDescription>
                    {run.message}
                    {run.snapshot && ` Snapshot: ${run.snapshot}.`}
                </DialogDescription>
            </DialogHeader>
            <pre className="max-h-[60vh] overflow-auto rounded-md bg-muted p-3 font-mono text-xs whitespace-pre-wrap">
                {isPending ? 'Loading…' : error ? error.message : data.log || 'No output.'}
            </pre>
        </>
    )
}
