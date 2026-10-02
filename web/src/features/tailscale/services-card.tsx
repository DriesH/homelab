import { ExternalLinkIcon, Loader2Icon } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Switch } from '@/components/ui/switch'
import { api, type Tailscale } from '@/lib/api'
import { useTailscaleMutation } from './use-tailscale-mutation'

const hostTag = 'tag:homelab'

export function ServicesCard({ tailscale }: { tailscale: Tailscale }) {
    const tagged = tailscale.tags.includes(hostTag)

    return (
        <Card>
            <CardHeader>
                <CardTitle>Apps on your tailnet</CardTitle>
                <CardDescription>
                    Tailscale Services give apps their own address with HTTPS, like{' '}
                    <span className="font-mono">https://seerr.&lt;tailnet&gt;.ts.net</span>. Only devices in your
                    tailnet can open them.
                </CardDescription>
            </CardHeader>
            {tagged ? <ServiceList tailscale={tailscale} /> : <TagSetup tailscale={tailscale} />}
        </Card>
    )
}

function ServiceList({ tailscale }: { tailscale: Tailscale }) {
    const service = useTailscaleMutation(api.setTailscaleService, ({ published }) =>
        published ? 'Published on your tailnet' : 'Removed from your tailnet',
    )

    return (
        <CardContent className="flex flex-col gap-4">
            <ul className="divide-y rounded-lg border">
                {tailscale.services.map((app) => (
                    <li key={app.name} className="flex items-center justify-between gap-4 px-4 py-3">
                        <div className="flex min-w-0 flex-col gap-0.5">
                            <span className="text-sm font-medium">{app.title}</span>
                            {app.url ? (
                                <a
                                    href={app.url}
                                    target="_blank"
                                    rel="noreferrer"
                                    className="inline-flex items-center gap-1 font-mono text-xs break-all text-muted-foreground underline-offset-4 hover:underline"
                                >
                                    {app.url}
                                    <ExternalLinkIcon className="size-3 shrink-0" />
                                </a>
                            ) : (
                                <span className="font-mono text-xs text-muted-foreground">svc:{app.name}</span>
                            )}
                        </div>
                        <Switch
                            checked={app.published}
                            disabled={service.isPending}
                            onCheckedChange={(published) => service.mutate({ name: app.name, published })}
                            aria-label={`Publish ${app.title} on your tailnet`}
                        />
                    </li>
                ))}
            </ul>
            <p className="text-xs text-muted-foreground">
                A new service only works after it is approved in the admin console (Services page), unless your access
                controls approve it automatically.
            </p>
        </CardContent>
    )
}

const policy = `"tagOwners": {
  "tag:homelab": ["autogroup:admin"]
},
"autoApprovers": {
  "services": {
    "svc:seerr": ["tag:homelab"],
    "svc:jellyfin": ["tag:homelab"]
  }
}`

function TagSetup({ tailscale }: { tailscale: Tailscale }) {
    const tag = useTailscaleMutation(api.useTailscaleTag, () => 'Log in again with the link on this page')

    return (
        <>
            <CardContent className="flex flex-col gap-4 text-sm">
                <p>Tailscale Services need a manager with a tag. Do this once in the Tailscale admin console:</p>
                <ol className="flex list-decimal flex-col gap-3 pl-5">
                    <li>
                        In <span className="font-medium">Access controls</span>, add this to your policy file:
                        <pre className="mt-2 overflow-x-auto rounded-md bg-muted p-3 font-mono text-xs">{policy}</pre>
                    </li>
                    <li>
                        In <span className="font-medium">Services</span>, choose Advertise &gt; Define a Service, and
                        add <span className="font-mono">svc:seerr</span> and{' '}
                        <span className="font-mono">svc:jellyfin</span>, each with the endpoint{' '}
                        <span className="font-mono">tcp:443</span>.
                    </li>
                    <li>
                        Click the button below, and log in again with the link that appears at the top of this page.
                    </li>
                </ol>
                <p className="text-xs text-muted-foreground">
                    After that, the manager belongs to {hostTag} instead of to your account, and its key no longer
                    expires. If your access controls only allow your own devices, also allow {hostTag}.
                </p>
            </CardContent>
            <CardFooter>
                <Button disabled={tag.isPending || tailscale.connecting} onClick={() => tag.mutate(undefined)}>
                    {(tag.isPending || tailscale.connecting) && <Loader2Icon className="animate-spin" />}
                    Use {hostTag}
                </Button>
            </CardFooter>
        </>
    )
}
