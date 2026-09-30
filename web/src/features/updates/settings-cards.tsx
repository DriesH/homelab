import { useState, type FormEvent } from 'react'
import { useMutation } from '@tanstack/react-query'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { api, type Updates } from '@/lib/api'
import { settingsFrom, useSaveUpdateSettings } from './use-updates'

const weekdays: Record<string, string> = {
    '1': 'Monday',
    '2': 'Tuesday',
    '3': 'Wednesday',
    '4': 'Thursday',
    '5': 'Friday',
    '6': 'Saturday',
    '0': 'Sunday',
}

function pad(value: number) {
    return String(value).padStart(2, '0')
}

export function ScheduleCard({ updates }: { updates: Updates }) {
    const save = useSaveUpdateSettings()
    const [enabled, setEnabled] = useState(updates.schedule.enabled)
    const [weekday, setWeekday] = useState(String(updates.schedule.weekday))
    const [time, setTime] = useState(`${pad(updates.schedule.hour)}:${pad(updates.schedule.minute)}`)

    function handleSubmit(event: FormEvent) {
        event.preventDefault()
        const [hour, minute] = time.split(':').map(Number)

        save.mutate(settingsFrom(updates, { schedule: { enabled, weekday: Number(weekday), hour, minute } }), {
            onSuccess: () => toast.success('Schedule saved'),
        })
    }

    return (
        <Card>
            <form onSubmit={handleSubmit} className="flex flex-col gap-6">
                <CardHeader>
                    <CardTitle>Schedule</CardTitle>
                    <CardDescription>
                        Once a week, containers with auto-update on get a snapshot and then their updates. The host is
                        only checked.
                    </CardDescription>
                </CardHeader>
                <CardContent className="flex flex-col gap-4">
                    <label className="flex items-center justify-between gap-4 text-sm font-medium">
                        Automatic updates
                        <Switch checked={enabled} onCheckedChange={setEnabled} />
                    </label>
                    <div className="grid grid-cols-2 gap-4">
                        <div className="flex flex-col gap-2">
                            <Label>Day</Label>
                            <Select
                                items={weekdays}
                                value={weekday}
                                onValueChange={(value) => value && setWeekday(value)}
                            >
                                <SelectTrigger className="w-full" disabled={!enabled}>
                                    <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                    {Object.entries(weekdays).map(([value, label]) => (
                                        <SelectItem key={value} value={value}>
                                            {label}
                                        </SelectItem>
                                    ))}
                                </SelectContent>
                            </Select>
                        </div>
                        <div className="flex flex-col gap-2">
                            <Label htmlFor="schedule-time">Time</Label>
                            <Input
                                id="schedule-time"
                                type="time"
                                required
                                disabled={!enabled}
                                value={time}
                                onChange={(event) => setTime(event.target.value)}
                            />
                        </div>
                    </div>
                </CardContent>
                <CardFooter>
                    <Button type="submit" disabled={save.isPending}>
                        Save schedule
                    </Button>
                </CardFooter>
            </form>
        </Card>
    )
}

export function TelegramCard({ updates }: { updates: Updates }) {
    const save = useSaveUpdateSettings()
    const [botToken, setBotToken] = useState('')
    const [chatId, setChatId] = useState(updates.telegram.chatId)

    const test = useMutation({
        mutationFn: api.testNotification,
        onSuccess: () => toast.success('Test message sent'),
        onError: (error) => toast.error(error.message),
    })

    function handleSubmit(event: FormEvent) {
        event.preventDefault()

        save.mutate(settingsFrom(updates, { telegram: { botToken, chatId } }), {
            onSuccess() {
                setBotToken('')
                toast.success(chatId ? 'Telegram saved' : 'Telegram turned off')
            },
        })
    }

    return (
        <Card>
            <form onSubmit={handleSubmit} className="flex flex-col gap-6">
                <CardHeader>
                    <CardTitle>Telegram</CardTitle>
                    <CardDescription>
                        Get a message after each update. Create a bot with @BotFather, send it a message, and get your
                        chat ID from @userinfobot.
                    </CardDescription>
                </CardHeader>
                <CardContent className="flex flex-col gap-4">
                    <div className="flex flex-col gap-2">
                        <Label htmlFor="bot-token">Bot token</Label>
                        <Input
                            id="bot-token"
                            type="password"
                            autoComplete="off"
                            placeholder={updates.telegram.configured ? 'Saved. Type to replace it.' : '123456:ABC-DEF…'}
                            value={botToken}
                            onChange={(event) => setBotToken(event.target.value)}
                        />
                    </div>
                    <div className="flex flex-col gap-2">
                        <Label htmlFor="chat-id">Chat ID</Label>
                        <Input
                            id="chat-id"
                            placeholder="Leave empty to turn Telegram off"
                            value={chatId}
                            onChange={(event) => setChatId(event.target.value)}
                        />
                    </div>
                </CardContent>
                <CardFooter className="gap-2">
                    <Button type="submit" disabled={save.isPending}>
                        Save
                    </Button>
                    <Button
                        type="button"
                        variant="outline"
                        disabled={!updates.telegram.configured || test.isPending}
                        onClick={() => test.mutate()}
                    >
                        Send test
                    </Button>
                </CardFooter>
            </form>
        </Card>
    )
}
