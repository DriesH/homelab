import { Card } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import type { LogEntry } from '@/lib/api'
import { cn } from '@/lib/utils'

const levelStyles: Record<number, { label: string; className: string }> = {
    0: { label: 'EMERG', className: 'text-destructive' },
    1: { label: 'ALERT', className: 'text-destructive' },
    2: { label: 'CRIT', className: 'text-destructive' },
    3: { label: 'ERROR', className: 'text-destructive' },
    4: { label: 'WARN', className: 'text-amber-600 dark:text-amber-400' },
    5: { label: 'NOTE', className: 'text-sky-600 dark:text-sky-400' },
    6: { label: 'INFO', className: 'text-muted-foreground' },
    7: { label: 'DEBUG', className: 'text-muted-foreground/60' },
}

const timeFormat = new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' })
const dateFormat = new Intl.DateTimeFormat(undefined, { month: 'short', day: 'numeric' })

function formatTime(value: string) {
    const date = new Date(value)
    const today = new Date().toDateString() === date.toDateString()

    return today ? timeFormat.format(date) : `${dateFormat.format(date)} ${timeFormat.format(date)}`
}

type LogListProps = {
    entries: LogEntry[]
    loading: boolean
    onSelect?: (entry: LogEntry) => void
}

export function LogList({ entries, loading, onSelect }: LogListProps) {
    if (loading) {
        return <Skeleton className="h-96 w-full rounded-xl" />
    }

    if (entries.length === 0) {
        return <Card className="p-6 text-center text-sm text-muted-foreground">No lines match the filters.</Card>
    }

    return (
        <Card className="gap-0 overflow-hidden py-0">
            <ol className="max-h-[70vh] divide-y overflow-auto font-mono text-xs">
                {entries.map((entry, index) => {
                    const level = levelStyles[entry.level] ?? levelStyles[6]
                    const row = (
                        <>
                            <span className="shrink-0 text-muted-foreground tabular-nums">
                                {formatTime(entry.time)}
                            </span>
                            <span className={cn('w-12 shrink-0 font-semibold', level.className)}>{level.label}</span>
                            <span className="w-28 shrink-0 truncate text-muted-foreground" title={entry.source}>
                                {entry.source}
                            </span>
                            <span className="min-w-0 basis-full break-words whitespace-pre-wrap sm:flex-1 sm:basis-auto">
                                {entry.message}
                            </span>
                        </>
                    )

                    return (
                        <li key={`${entry.time}-${index}`}>
                            {onSelect ? (
                                <button
                                    type="button"
                                    className="flex w-full flex-wrap gap-x-3 gap-y-0.5 px-4 py-1.5 text-left hover:bg-muted/50 sm:flex-nowrap"
                                    onClick={() => onSelect(entry)}
                                >
                                    {row}
                                </button>
                            ) : (
                                <div className="flex flex-wrap gap-x-3 gap-y-0.5 px-4 py-1.5 sm:flex-nowrap">{row}</div>
                            )}
                        </li>
                    )
                })}
            </ol>
        </Card>
    )
}
