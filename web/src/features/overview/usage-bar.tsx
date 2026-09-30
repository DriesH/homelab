import { Progress, ProgressLabel } from '@/components/ui/progress'
import { cn } from '@/lib/utils'

type UsageBarProps = {
    label: string
    value: number
    detail: string
}

export function UsageBar({ label, value, detail }: UsageBarProps) {
    return (
        <Progress value={value} className={cn(value >= 90 && '[&_[data-slot=progress-indicator]]:bg-destructive')}>
            <ProgressLabel className="text-muted-foreground">{label}</ProgressLabel>
            <span className="ml-auto text-sm tabular-nums">{detail}</span>
        </Progress>
    )
}
