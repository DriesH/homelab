import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'

import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import type { Timeframe, UsagePoint, UsageTarget } from '@/lib/api'
import { formatBytes } from '@/lib/format'
import { usageQuery } from '@/lib/queries'

const timeframes: Record<Timeframe, string> = {
    hour: 'Last hour',
    day: 'Last day',
    week: 'Last week',
    month: 'Last month',
    year: 'Last year',
}

const tickFormats: Record<Timeframe, Intl.DateTimeFormatOptions> = {
    hour: { hour: '2-digit', minute: '2-digit' },
    day: { hour: '2-digit', minute: '2-digit' },
    week: { weekday: 'short', day: 'numeric' },
    month: { day: 'numeric', month: 'short' },
    year: { month: 'short' },
}

type Series = { key: Exclude<keyof UsagePoint, 'time'>; label: string; color: string }

type ChartProps = {
    title: string
    points: UsagePoint[]
    series: Series[]
    timeframe: Timeframe
    format: (value: number) => string
    max?: number
    syncId: string
}

const percent = (value: number) => `${Math.round(value * 100)}%`
const bytesPerSecond = (value: number) => `${formatBytes(value)}/s`

export function UsageCharts({ target }: { target: UsageTarget }) {
    const [timeframe, setTimeframe] = useState<Timeframe>('hour')
    const { data: points, error, isPending } = useQuery(usageQuery(target, timeframe))

    const maxMem = points?.findLast((point) => point.maxMem !== null)?.maxMem ?? undefined
    const hasData = points?.some((point) => point.cpu !== null)
    const syncId = `usage-${target.node}-${target.vmid ?? 'node'}`

    return (
        <div className="@container flex flex-col gap-3">
            <div className="flex items-center justify-between gap-2">
                <h3 className="text-sm font-medium">History</h3>
                <Select items={timeframes} value={timeframe} onValueChange={(value) => value && setTimeframe(value)}>
                    <SelectTrigger size="sm" aria-label="Timeframe">
                        <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                        {Object.entries(timeframes).map(([value, label]) => (
                            <SelectItem key={value} value={value}>
                                {label}
                            </SelectItem>
                        ))}
                    </SelectContent>
                </Select>
            </div>

            {isPending ? (
                <Skeleton className="h-40 w-full" />
            ) : error ? (
                <p className="text-sm text-destructive">{error.message}</p>
            ) : !hasData ? (
                <p className="text-sm text-muted-foreground">No data for this timeframe.</p>
            ) : (
                <div className="grid gap-6 @3xl:grid-cols-3">
                    <UsageChart
                        title="CPU"
                        points={points}
                        series={[{ key: 'cpu', label: 'CPU', color: 'var(--chart-1)' }]}
                        timeframe={timeframe}
                        syncId={syncId}
                        format={percent}
                    />
                    <UsageChart
                        title="Memory"
                        points={points}
                        series={[{ key: 'mem', label: 'Used', color: 'var(--chart-1)' }]}
                        timeframe={timeframe}
                        syncId={syncId}
                        format={formatBytes}
                        max={maxMem}
                    />
                    <UsageChart
                        title="Network"
                        points={points}
                        series={[
                            { key: 'netIn', label: 'In', color: 'var(--chart-1)' },
                            { key: 'netOut', label: 'Out', color: 'var(--chart-2)' },
                        ]}
                        timeframe={timeframe}
                        syncId={syncId}
                        format={bytesPerSecond}
                    />
                </div>
            )}
        </div>
    )
}

function UsageChart({ title, points, series, timeframe, format, max, syncId }: ChartProps) {
    const latest = points.findLast((point) => point[series[0].key] !== null)
    const ticks = calendarTicks(points, timeframe)

    return (
        <figure className="flex min-w-0 flex-col gap-2">
            <figcaption className="flex flex-wrap items-baseline justify-between gap-x-3 text-sm">
                <span className="text-muted-foreground">{title}</span>
                {latest && (
                    <span className="flex gap-3 tabular-nums">
                        {series.map((item) => (
                            <span key={item.key} className="flex items-center gap-1.5">
                                {series.length > 1 && (
                                    <>
                                        <span className="h-0.5 w-3 rounded-full" style={{ background: item.color }} />
                                        <span className="text-muted-foreground">{item.label}</span>
                                    </>
                                )}
                                {format(latest[item.key] ?? 0)}
                            </span>
                        ))}
                    </span>
                )}
            </figcaption>

            <ResponsiveContainer width="100%" height={120}>
                <AreaChart
                    data={points}
                    margin={{ top: 4, right: 4, bottom: 0, left: 0 }}
                    title={title}
                    syncId={syncId}
                >
                    <CartesianGrid vertical={false} stroke="var(--border)" />
                    <XAxis
                        dataKey="time"
                        type="number"
                        scale="time"
                        domain={['dataMin', 'dataMax']}
                        ticks={ticks}
                        interval={ticks ? 'equidistantPreserveStart' : 'preserveEnd'}
                        tickFormatter={(time: number) => formatTime(time, tickFormats[timeframe])}
                        tick={{ fontSize: 11, fill: 'var(--muted-foreground)' }}
                        tickLine={false}
                        axisLine={false}
                        minTickGap={24}
                    />
                    <YAxis
                        domain={[0, max ?? 'auto']}
                        ticks={max ? [0, max / 2, max] : undefined}
                        // A normal space lets Recharts wrap the label over two lines.
                        tickFormatter={(value: number) => format(value).replace(' ', '\u00a0')}
                        tick={{ fontSize: 11, fill: 'var(--muted-foreground)' }}
                        tickLine={false}
                        axisLine={false}
                        tickCount={3}
                        width={64}
                    />
                    <Tooltip
                        isAnimationActive={false}
                        cursor={{ stroke: 'var(--muted-foreground)', strokeWidth: 1 }}
                        content={({ active, payload }) => {
                            const point = payload?.[0]?.payload as UsagePoint | undefined
                            if (!active || !point) {
                                return null
                            }

                            return (
                                <div className="rounded-lg bg-popover px-2.5 py-1.5 text-xs text-popover-foreground shadow-md ring-1 ring-foreground/10">
                                    <p className="text-muted-foreground">
                                        {formatTime(point.time, {
                                            weekday: 'short',
                                            day: 'numeric',
                                            month: 'short',
                                            hour: '2-digit',
                                            minute: '2-digit',
                                        })}
                                    </p>
                                    {series.map((item) => (
                                        <p key={item.key} className="flex items-center gap-1.5 tabular-nums">
                                            <span
                                                className="h-0.5 w-3 rounded-full"
                                                style={{ background: item.color }}
                                            />
                                            {item.label} {point[item.key] === null ? '—' : format(point[item.key] ?? 0)}
                                        </p>
                                    ))}
                                </div>
                            )
                        }}
                    />
                    {series.map((item) => (
                        <Area
                            key={item.key}
                            dataKey={item.key}
                            name={item.label}
                            type="monotone"
                            stroke={item.color}
                            strokeWidth={2}
                            fill={item.color}
                            fillOpacity={0.1}
                            dot={false}
                            activeDot={{ r: 4, strokeWidth: 2, stroke: 'var(--card)' }}
                            isAnimationActive={false}
                        />
                    ))}
                </AreaChart>
            </ResponsiveContainer>
        </figure>
    )
}

// calendarTicks puts the ticks of the longer timeframes at the start of a day or month, so labels don't repeat.
function calendarTicks(points: UsagePoint[], timeframe: Timeframe) {
    if (timeframe === 'hour' || timeframe === 'day' || points.length === 0) {
        return undefined
    }

    const first = points[0].time * 1000
    const last = points[points.length - 1].time * 1000
    const tick = new Date(first)
    tick.setHours(0, 0, 0, 0)
    if (timeframe === 'year') {
        tick.setDate(1)
    }

    const ticks = []
    while (tick.getTime() <= last) {
        if (tick.getTime() >= first) {
            ticks.push(tick.getTime() / 1000)
        }
        if (timeframe === 'year') {
            tick.setMonth(tick.getMonth() + 1)
        } else {
            tick.setDate(tick.getDate() + (timeframe === 'month' ? 7 : 1))
        }
    }

    return ticks
}

function formatTime(seconds: number, options: Intl.DateTimeFormatOptions) {
    return new Date(seconds * 1000).toLocaleString(undefined, options)
}
