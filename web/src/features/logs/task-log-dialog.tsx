import { useQuery } from '@tanstack/react-query'

import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Skeleton } from '@/components/ui/skeleton'
import { api, type TaskEntry } from '@/lib/api'

export function TaskLogDialog({ task, onClose }: { task: TaskEntry | null; onClose: () => void }) {
    return (
        <Dialog open={task !== null} onOpenChange={(open) => !open && onClose()}>
            <DialogContent className="sm:max-w-3xl">{task && <TaskLog task={task} />}</DialogContent>
        </Dialog>
    )
}

function TaskLog({ task }: { task: TaskEntry }) {
    const { data, error, isPending } = useQuery({
        queryKey: ['logs', 'task', task.node, task.upid],
        queryFn: () => api.taskLog(task.node, task.upid),
    })

    return (
        <>
            <DialogHeader>
                <DialogTitle>{task.message}</DialogTitle>
                <DialogDescription>Task on {task.node}</DialogDescription>
            </DialogHeader>
            {isPending ? (
                <Skeleton className="h-64 w-full" />
            ) : (
                <pre className="max-h-[60vh] overflow-auto rounded-md bg-muted p-3 font-mono text-xs whitespace-pre-wrap">
                    {error ? error.message : data.lines.join('\n')}
                </pre>
            )}
        </>
    )
}
