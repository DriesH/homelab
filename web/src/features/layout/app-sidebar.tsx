import { Fragment } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { getRouteApi, Link, useNavigate, useRouterState } from '@tanstack/react-router'
import { ChevronsUpDownIcon, LogOutIcon, MonitorIcon, MoonIcon, ServerIcon, SettingsIcon, SunIcon } from 'lucide-react'

import { useTheme } from '@/components/theme-provider'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuGroup,
    DropdownMenuItem,
    DropdownMenuLabel,
    DropdownMenuSeparator,
    DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
    Sidebar,
    SidebarContent,
    SidebarFooter,
    SidebarGroup,
    SidebarGroupContent,
    SidebarGroupLabel,
    SidebarHeader,
    SidebarMenu,
    SidebarMenuBadge,
    SidebarMenuButton,
    SidebarMenuItem,
    SidebarRail,
    SidebarSeparator,
    useSidebar,
} from '@/components/ui/sidebar'
import { api } from '@/lib/api'
import { selfUpdateQuery } from '@/lib/queries'
import { navigation, type NavItem } from './navigation'

const route = getRouteApi('/_app')

export function AppSidebar() {
    const { data: selfUpdate } = useQuery(selfUpdateQuery)

    return (
        <Sidebar collapsible="icon">
            <SidebarHeader>
                <SidebarMenu>
                    <SidebarMenuItem>
                        <SidebarMenuButton size="lg" render={<Link to="/" />}>
                            <div className="flex aspect-square size-8 items-center justify-center rounded-md bg-sidebar-primary text-sidebar-primary-foreground">
                                <ServerIcon className="size-4" />
                            </div>
                            <div className="grid flex-1 text-left leading-tight">
                                <span className="font-semibold">Homelab</span>
                                {selfUpdate && (
                                    <span className="font-mono text-xs text-muted-foreground">
                                        {selfUpdate.version}
                                    </span>
                                )}
                            </div>
                        </SidebarMenuButton>
                    </SidebarMenuItem>
                </SidebarMenu>
            </SidebarHeader>

            <SidebarContent>
                {navigation.map((group, index) => (
                    <Fragment key={group.label}>
                        {/* Folded, the group labels are hidden, so a line marks where a group starts. */}
                        {index > 0 && <SidebarSeparator className="hidden group-data-[collapsible=icon]:block" />}
                        <SidebarGroup>
                            <SidebarGroupLabel>{group.label}</SidebarGroupLabel>
                            <SidebarGroupContent>
                                <SidebarMenu>
                                    {group.items.map((item) => (
                                        <NavLink
                                            key={item.to}
                                            item={item}
                                            badge={item.to === '/updates' && selfUpdate?.updateAvailable}
                                        />
                                    ))}
                                </SidebarMenu>
                            </SidebarGroupContent>
                        </SidebarGroup>
                    </Fragment>
                ))}
            </SidebarContent>

            <SidebarFooter>
                <SidebarMenu>
                    <NavLink item={{ to: '/settings', label: 'Settings', icon: SettingsIcon }} />
                    <AccountMenu />
                </SidebarMenu>
            </SidebarFooter>
            <SidebarRail />
        </Sidebar>
    )
}

function NavLink({ item, badge }: { item: NavItem; badge?: boolean }) {
    const { isMobile, setOpenMobile } = useSidebar()
    const pathname = useRouterState({ select: (state) => state.location.pathname })

    return (
        <SidebarMenuItem>
            <SidebarMenuButton
                isActive={pathname === item.to}
                tooltip={item.label}
                render={<Link to={item.to} onClick={() => isMobile && setOpenMobile(false)} />}
            >
                <item.icon />
                <span>{item.label}</span>
            </SidebarMenuButton>
            {badge && (
                <SidebarMenuBadge>
                    <span className="size-2 rounded-full bg-primary" aria-label="Homelab update available" />
                </SidebarMenuBadge>
            )}
        </SidebarMenuItem>
    )
}

function AccountMenu() {
    const { session } = route.useRouteContext()
    const { setTheme } = useTheme()
    const { isMobile } = useSidebar()
    const queryClient = useQueryClient()
    const navigate = useNavigate()

    async function logout() {
        await api.logout()
        queryClient.clear()
        navigate({ to: '/login' })
    }

    return (
        <SidebarMenuItem>
            <DropdownMenu>
                <DropdownMenuTrigger
                    render={<SidebarMenuButton size="lg" className="data-popup-open:bg-sidebar-accent" />}
                >
                    <Avatar>
                        <AvatarFallback className="uppercase">{session.username.charAt(0)}</AvatarFallback>
                    </Avatar>
                    <span className="flex-1 truncate font-medium">{session.username}</span>
                    <ChevronsUpDownIcon className="ml-auto" />
                </DropdownMenuTrigger>
                <DropdownMenuContent side={isMobile ? 'top' : 'right'} align="end" className="w-48">
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
        </SidebarMenuItem>
    )
}
