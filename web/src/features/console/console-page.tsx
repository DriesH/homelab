import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { getRouteApi, Link } from '@tanstack/react-router'
import { FitAddon } from '@xterm/addon-fit'
import { Terminal } from '@xterm/xterm'
import '@xterm/xterm/css/xterm.css'
import { ArrowLeftIcon, RotateCwIcon } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { overviewQuery } from '@/lib/queries'

const route = getRouteApi('/_app/console/$vmid')

const font = '"Geist Mono Variable", ui-monospace, monospace'

type Status = 'connecting' | 'open' | 'closed'

export function ConsolePage() {
    const { vmid } = route.useParams()
    const { data: overview } = useQuery(overviewQuery)
    const guest = overview?.guests.find((item) => String(item.vmid) === vmid)
    const [status, setStatus] = useState<Status>('connecting')
    const [attempt, setAttempt] = useState(0)

    return (
        <div className="flex flex-col gap-4">
            <header className="flex flex-wrap items-center gap-3">
                <Button variant="ghost" size="icon-sm" render={<Link to="/" />} aria-label="Back to the overview">
                    <ArrowLeftIcon />
                </Button>
                <h1 className="text-lg font-semibold">{guest?.name ?? `Container ${vmid}`}</h1>
                <Badge variant="outline" className="font-mono">
                    LXC {vmid}
                </Badge>
                <StatusBadge status={status} />
                {status === 'closed' && (
                    <Button variant="outline" size="sm" className="ml-auto" onClick={() => setAttempt(attempt + 1)}>
                        <RotateCwIcon />
                        Connect again
                    </Button>
                )}
            </header>

            <ConsoleTerminal key={`${vmid}-${attempt}`} vmid={vmid} onStatus={setStatus} />

            <p className="text-xs text-muted-foreground">
                This is the console of the container, like the Console tab in Proxmox. Press Enter if it stays empty.
                Opening a console sends a Telegram message.
            </p>
        </div>
    )
}

function StatusBadge({ status }: { status: Status }) {
    if (status === 'open') {
        return <Badge variant="secondary">connected</Badge>
    }
    if (status === 'closed') {
        return <Badge variant="destructive">disconnected</Badge>
    }
    return <Badge variant="outline">connecting…</Badge>
}

function ConsoleTerminal({ vmid, onStatus }: { vmid: string; onStatus: (status: Status) => void }) {
    const container = useRef<HTMLDivElement>(null)

    useEffect(() => {
        const element = container.current
        if (!element) {
            return
        }

        let disposed = false
        const terminal = new Terminal({
            fontFamily: font,
            fontSize: 13,
            cursorBlink: true,
            theme: { background: '#0a0a0a', foreground: '#e5e5e5', cursor: '#e5e5e5' },
        })
        const fit = new FitAddon()
        terminal.loadAddon(fit)

        const scheme = window.location.protocol === 'https:' ? 'wss' : 'ws'
        const socket = new WebSocket(`${scheme}://${window.location.host}/api/guests/${vmid}/console`)
        socket.binaryType = 'arraybuffer'

        const encoder = new TextEncoder()
        const sendResize = () => {
            if (socket.readyState === WebSocket.OPEN) {
                socket.send(JSON.stringify({ type: 'resize', cols: terminal.cols, rows: terminal.rows }))
            }
        }

        socket.onopen = () => {
            onStatus('open')
            sendResize()
            terminal.focus()
        }
        socket.onmessage = (event) => {
            terminal.write(typeof event.data === 'string' ? event.data : new Uint8Array(event.data))
        }
        socket.onclose = () => {
            if (!disposed) {
                onStatus('closed')
                terminal.write('\r\n\x1b[2m[disconnected]\x1b[0m\r\n')
            }
        }

        terminal.onData((data) => {
            if (socket.readyState === WebSocket.OPEN) {
                socket.send(encoder.encode(data))
            }
        })
        terminal.onResize(sendResize)

        const observer = new ResizeObserver(() => fit.fit())

        // Measure the characters only after the font has loaded, or the columns are off.
        document.fonts.load(`13px ${font}`).finally(() => {
            if (disposed) {
                return
            }
            terminal.open(element)
            fit.fit()
            observer.observe(element)
        })

        return () => {
            disposed = true
            observer.disconnect()
            socket.close()
            terminal.dispose()
        }
    }, [vmid, onStatus])

    return (
        <div className="h-[70vh] overflow-hidden rounded-lg border bg-[#0a0a0a] p-2">
            <div ref={container} className="size-full" />
        </div>
    )
}
