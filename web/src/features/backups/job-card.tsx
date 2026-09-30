import { useState, type FormEvent } from 'react'
import { TriangleAlertIcon } from 'lucide-react'
import { toast } from 'sonner'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardAction, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import type { Backups } from '@/lib/api'
import { formatBytes } from '@/lib/format'
import { cn } from '@/lib/utils'
import { jobSettings, useSaveBackupJob } from './use-backups'

const weekdays = [
    { value: 'mon', label: 'Mon' },
    { value: 'tue', label: 'Tue' },
    { value: 'wed', label: 'Wed' },
    { value: 'thu', label: 'Thu' },
    { value: 'fri', label: 'Fri' },
    { value: 'sat', label: 'Sat' },
    { value: 'sun', label: 'Sun' },
]

function pad(value: number) {
    return String(value).padStart(2, '0')
}

export function JobCard({ backups }: { backups: Backups }) {
    const { job } = backups
    const save = useSaveBackupJob()
    const [enabled, setEnabled] = useState(job.enabled)
    const [days, setDays] = useState(job.days)
    const [time, setTime] = useState(`${pad(job.hour)}:${pad(job.minute)}`)
    const [storage, setStorage] = useState(job.storage)
    const [keepDaily, setKeepDaily] = useState(job.keepDaily)
    const [keepWeekly, setKeepWeekly] = useState(job.keepWeekly)
    const [keepMonthly, setKeepMonthly] = useState(job.keepMonthly)

    const storages = Object.fromEntries(
        backups.storages.map((item) => [
            item.name,
            item.total > 0 ? `${item.name} · ${formatBytes(item.total - item.used)} free` : item.name,
        ]),
    )

    function toggleDay(day: string) {
        setDays(days.includes(day) ? days.filter((item) => item !== day) : [...days, day])
    }

    function handleSubmit(event: FormEvent) {
        event.preventDefault()
        const [hour, minute] = time.split(':').map(Number)

        save.mutate(jobSettings(job, { enabled, days, hour, minute, storage, keepDaily, keepWeekly, keepMonthly }), {
            onSuccess: () => toast.success('Backup schedule saved'),
        })
    }

    return (
        <Card>
            <form onSubmit={handleSubmit} className="flex flex-col gap-6">
                <CardHeader>
                    <CardTitle>Schedule</CardTitle>
                    <CardDescription>
                        A Proxmox backup job with snapshot mode, so the guests keep running. New guests are included
                        automatically.
                    </CardDescription>
                    <CardAction>
                        <Switch checked={enabled} onCheckedChange={setEnabled} aria-label="Automatic backups" />
                    </CardAction>
                </CardHeader>
                <CardContent className="flex flex-col gap-5">
                    {job.custom && (
                        <Alert>
                            <TriangleAlertIcon />
                            <AlertDescription>
                                The schedule was changed in Proxmox to “{job.custom}”. Saving here replaces it.
                            </AlertDescription>
                        </Alert>
                    )}

                    <div className="flex flex-col gap-2">
                        <Label>Days</Label>
                        <div className="flex flex-wrap gap-1.5" role="group" aria-label="Days">
                            {weekdays.map((day) => (
                                <Button
                                    key={day.value}
                                    type="button"
                                    size="sm"
                                    variant={days.includes(day.value) ? 'default' : 'outline'}
                                    aria-pressed={days.includes(day.value)}
                                    disabled={!enabled}
                                    onClick={() => toggleDay(day.value)}
                                    className="w-12"
                                >
                                    {day.label}
                                </Button>
                            ))}
                        </div>
                        <p className="text-xs text-muted-foreground">
                            {days.length === 0 || days.length === 7 ? 'Every day.' : 'Only on the selected days.'}
                        </p>
                    </div>

                    <div className="grid gap-4 sm:grid-cols-2">
                        <div className="flex flex-col gap-2">
                            <Label htmlFor="backup-time">Time</Label>
                            <Input
                                id="backup-time"
                                type="time"
                                required
                                disabled={!enabled}
                                value={time}
                                onChange={(event) => setTime(event.target.value)}
                            />
                        </div>
                        <div className="flex flex-col gap-2">
                            <Label>Storage</Label>
                            <Select
                                items={storages}
                                value={storage}
                                onValueChange={(value) => value && setStorage(value)}
                            >
                                <SelectTrigger className="w-full" aria-label="Storage">
                                    <SelectValue placeholder="Choose a storage" />
                                </SelectTrigger>
                                <SelectContent>
                                    {Object.entries(storages).map(([value, label]) => (
                                        <SelectItem key={value} value={value}>
                                            {label}
                                        </SelectItem>
                                    ))}
                                </SelectContent>
                            </Select>
                        </div>
                    </div>

                    <div className="flex flex-col gap-2">
                        <Label>Keep</Label>
                        <div className="grid grid-cols-3 gap-3">
                            <KeepInput label="daily" value={keepDaily} onChange={setKeepDaily} />
                            <KeepInput label="weekly" value={keepWeekly} onChange={setKeepWeekly} />
                            <KeepInput label="monthly" value={keepMonthly} onChange={setKeepMonthly} />
                        </div>
                        <p className="text-xs text-muted-foreground">
                            {keepDaily + keepWeekly + keepMonthly === 0
                                ? 'All backups are kept. Watch the free space.'
                                : 'Older backups are removed after each scheduled backup.'}
                        </p>
                    </div>
                </CardContent>
                <CardFooter>
                    <Button type="submit" disabled={save.isPending || !storage}>
                        {job.exists ? 'Save schedule' : 'Create schedule'}
                    </Button>
                </CardFooter>
            </form>
        </Card>
    )
}

function KeepInput({ label, value, onChange }: { label: string; value: number; onChange: (value: number) => void }) {
    const id = `keep-${label}`

    return (
        <div className="flex flex-col gap-1">
            <Input
                id={id}
                type="number"
                min={0}
                max={1000}
                value={value}
                onChange={(event) => onChange(Math.max(0, Number(event.target.value) || 0))}
                className={cn('tabular-nums')}
            />
            <Label htmlFor={id} className="text-xs font-normal text-muted-foreground">
                {label}
            </Label>
        </div>
    )
}
