import { useQuery, useQueryClient } from '@tanstack/react-query'
import { getRouteApi, Link, Outlet, useNavigate } from '@tanstack/react-router'
import { ArrowUpCircleIcon, LogOutIcon, MonitorIcon, MoonIcon, ServerIcon, SunIcon, UserIcon } from 'lucide-react'

import { useTheme } from '@/components/theme-provider'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuGroup,
    DropdownMenuItem,
    DropdownMenuLabel,
    DropdownMenuSeparator,
    DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { useReloadOnNewVersion } from '@/features/self-update/use-reload-on-new-version'
import { api } from '@/lib/api'
import { selfUpdateQuery } from '@/lib/queries'

const route = getRouteApi('/_app')

const navigation = [
    { to: '/', label: 'Overview' },
    { to: '/health', label: 'Health' },
    { to: '/updates', label: 'Updates' },
    { to: '/jellyfin', label: 'Jellyfin' },
    { to: '/tailscale', label: 'Tailscale' },
] as const

export function AppLayout() {
    const { session } = route.useRouteContext()
    const { setTheme } = useTheme()
    const queryClient = useQueryClient()
    const navigate = useNavigate()
    const { data: selfUpdate } = useQuery(selfUpdateQuery)
    useReloadOnNewVersion()

    async function logout() {
        await api.logout()
        queryClient.clear()
        navigate({ to: '/login' })
    }

    return (
        <div className="min-h-svh bg-muted/40">
            <header className="sticky top-0 z-10 border-b bg-background/80 backdrop-blur">
                <div className="mx-auto flex h-14 max-w-6xl items-center gap-3 px-4 sm:px-6">
                    <div className="flex size-8 items-center justify-center rounded-md bg-primary text-primary-foreground">
                        <ServerIcon className="size-4" />
                    </div>
                    <span className="hidden font-semibold sm:inline">Homelab</span>
                    {selfUpdate && (
                        <span className="hidden font-mono text-xs text-muted-foreground md:inline">
                            {selfUpdate.version}
                        </span>
                    )}

                    <nav className="-mx-1 flex min-w-0 items-center gap-1 overflow-x-auto px-1 text-sm sm:ml-4">
                        {navigation.map((item) => (
                            <Link
                                key={item.to}
                                to={item.to}
                                activeOptions={{ exact: true }}
                                className="shrink-0 rounded-md px-2 py-1.5 text-muted-foreground transition-colors hover:text-foreground data-[status=active]:bg-muted data-[status=active]:text-foreground sm:px-3"
                            >
                                {item.label}
                            </Link>
                        ))}
                    </nav>

                    {selfUpdate?.updateAvailable && (
                        <Badge
                            className="ml-auto shrink-0"
                            render={
                                <Link to="/updates" aria-label={`Update available: ${selfUpdate.latest?.version}`} />
                            }
                        >
                            <ArrowUpCircleIcon />
                            <span className="hidden sm:inline">Update available</span>
                        </Badge>
                    )}

                    <DropdownMenu>
                        <DropdownMenuTrigger
                            render={
                                <Button
                                    variant="ghost"
                                    size="sm"
                                    className={selfUpdate?.updateAvailable ? '' : 'ml-auto'}
                                />
                            }
                            aria-label="Account menu"
                        >
                            <UserIcon />
                            <span className="hidden sm:inline">{session.username}</span>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end" className="w-44">
                            <DropdownMenuGroup>
                                <DropdownMenuLabel>Theme</DropdownMenuLabel>
                                <DropdownMenuItem onClick={() => setTheme('light')}>
                                    <SunIcon /> Light
                                </DropdownMenuItem>
                                <DropdownMenuItem onClick={() => setTheme('dark')}>
                                    <MoonIcon /> Dark
                                </DropdownMenuItem>
                                <DropdownMenuItem onClick={() => setTheme('system')}>
                                    <MonitorIcon /> System
                                </DropdownMenuItem>
                            </DropdownMenuGroup>
                            <DropdownMenuSeparator />
                            {selfUpdate && (
                                <DropdownMenuGroup>
                                    <DropdownMenuLabel className="font-mono font-normal">
                                        Homelab {selfUpdate.version}
                                    </DropdownMenuLabel>
                                </DropdownMenuGroup>
                            )}
                            <DropdownMenuItem onClick={logout}>
                                <LogOutIcon /> Sign out
                            </DropdownMenuItem>
                        </DropdownMenuContent>
                    </DropdownMenu>
                </div>
            </header>

            <main className="mx-auto max-w-6xl px-4 py-6 sm:px-6">
                <Outlet />
            </main>
        </div>
    )
}
