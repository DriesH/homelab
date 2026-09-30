import { useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'

import { api, type BackupJob } from '@/lib/api'
import { backupsQuery } from '@/lib/queries'

export function useSaveBackupJob() {
    const queryClient = useQueryClient()

    return useMutation({
        mutationFn: api.saveBackupJob,
        onSuccess: () => queryClient.invalidateQueries({ queryKey: backupsQuery.queryKey }),
        onError: (error) => toast.error(error.message),
    })
}

// useBackupAction starts a backup, restore or delete, which runs in the background.
export function useBackupAction() {
    const queryClient = useQueryClient()

    return useMutation({
        mutationFn: (start: () => Promise<void>) => start(),
        onSuccess: () => queryClient.invalidateQueries({ queryKey: backupsQuery.queryKey }),
        onError: (error) => toast.error(error.message),
    })
}

// jobSettings keeps only the fields the server accepts.
export function jobSettings(job: BackupJob, changes: Partial<BackupJob> = {}): BackupJob {
    return {
        enabled: job.enabled,
        days: job.days,
        hour: job.hour,
        minute: job.minute,
        storage: job.storage,
        exclude: job.exclude,
        keepDaily: job.keepDaily,
        keepWeekly: job.keepWeekly,
        keepMonthly: job.keepMonthly,
        ...changes,
    }
}
