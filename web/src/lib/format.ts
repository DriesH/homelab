const units = ['B', 'KB', 'MB', 'GB', 'TB']

export function formatBytes(bytes: number) {
    if (bytes <= 0) {
        return '0 B'
    }

    const exponent = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
    const value = bytes / 1024 ** exponent

    return `${value.toFixed(value < 10 && exponent > 0 ? 1 : 0)} ${units[exponent]}`
}

export function formatUptime(seconds: number) {
    if (seconds <= 0) {
        return '—'
    }

    const days = Math.floor(seconds / 86400)
    const hours = Math.floor((seconds % 86400) / 3600)
    const minutes = Math.floor((seconds % 3600) / 60)

    if (days > 0) {
        return `${days}d ${hours}h`
    }
    if (hours > 0) {
        return `${hours}h ${minutes}m`
    }

    return `${minutes}m`
}

export function percentage(used: number, total: number) {
    if (total <= 0) {
        return 0
    }

    return Math.min(100, Math.round((used / total) * 100))
}

const relativeTime = new Intl.RelativeTimeFormat('en', { numeric: 'auto' })

export function formatRelative(date: string | Date) {
    const seconds = Math.round((new Date(date).getTime() - Date.now()) / 1000)
    const units: [Intl.RelativeTimeFormatUnit, number][] = [
        ['day', 86400],
        ['hour', 3600],
        ['minute', 60],
    ]

    for (const [unit, size] of units) {
        if (Math.abs(seconds) >= size) {
            return relativeTime.format(Math.round(seconds / size), unit)
        }
    }

    return 'just now'
}

export function formatDateTime(date: string | Date) {
    return new Date(date).toLocaleString(undefined, {
        weekday: 'long',
        hour: '2-digit',
        minute: '2-digit',
    })
}
