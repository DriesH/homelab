import { useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'

import { tailscaleQuery } from '@/lib/queries'

export function useTailscaleMutation<T>(mutationFn: (input: T) => Promise<void>, success?: (input: T) => string) {
    const queryClient = useQueryClient()

    return useMutation({
        mutationFn,
        onSuccess(_, input) {
            if (success) {
                toast.success(success(input))
            }
            queryClient.invalidateQueries({ queryKey: tailscaleQuery.queryKey })
        },
        onError: (error) => toast.error(error.message),
    })
}
