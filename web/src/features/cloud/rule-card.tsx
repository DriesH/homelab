import { useState, type FormEvent } from 'react'
import { Loader2Icon } from 'lucide-react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Field } from '@/features/apps/form-parts'
import { api, type CloudRule } from '@/lib/api'
import { useCloudMutation } from './use-cloud'

const pad = (value: number) => String(value).padStart(2, '0')

export function RuleCard({ rule }: { rule: CloudRule }) {
    const [minAgeDays, setMinAgeDays] = useState(String(rule.minAgeDays))
    const [targetPercent, setTargetPercent] = useState(String(rule.targetPercent))
    const [minSizeMb, setMinSizeMb] = useState(String(rule.minSizeMb))
    const [time, setTime] = useState(`${pad(rule.hour)}:${pad(rule.minute)}`)

    const save = useCloudMutation(
        () => {
            const [hour, minute] = time.split(':').map(Number)
            return api.saveCloudRule({
                minAgeDays: Number(minAgeDays),
                targetPercent: Number(targetPercent),
                minSizeMb: Number(minSizeMb),
                hour,
                minute,
            })
        },
        'Could not save the rule',
        () => toast.success('Rule saved'),
    )

    function handleSubmit(event: FormEvent) {
        event.preventDefault()
        save.mutate()
    }

    return (
        <Card>
            <form onSubmit={handleSubmit} className="flex flex-col gap-6">
                <CardHeader>
                    <CardTitle>What moves to the cloud</CardTitle>
                    <CardDescription>
                        Each night, Homelab moves the oldest video files to the cloud until the local media is below the
                        target. Subtitles, posters and new files stay local.
                    </CardDescription>
                </CardHeader>
                <CardContent className="grid gap-4 sm:grid-cols-2">
                    <Field id="rule-target" label="Keep the local media below" help="Percent of the disk or share.">
                        <Input
                            id="rule-target"
                            type="number"
                            min={50}
                            max={95}
                            value={targetPercent}
                            onChange={(event) => setTargetPercent(event.target.value)}
                            required
                        />
                    </Field>
                    <Field id="rule-age" label="Only files older than" help="Days. At least 7.">
                        <Input
                            id="rule-age"
                            type="number"
                            min={7}
                            max={3650}
                            value={minAgeDays}
                            onChange={(event) => setMinAgeDays(event.target.value)}
                            required
                        />
                    </Field>
                    <Field id="rule-size" label="Only files bigger than" help="MB. At least 10.">
                        <Input
                            id="rule-size"
                            type="number"
                            min={10}
                            max={100000}
                            value={minSizeMb}
                            onChange={(event) => setMinSizeMb(event.target.value)}
                            required
                        />
                    </Field>
                    <Field id="rule-time" label="Time">
                        <Input
                            id="rule-time"
                            type="time"
                            value={time}
                            onChange={(event) => setTime(event.target.value)}
                            required
                        />
                    </Field>
                </CardContent>
                <CardFooter>
                    <Button type="submit" disabled={save.isPending}>
                        {save.isPending && <Loader2Icon className="animate-spin" />}
                        Save rule
                    </Button>
                </CardFooter>
            </form>
        </Card>
    )
}
