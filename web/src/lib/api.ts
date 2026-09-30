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

export type JellyfinNowPlaying = {
    user: string
    device: string
    itemId: string
    title: string
    subtitle: string
    progress: number
    paused: boolean
    transcode: boolean
    playMethod: string
    reasons: string[]
}

export type Jellyfin = {
    configured: boolean
    url: string
    error?: string
    serverName?: string
    version?: string
    themeEnabled: boolean
    movies: number
    series: number
    episodes: number
    nowPlaying: JellyfinNowPlaying[]
    recent: { id: string; name: string; type: string; year?: number }[]
}

export type UpdateSettings = {
    schedule: Schedule
    excluded: number[]
    telegram: { botToken: string; chatId: string }
}

export type CheckKind = 'http' | 'tcp'

export type CheckInput = { name: string; kind: CheckKind; target: string }

export type ServiceCheck = CheckInput & {
    id: string
    status: 'pending' | 'up' | 'down'
    latencyMs: number
    error?: string
    checkedAt?: string
    since?: string
}

export type Disk = {
    node: string
    devPath: string
    model: string
    serial: string
    size: number
    type: string
    used: string
    health: string
    wearout: number | null
    problem?: string
}

export type ZFSPool = {
    node: string
    name: string
    health: string
    size: number
    alloc: number
    frag: number
    problem?: string
}

export type Storage = {
    node: string
    name: string
    status: string
    used: number
    total: number
    problem?: string
}

export type Share = {
    path: string
    source: string
    fsType: string
    mounted: boolean
    size: number
    used: number
    error?: string
    problem?: string
}

export type Health = {
    services: ServiceCheck[]
    disks: Disk[]
    pools: ZFSPool[]
    storage: Storage[]
    shares: Share[]
    errors: string[]
    checkedAt?: string
}

export type UpgradeStatus = {
    state: 'running' | 'succeeded' | 'failed'
    version?: string
    message?: string
    startedAt?: string
    finishedAt?: string
    log?: string
}

export type SelfUpdate = {
    version: string
    repo: string
    tokenSet: boolean
    autoInstall: boolean
    latest: { version: string; notes: string; url: string; publishedAt: string } | null
    updateAvailable: boolean
    checkedAt?: string
    error?: string
    installing: boolean
    upgrade: UpgradeStatus | null
}

export type SelfUpdateSettings = { repo: string; token: string; clearToken: boolean; autoInstall: boolean }

export type TailscaleSettings = { shareSubnet: boolean; subnet: string }

export type Tailscale = {
    installed: boolean
    state: string
    authUrl?: string
    connecting: boolean
    error?: string
    health: string[]
    name?: string
    dnsName?: string
    ips: string[]
    tailnet?: string
    serving: boolean
    serveUrl?: string
    settings: TailscaleSettings
    suggestedSubnet?: string
    subnetApproved: boolean
    peers: { name: string; dnsName: string; os: string; ips: string[]; online: boolean; lastSeen?: string }[]
}

export type BackupJob = {
    enabled: boolean
    days: string[]
    hour: number
    minute: number
    storage: string
    exclude: number[]
    keepDaily: number
    keepWeekly: number
    keepMonthly: number
}

export type GuestBackup = {
    volid: string
    storage: string
    createdAt: string
    size: number
    notes: string
    protected: boolean
}

export type BackupRun = {
    id: string
    kind: 'backup' | 'restore' | 'delete'
    vmid: number
    target: string
    succeeded: boolean
    message: string
    startedAt: string
    finishedAt: string
}

export type Backups = {
    busy: string
    job: BackupJob & { exists: boolean; nextRun?: string; custom?: string }
    storages: { node: string; name: string; type: string; total: number; used: number }[]
    guests: {
        vmid: number
        name: string
        type: 'lxc' | 'qemu'
        status: string
        included: boolean
        self: boolean
        backups: GuestBackup[]
    }[]
    history: BackupRun[]
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
    jellyfin: () => request<Jellyfin>('GET', '/jellyfin'),
    saveJellyfinSettings: (settings: { url: string; apiKey: string }) =>
        request<void>('PUT', '/jellyfin/settings', settings),
    setJellyfinTheme: (enabled: boolean) => request<void>('PUT', '/jellyfin/theme', { enabled }),
    selfUpdate: () => request<SelfUpdate>('GET', '/self-update'),
    checkSelfUpdate: () => request<SelfUpdate>('POST', '/self-update/check'),
    installSelfUpdate: () => request<void>('POST', '/self-update/install'),
    saveSelfUpdateSettings: (settings: SelfUpdateSettings) => request<void>('PUT', '/self-update/settings', settings),
    backups: () => request<Backups>('GET', '/backups'),
    saveBackupJob: (job: BackupJob) => request<void>('PUT', '/backups/job', job),
    backUpGuest: (vmid: number) => request<void>('POST', `/backups/guests/${vmid}`),
    restoreBackup: (vmid: number, volid: string) => request<void>('POST', '/backups/restore', { vmid, volid }),
    deleteBackup: (volid: string) => request<void>('POST', '/backups/delete', { volid }),
    tailscale: () => request<Tailscale>('GET', '/tailscale'),
    connectTailscale: (authKey: string) => request<void>('POST', '/tailscale/connect', { authKey }),
    logoutTailscale: () => request<void>('POST', '/tailscale/logout'),
    setTailscaleServe: (enabled: boolean) => request<void>('PUT', '/tailscale/serve', { enabled }),
    saveTailscaleSettings: (settings: TailscaleSettings) => request<void>('PUT', '/tailscale/settings', settings),
    health: () => request<Health>('GET', '/health'),
    refreshHealth: () => request<Health>('POST', '/health/refresh'),
    addCheck: (input: CheckInput) => request<ServiceCheck>('POST', '/health/checks', input),
    updateCheck: (id: string, input: CheckInput) => request<void>('PUT', `/health/checks/${id}`, input),
    deleteCheck: (id: string) => request<void>('DELETE', `/health/checks/${id}`),
}

export function jellyfinImage(itemId: string, type: 'Primary' | 'Backdrop') {
    return `/api/jellyfin/items/${itemId}/image?type=${type}`
}
