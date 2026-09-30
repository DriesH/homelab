export class ApiError extends Error {
    status: number

    constructor(status: number, message: string) {
        super(message)
        this.status = status
    }
}

export type Usage = {
    cpu: number
    maxCpu: number
    mem: number
    maxMem: number
    disk: number
    maxDisk: number
    uptime: number
}

export type ProxmoxNode = Usage & {
    name: string
    status: string
    pveVersion: string
}

export type Guest = Usage & {
    id: string
    vmid: number
    type: 'lxc' | 'qemu'
    node: string
    name: string
    status: string
}

export type Overview = {
    nodes: ProxmoxNode[]
    guests: Guest[]
    agent: { connected: boolean; hostname?: string; version?: string }
}

export type Session = { username: string }

export type GuestAction = 'start' | 'shutdown' | 'reboot' | 'stop'

export type Package = { name: string; from: string; to: string }

export type UpdateTarget = {
    vmid?: number
    name: string
    checkedAt?: string
    packages: Package[]
    images: { service: string; image: string }[]
    unsupported?: boolean
    rebootRequired?: boolean
    error?: string
}

export type GuestUpdates = UpdateTarget & {
    vmid: number
    status: string
    autoUpdate: boolean
    self: boolean
}

export type Schedule = { enabled: boolean; weekday: number; hour: number; minute: number }

export type UpdateRun = {
    id: string
    kind: 'check' | 'host-update' | 'guest-update'
    target: string
    vmid?: number
    scheduled: boolean
    status: 'succeeded' | 'failed' | 'rolled-back'
    snapshot?: string
    message: string
    log?: string
    startedAt: string
    finishedAt: string
}

export type Updates = {
    busy: string
    schedule: Schedule
    nextRun: string | null
    telegram: { configured: boolean; chatId: string }
    host: UpdateTarget
    guests: GuestUpdates[]
    history: UpdateRun[]
}

export type UpdateSettings = {
    schedule: Schedule
    excluded: number[]
    telegram: { botToken: string; chatId: string }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const response = await fetch(`/api${path}`, {
        method,
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', 'X-Homelab-Request': '1' },
        body: body === undefined ? undefined : JSON.stringify(body),
    })

    if (response.status === 204) {
        return undefined as T
    }

    const data = await response.json().catch(() => ({}))

    if (!response.ok) {
        throw new ApiError(response.status, data.error ?? response.statusText)
    }

    return data as T
}

export const api = {
    me: () => request<Session>('GET', '/auth/me'),
    login: (credentials: { username: string; password: string; code: string }) =>
        request<Session>('POST', '/auth/login', credentials),
    logout: () => request<void>('POST', '/auth/logout'),
    overview: () => request<Overview>('GET', '/overview'),
    guestAction: (guest: Guest, action: GuestAction) =>
        request<{ task: string }>('POST', `/guests/${guest.node}/${guest.type}/${guest.vmid}/${action}`),
    updates: () => request<Updates>('GET', '/updates'),
    updateRun: (id: string) => request<UpdateRun>('GET', `/updates/runs/${id}`),
    checkUpdates: () => request<void>('POST', '/updates/check'),
    updateHost: () => request<void>('POST', '/updates/host'),
    updateGuest: (vmid: number) => request<void>('POST', `/updates/guests/${vmid}`),
    saveUpdateSettings: (settings: UpdateSettings) => request<void>('PUT', '/updates/settings', settings),
    testNotification: () => request<void>('POST', '/updates/test-notification'),
}
