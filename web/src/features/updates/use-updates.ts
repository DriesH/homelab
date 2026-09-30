import { useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'

import { api, type UpdateSettings, type Updates } from '@/lib/api'
import { updatesQuery } from '@/lib/queries'

// useStartUpdate runs one of the background operations and refreshes the page data.
export function useStartUpdate() {
    const queryClient = useQueryClient()

    return useMutation({
        mutationFn: (start: () => Promise<void>) => start(),
        onSuccess() {
            queryClient.invalidateQueries({ queryKey: updatesQuery.queryKey })
        },
        onError(error) {
            toast.error(error.message)
        },
    })
}

export function useSaveUpdateSettings() {
    const queryClient = useQueryClient()

    return useMutation({
        mutationFn: api.saveUpdateSettings,
        onSuccess() {
            queryClient.invalidateQueries({ queryKey: updatesQuery.queryKey })
        },
        onError(error) {
            toast.error(error.message)
        },
    })
}

// settingsFrom builds a full settings object from the current data, so one card
// can change its part without touching the others. An empty bot token keeps the saved one.
export function settingsFrom(updates: Updates, changes: Partial<UpdateSettings> = {}): UpdateSettings {
    return {
        schedule: updates.schedule,
        excluded: updates.guests.filter((guest) => !guest.autoUpdate && !guest.self).map((guest) => guest.vmid),
        telegram: { botToken: '', chatId: updates.telegram.chatId },
        ...changes,
    }
}
