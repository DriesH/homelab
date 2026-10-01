import type { ReactNode } from 'react'

import { Label } from '@/components/ui/label'

export function Section({ title, help, children }: { title: string; help?: string; children: ReactNode }) {
    return (
        <fieldset className="flex flex-col gap-4">
            <div>
                <legend className="text-sm font-medium">{title}</legend>
                {help && <p className="text-xs text-muted-foreground">{help}</p>}
            </div>
            {children}
        </fieldset>
    )
}

type FieldProps = { id: string; label: string; help?: ReactNode; error?: string; children: ReactNode }

export function Field({ id, label, help, error, children }: FieldProps) {
    return (
        <div className="flex flex-col gap-2">
            <Label htmlFor={id}>{label}</Label>
            {children}
            {error ? (
                <p className="text-xs text-destructive">{error}</p>
            ) : (
                help && <p className="text-xs text-muted-foreground">{help}</p>
            )}
        </div>
    )
}
