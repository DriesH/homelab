import { useQueryClient } from '@tanstack/react-query'
import { getRouteApi, Link, Outlet, useNavigate } from '@tanstack/react-router'
import { LogOutIcon, MonitorIcon, MoonIcon, ServerIcon, SunIcon, UserIcon } from 'lucide-react'

import { useTheme } from '@/components/theme-provider'
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
import { api } from '@/lib/api'

const route = getRouteApi('/_app')

const navigation = [
    { to: '/', label: 'Overview' },
    { to: '/health', label: 'Health' },
    { to: '/updates', label: 'Updates' },
    { to: '/jellyfin', label: 'Jellyfin' },
] as const

export function AppLayout() {
    const { session } = route.useRouteContext()
    const { setTheme } = useTheme()
    const queryClient = useQueryClient()
    const navigate = useNavigate()

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

                    <DropdownMenu>
                        <DropdownMenuTrigger
                            render={<Button variant="ghost" size="sm" className="ml-auto" />}
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
