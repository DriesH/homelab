import { useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'

import { cloudQuery } from '@/lib/queries'

// useCloudMutation runs a cloud action, then loads the cloud storage again.
export function useCloudMutation<T>(action: () => Promise<T>, errorTitle: string, onSuccess?: (result: T) => void) {
    const queryClient = useQueryClient()

    return useMutation({
        mutationFn: action,
        onSuccess: (result) => {
            queryClient.invalidateQueries({ queryKey: cloudQuery.queryKey })
            onSuccess?.(result)
        },
        onError: (error) => toast.error(errorTitle, { description: error.message }),
    })
}
