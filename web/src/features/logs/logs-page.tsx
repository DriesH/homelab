import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { RefreshCwIcon, TriangleAlertIcon } from 'lucide-react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { api, type LogEntry, type TaskEntry } from '@/lib/api'
import { overviewQuery } from '@/lib/queries'
import { cn } from '@/lib/utils'
import { LogList } from './log-list'
import { TaskLogDialog } from './task-log-dialog'

type Kind = 'host' | 'container' | 'docker' | 'tasks'

const kinds: { value: Kind; label: string }[] = [
    { value: 'host', label: 'Host' },
    { value: 'container', label: 'Container' },
    { value: 'docker', label: 'Docker' },
    { value: 'tasks', label: 'Tasks' },
]

// Each level also shows the more important ones, like journalctl -p.
const levels: Record<string, string> = {
    '3': 'Errors',
    '4': 'Warnings and errors',
    '6': 'Info and more',
    '7': 'Everything',
}

const allSources = '__all'

export function LogsPage() {
    const [kind, setKind] = useState<Kind>('host')
    const [vmid, setVmid] = useState<string>('')
    const [level, setLevel] = useState('6')
    const [source, setSource] = useState(allSources)
    const [search, setSearch] = useState('')
    const [follow, setFollow] = useState(false)
    const [openTask, setOpenTask] = useState<TaskEntry | null>(null)

    const { data: overview } = useQuery(overviewQuery)
    const containers = (overview?.guests ?? []).filter((guest) => guest.type === 'lxc' && guest.status === 'running')
    const guestItems = Object.fromEntries(
        containers.map((guest) => [String(guest.vmid), `${guest.name} (${guest.vmid})`]),
    )
    const selectedVmid = vmid || String(containers[0]?.vmid ?? '')
    const needsGuest = kind === 'container' || kind === 'docker'

    const refetchInterval = follow ? 5000 : false
    const logs = useQuery({
        queryKey: ['logs', kind, needsGuest ? selectedVmid : '', kind === 'host' || kind === 'container' ? level : ''],
        queryFn: async (): Promise<LogEntry[]> => {
            switch (kind) {
                case 'host':
                    return api.journal(0, Number(level))
                case 'container':
                    return api.journal(Number(selectedVmid), Number(level))
                case 'docker':
                    return (await api.dockerLogs(Number(selectedVmid))).entries
                case 'tasks':
                    return api.tasks()
            }
        },
        enabled: !needsGuest || selectedVmid !== '',
        refetchInterval,
        retry: false,
    })

    const entries = useMemo(() => logs.data ?? [], [logs.data])
    const sources = useMemo(() => [...new Set(entries.map((entry) => entry.source))].sort(), [entries])
    const sourceItems = { [allSources]: 'All sources', ...Object.fromEntries(sources.map((name) => [name, name])) }

    // Journals are filtered by level on the server. Docker and task levels are filtered here.
    const visible = useMemo(() => {
        const needle = search.trim().toLowerCase()

        return entries
            .filter((entry) => entry.level <= Number(level))
            .filter((entry) => source === allSources || entry.source === source)
            .filter((entry) => !needle || entry.message.toLowerCase().includes(needle))
            .toReversed()
    }, [entries, level, source, search])

    function changeKind(next: Kind) {
        setKind(next)
        setSource(allSources)
    }

    return (
        <div className="flex flex-col gap-6">
            <header className="flex flex-wrap items-center justify-between gap-3">
                <div>
                    <h1 className="text-lg font-semibold">Logs</h1>
                    <p className="text-sm text-muted-foreground">
                        {logs.data ? `${visible.length} of ${entries.length} lines, newest first` : 'Loading…'}
                    </p>
                </div>
                <div className="flex items-center gap-4">
                    <label className="flex items-center gap-2 text-sm text-muted-foreground">
                        Follow
                        <Switch checked={follow} onCheckedChange={setFollow} aria-label="Refresh every 5 seconds" />
                    </label>
                    <Button variant="outline" disabled={logs.isFetching} onClick={() => logs.refetch()}>
                        <RefreshCwIcon className={cn(logs.isFetching && 'animate-spin')} />
                        Refresh
                    </Button>
                </div>
            </header>

            <div className="flex flex-col gap-4">
                <div className="inline-flex w-fit rounded-lg border p-0.5" role="tablist" aria-label="Log source">
                    {kinds.map((item) => (
                        <Button
                            key={item.value}
                            role="tab"
                            aria-selected={kind === item.value}
                            variant={kind === item.value ? 'secondary' : 'ghost'}
                            size="sm"
                            onClick={() => changeKind(item.value)}
                        >
                            {item.label}
                        </Button>
                    ))}
                </div>

                <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
                    {needsGuest && (
                        <Filter label="Container">
                            <Select
                                items={guestItems}
                                value={selectedVmid}
                                onValueChange={(value) => {
                                    if (value) {
                                        setVmid(value)
                                        setSource(allSources)
                                    }
                                }}
                            >
                                <SelectTrigger className="w-full" aria-label="Container">
                                    <SelectValue placeholder="No running containers" />
                                </SelectTrigger>
                                <SelectContent>
                                    {Object.entries(guestItems).map(([value, label]) => (
                                        <SelectItem key={value} value={value}>
                                            {label}
                                        </SelectItem>
                                    ))}
                                </SelectContent>
                            </Select>
                        </Filter>
                    )}
                    <Filter label="Level">
                        <Select items={levels} value={level} onValueChange={(value) => value && setLevel(value)}>
                            <SelectTrigger className="w-full" aria-label="Level">
                                <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                                {Object.entries(levels).map(([value, label]) => (
                                    <SelectItem key={value} value={value}>
                                        {label}
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                    </Filter>
                    <Filter label="Source">
                        <Select items={sourceItems} value={source} onValueChange={(value) => value && setSource(value)}>
                            <SelectTrigger className="w-full" aria-label="Source">
                                <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                                {Object.entries(sourceItems).map(([value, label]) => (
                                    <SelectItem key={value} value={value}>
                                        {label}
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                    </Filter>
                    <Filter label="Search">
                        <Input
                            type="search"
                            placeholder="Filter messages"
                            value={search}
                            onChange={(event) => setSearch(event.target.value)}
                            aria-label="Search"
                        />
                    </Filter>
                </div>
            </div>

            {logs.error ? (
                <Alert variant="destructive">
                    <TriangleAlertIcon />
                    <AlertTitle>Could not load the logs</AlertTitle>
                    <AlertDescription>{logs.error.message}</AlertDescription>
                </Alert>
            ) : (
                <LogList
                    entries={visible}
                    loading={logs.isPending}
                    onSelect={kind === 'tasks' ? (entry) => setOpenTask(entry as TaskEntry) : undefined}
                />
            )}

            <TaskLogDialog task={openTask} onClose={() => setOpenTask(null)} />
        </div>
    )
}

function Filter({ label, children }: { label: string; children: React.ReactNode }) {
    return (
        <div className="flex flex-col gap-2">
            <Label className="text-xs text-muted-foreground">{label}</Label>
            {children}
        </div>
    )
}
