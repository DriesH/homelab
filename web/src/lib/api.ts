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

export type Timeframe = 'hour' | 'day' | 'week' | 'month' | 'year'

// One sample of the Proxmox statistics. A value is null when Proxmox has no data, like when a guest was stopped.
export type UsagePoint = {
    time: number
    cpu: number | null
    mem: number | null
    maxMem: number | null
    netIn: number | null
    netOut: number | null
}

export type UsageTarget = { node: string; type?: Guest['type']; vmid?: number }

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

export type TailscaleSettings = { shareSubnet: boolean; subnet: string; useTag?: boolean }

export type TailscaleService = { name: string; title: string; published: boolean; url?: string }

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
    tags: string[]
    services: TailscaleService[]
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

export type LogEntry = { time: string; level: number; source: string; message: string }

export type TaskEntry = LogEntry & { node: string; upid: string }

export type DockerLogs = {
    containers: { name: string; state: string; image: string }[]
    entries: LogEntry[]
}

export type AppInstall = {
    app?: string
    state: 'idle' | 'running' | 'succeeded' | 'failed'
    message?: string
    startedAt?: string
    finishedAt?: string
    vmid?: number
    ip?: string
    log?: string
}

export type JellyfinAnswers = {
    nasServer: string
    nasExport: string
    mediaFolder: string
    moviesFolder: string
    seriesFolder: string
    adminUsername: string
    adminPassword: string
    theme: boolean
    storage: string
}

export type SavedAnswers = {
    answers: MediaStackAnswers
    jellyfin: JellyfinAnswers
    hasJellyfinApiKey: boolean
    hasJellyfinAdminPassword: boolean
    hasOpenSubtitlesPassword: boolean
    until: string
}

export type CatalogApp = {
    id: string
    name: string
    description: string
    links: { name: string; description: string; url?: string }[]
    installed: boolean
    hostUrl?: string
    vmid?: number
    status?: string
    install: AppInstall | null
    saved: SavedAnswers | null
}

export type Apps = {
    apps: CatalogApp[]
    defaults: {
        storages: string[]
        storage: string
        jellyfinVmid?: number
        mediaShare?: string
        mediaFolders: { storage: string; path: string }[]
        moviesFolder: string
        seriesFolder: string
        vpnCountries: string
        subtitleLanguages: string
        username: string
        downloadsSize: number
    }
    error?: string
}

export type MediaStackAnswers = {
    nasServer: string
    nasExport: string
    mediaFolder: string
    moviesFolder: string
    seriesFolder: string
    wireguardPrivateKey: string
    vpnCountries: string
    subtitleLanguages: string
    username: string
    password: string
    jellyfinApiKey: string
    jellyfinAdminUsername: string
    jellyfinAdminPassword: string
    openSubtitlesUsername: string
    openSubtitlesPassword: string
    restartJellyfin: boolean
    storage: string
    downloadsSize: number
}

export type SettingsChange = {
    section: 'selfUpdate' | 'notifications' | 'updates' | 'health' | 'backups' | 'tailscale' | 'jellyfin'
    status: 'unchanged' | 'changed' | 'applied' | 'skipped' | 'failed'
    message?: string
}

export type SettingsImport = { applied: boolean; changes: SettingsChange[] }

async function fail(response: Response): Promise<never> {
    const data = await response.json().catch(() => ({}))
    throw new ApiError(response.status, data.error ?? response.statusText)
}

// downloadDataBackup returns the encrypted backup and its file name.
async function downloadDataBackup(password: string, passphrase: string) {
    const response = await fetch('/api/data-backup/download', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', 'X-Homelab-Request': '1' },
        body: JSON.stringify({ password, passphrase }),
    })
    if (!response.ok) {
        return fail(response)
    }

    const name = /filename="([^"]+)"/.exec(response.headers.get('Content-Disposition') ?? '')?.[1]
    return { blob: await response.blob(), name: name ?? 'homelab-data.hlbackup' }
}

async function restoreDataBackup(file: File, password: string, passphrase: string) {
    const form = new FormData()
    form.append('password', password)
    form.append('passphrase', passphrase)
    form.append('file', file)

    const response = await fetch('/api/data-backup/restore', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'X-Homelab-Request': '1' },
        body: form,
    })
    if (!response.ok) {
        return fail(response)
    }
}

// A string body is sent as it is, for example YAML. Anything else is sent as JSON.
async function request<T>(method: string, path: string, body?: unknown, contentType = 'application/json'): Promise<T> {
    const response = await fetch(`/api${path}`, {
        method,
        credentials: 'same-origin',
        headers: { 'Content-Type': contentType, 'X-Homelab-Request': '1' },
        body: body === undefined ? undefined : typeof body === 'string' ? body : JSON.stringify(body),
    })

    if (response.status === 204) {
        return undefined as T
    }

    if (!response.ok) {
        return fail(response)
    }

    return (await response.json().catch(() => ({}))) as T
}

export const api = {
    me: () => request<Session>('GET', '/auth/me'),
    login: (credentials: { username: string; password: string; code: string }) =>
        request<Session>('POST', '/auth/login', credentials),
    logout: () => request<void>('POST', '/auth/logout'),
    overview: () => request<Overview>('GET', '/overview'),
    guestAction: (guest: Guest, action: GuestAction) =>
        request<{ task: string }>('POST', `/guests/${guest.node}/${guest.type}/${guest.vmid}/${action}`),
    usage: ({ node, type, vmid }: UsageTarget, timeframe: Timeframe) =>
        request<{ points: UsagePoint[] }>(
            'GET',
            `/usage/${type ? `${node}/${type}/${vmid}` : node}?${new URLSearchParams({ timeframe })}`,
        ),
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
    journal: (vmid: number, priority: number, lines = 500) =>
        request<LogEntry[]>(
            'GET',
            `/logs/journal?${new URLSearchParams({ vmid: String(vmid), priority: String(priority), lines: String(lines) })}`,
        ),
    dockerLogs: (vmid: number, lines = 200) =>
        request<DockerLogs>('GET', `/logs/docker?${new URLSearchParams({ vmid: String(vmid), lines: String(lines) })}`),
    tasks: () => request<TaskEntry[]>('GET', '/logs/tasks'),
    taskLog: (node: string, upid: string) =>
        request<{ lines: string[] }>('GET', `/logs/tasks/${node}/log?${new URLSearchParams({ upid })}`),
    tailscale: () => request<Tailscale>('GET', '/tailscale'),
    connectTailscale: (authKey: string) => request<void>('POST', '/tailscale/connect', { authKey }),
    logoutTailscale: () => request<void>('POST', '/tailscale/logout'),
    setTailscaleServe: (enabled: boolean) => request<void>('PUT', '/tailscale/serve', { enabled }),
    saveTailscaleSettings: (settings: TailscaleSettings) => request<void>('PUT', '/tailscale/settings', settings),
    useTailscaleTag: () => request<void>('POST', '/tailscale/tag'),
    setTailscaleService: ({ name, published }: { name: string; published: boolean }) =>
        request<void>('PUT', `/tailscale/services/${name}`, { published }),
    apps: () => request<Apps>('GET', '/apps'),
    installJellyfin: (jellyfin: JellyfinAnswers, keepSecrets = false) =>
        request<void>('POST', '/apps/jellyfin/install', { jellyfin, keepSecrets }),
    installApp: (id: string, answers: MediaStackAnswers, keepSecrets = false) =>
        request<void>('POST', `/apps/${id}/install`, { ...answers, keepSecrets }),
    retryApp: (id: string) => request<void>('POST', `/apps/${id}/retry`),
    forgetAppAnswers: (id: string) => request<void>('DELETE', `/apps/${id}/answers`),
    settingsExportUrl: '/api/settings/export',
    downloadDataBackup,
    restoreDataBackup,
    importSettings: (yaml: string, apply: boolean) =>
        request<SettingsImport>('POST', `/settings/import${apply ? '?apply=1' : ''}`, yaml, 'application/yaml'),
    health: () => request<Health>('GET', '/health'),
    refreshHealth: () => request<Health>('POST', '/health/refresh'),
    addCheck: (input: CheckInput) => request<ServiceCheck>('POST', '/health/checks', input),
    updateCheck: (id: string, input: CheckInput) => request<void>('PUT', `/health/checks/${id}`, input),
    deleteCheck: (id: string) => request<void>('DELETE', `/health/checks/${id}`),
}

export function jellyfinImage(itemId: string, type: 'Primary' | 'Backdrop') {
    return `/api/jellyfin/items/${itemId}/image?type=${type}`
}
