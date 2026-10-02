import { useEffect, useRef, type ReactNode } from 'react'
import { Loader2Icon, TriangleAlertIcon } from 'lucide-react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'

type JobProgressProps = {
    state: 'running' | 'failed'
    title: string
    detail: ReactNode
    log?: string
}

// JobProgress shows a job on the host that runs or failed, with its log.
export function JobProgress({ state, title, detail, log }: JobProgressProps) {
    const logRef = useRef<HTMLPreElement>(null)

    // Follow the log while it grows.
    useEffect(() => {
        logRef.current?.scrollTo({ top: logRef.current.scrollHeight })
    }, [log])

    const logView = log && (
        <pre
            ref={logRef}
            className="max-h-72 overflow-auto rounded-md bg-muted p-3 font-mono text-xs whitespace-pre-wrap text-foreground"
        >
            {log}
        </pre>
    )

    if (state === 'running') {
        return (
            <div className="flex flex-col gap-3">
                <Alert>
                    <Loader2Icon className="animate-spin" />
                    <AlertTitle>{title}</AlertTitle>
                    <AlertDescription>{detail}</AlertDescription>
                </Alert>
                {logView}
            </div>
        )
    }

    return (
        <Alert variant="destructive">
            <TriangleAlertIcon />
            <AlertTitle>{title}</AlertTitle>
            <AlertDescription className="flex flex-col gap-2">
                <span>{detail}</span>
                {logView && (
                    <details open>
                        <summary className="cursor-pointer">Log</summary>
                        <div className="mt-2">{logView}</div>
                    </details>
                )}
            </AlertDescription>
        </Alert>
    )
}
