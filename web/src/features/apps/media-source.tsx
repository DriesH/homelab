import { useState, type ReactNode } from 'react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import type { Apps } from '@/lib/api'
import { Field, Section } from './form-parts'

export type MediaSource = { nasServer: string; nasExport: string; mediaFolder: string }

type MediaSourceFieldsProps = {
    id: string
    defaults: Apps['defaults']
    value: MediaSource
    onChange: (value: Partial<MediaSource>) => void
    help: string
    // children are the movies and series fields. path is where the media is, for their help.
    children: (path: string) => ReactNode
}

export function MediaSourceFields({ id, defaults, value, onChange, help, children }: MediaSourceFieldsProps) {
    const [where, setWhere] = useState<'nas' | 'folder'>(value.mediaFolder ? 'folder' : 'nas')

    function choose(next: 'nas' | 'folder') {
        setWhere(next)
        onChange(next === 'nas' ? { mediaFolder: '' } : { nasServer: '', nasExport: '' })
    }

    if (defaults.mediaShare) {
        return (
            <Section title="Media" help={help}>
                <p className="text-sm">
                    Uses the media that is already mounted: <span className="font-mono">{defaults.mediaShare}</span>
                </p>
                {children(defaults.mediaShare)}
            </Section>
        )
    }

    const path =
        where === 'folder'
            ? value.mediaFolder || '/mnt/pve/media'
            : `${value.nasServer || 'NAS'}:${value.nasExport || '/volume1/media'}`

    return (
        <Section title="Media" help={help}>
            <div role="radiogroup" aria-label="Where your movies and series are" className="grid grid-cols-2 gap-2">
                <Button
                    type="button"
                    role="radio"
                    aria-checked={where === 'nas'}
                    variant={where === 'nas' ? 'secondary' : 'outline'}
                    onClick={() => choose('nas')}
                >
                    On a NAS (NFS)
                </Button>
                <Button
                    type="button"
                    role="radio"
                    aria-checked={where === 'folder'}
                    variant={where === 'folder' ? 'secondary' : 'outline'}
                    onClick={() => choose('folder')}
                >
                    On this host
                </Button>
            </div>

            {where === 'nas' ? (
                <>
                    <Field id={`${id}-nas-server`} label="NAS address">
                        <Input
                            id={`${id}-nas-server`}
                            placeholder="192.168.1.5"
                            value={value.nasServer}
                            onChange={(event) => onChange({ nasServer: event.target.value.trim() })}
                            required
                        />
                    </Field>
                    <Field id={`${id}-nas-export`} label="NFS export path" help="UGOS shows it on the NFS page.">
                        <Input
                            id={`${id}-nas-export`}
                            placeholder="/volume1/media"
                            value={value.nasExport}
                            onChange={(event) => onChange({ nasExport: event.target.value.trim() })}
                            required
                        />
                    </Field>
                </>
            ) : (
                <Field
                    id={`${id}-media-folder`}
                    label="Folder on the Proxmox host"
                    help="A folder on a disk of the host. For a new disk, make one first in Proxmox: Disks > Directory."
                >
                    <Input
                        id={`${id}-media-folder`}
                        placeholder="/mnt/pve/media"
                        list={`${id}-media-folders`}
                        autoComplete="off"
                        value={value.mediaFolder}
                        onChange={(event) => onChange({ mediaFolder: event.target.value.trim() })}
                        required
                    />
                    <datalist id={`${id}-media-folders`}>
                        {defaults.mediaFolders.map((folder) => (
                            <option key={folder.path} value={folder.path}>
                                {folder.storage}
                            </option>
                        ))}
                    </datalist>
                </Field>
            )}

            {children(path)}
        </Section>
    )
}
