import { Outlet, useRouterState } from '@tanstack/react-router'

import { SidebarInset, SidebarProvider, SidebarTrigger } from '@/components/ui/sidebar'
import { useReloadOnNewVersion } from '@/features/self-update/use-reload-on-new-version'
import { AppSidebar } from './app-sidebar'
import { navigation } from './navigation'

export function AppLayout() {
    const pathname = useRouterState({ select: (state) => state.location.pathname })
    const currentPage = navigation.flatMap((group) => group.items).find((item) => item.to === pathname)?.label
    useReloadOnNewVersion()

    return (
        <SidebarProvider>
            <AppSidebar />
            <SidebarInset className="bg-muted/40">
                {/* On desktop the sidebar is always there, so only phones need a bar to open it. */}
                <header className="sticky top-0 z-10 flex h-14 items-center gap-2 border-b bg-background/80 px-4 backdrop-blur md:hidden">
                    <SidebarTrigger className="-ml-1" />
                    <span className="font-semibold">{currentPage ?? 'Homelab'}</span>
                </header>
                <div className="mx-auto w-full max-w-6xl px-4 py-6 sm:px-6">
                    <Outlet />
                </div>
            </SidebarInset>
        </SidebarProvider>
    )
}
