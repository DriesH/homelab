import { useState } from 'react'

import { Button } from '@/components/ui/button'
import type { UpdateTarget } from '@/lib/api'

const collapsedCount = 6

export function PackageList({ target }: { target: UpdateTarget }) {
    const [expanded, setExpanded] = useState(false)
    const items = [
        ...target.packages.map((item) => ({
            key: `package-${item.name}`,
            name: item.name,
            detail: `${item.from} → ${item.to}`,
        })),
        ...target.images.map((item) => ({ key: `image-${item.service}`, name: item.service, detail: item.image })),
    ]

    if (items.length === 0) {
        return null
    }

    const visible = expanded ? items : items.slice(0, collapsedCount)

    return (
        <div className="flex flex-col gap-1">
            <ul className="divide-y rounded-md border text-sm">
                {visible.map((item) => (
                    <li key={item.key} className="flex items-center justify-between gap-4 px-3 py-1.5">
                        <span className="truncate font-medium">{item.name}</span>
                        <span className="truncate font-mono text-xs text-muted-foreground">{item.detail}</span>
                    </li>
                ))}
            </ul>
            {items.length > collapsedCount && (
                <Button variant="link" size="sm" className="self-start px-0" onClick={() => setExpanded(!expanded)}>
                    {expanded ? 'Show less' : `Show all ${items.length}`}
                </Button>
            )}
        </div>
    )
}
