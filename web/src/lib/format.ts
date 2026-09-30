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
