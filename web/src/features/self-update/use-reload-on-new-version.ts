import { useEffect, useRef } from 'react'
import { useQuery } from '@tanstack/react-query'

import { selfUpdateQuery } from '@/lib/queries'

// Reloads the page when the server runs a new version, so the browser gets the new app.
export function useReloadOnNewVersion() {
    const { data } = useQuery(selfUpdateQuery)
    const firstVersion = useRef<string | null>(null)

    useEffect(() => {
        if (!data) {
            return
        }
        if (firstVersion.current === null) {
            firstVersion.current = data.version
        } else if (data.version !== firstVersion.current) {
            window.location.reload()
        }
    }, [data])
}
