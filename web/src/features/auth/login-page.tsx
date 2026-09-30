import { useState, type FormEvent } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { getRouteApi, useRouter } from '@tanstack/react-router'
import { ServerIcon } from 'lucide-react'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { InputOTP, InputOTPGroup, InputOTPSlot } from '@/components/ui/input-otp'
import { Label } from '@/components/ui/label'
import { api } from '@/lib/api'
import { sessionQuery } from '@/lib/queries'

const route = getRouteApi('/login')

// Only allow redirects within this app.
function safeRedirect(target: string | undefined) {
    if (target && target.startsWith('/') && !target.startsWith('//')) {
        return target
    }

    return '/'
}

export function LoginPage() {
    const router = useRouter()
    const queryClient = useQueryClient()
    const { redirect } = route.useSearch()

    const [username, setUsername] = useState('')
    const [password, setPassword] = useState('')
    const [code, setCode] = useState('')

    const login = useMutation({
        mutationFn: api.login,
        onSuccess(session) {
            queryClient.setQueryData(sessionQuery.queryKey, session)
            router.history.push(safeRedirect(redirect))
        },
        onError() {
            setCode('')
        },
    })

    function handleSubmit(event: FormEvent) {
        event.preventDefault()
        login.mutate({ username, password, code })
    }

    return (
        <main className="flex min-h-svh items-center justify-center bg-muted/40 p-6">
            <Card className="w-full max-w-sm">
                <CardHeader>
                    <div className="mb-2 flex size-10 items-center justify-center rounded-lg bg-primary text-primary-foreground">
                        <ServerIcon className="size-5" />
                    </div>
                    <CardTitle className="text-xl">Homelab</CardTitle>
                    <CardDescription>Sign in to manage your server.</CardDescription>
                </CardHeader>
                <CardContent>
                    <form onSubmit={handleSubmit} className="flex flex-col gap-5">
                        {login.error && (
                            <Alert variant="destructive">
                                <AlertDescription>{login.error.message}</AlertDescription>
                            </Alert>
                        )}

                        <div className="flex flex-col gap-2">
                            <Label htmlFor="username">Username</Label>
                            <Input
                                id="username"
                                autoComplete="username"
                                autoFocus
                                required
                                value={username}
                                onChange={(event) => setUsername(event.target.value)}
                            />
                        </div>

                        <div className="flex flex-col gap-2">
                            <Label htmlFor="password">Password</Label>
                            <Input
                                id="password"
                                type="password"
                                autoComplete="current-password"
                                required
                                value={password}
                                onChange={(event) => setPassword(event.target.value)}
                            />
                        </div>

                        <div className="flex flex-col gap-2">
                            <Label htmlFor="code">Authenticator code</Label>
                            <InputOTP
                                id="code"
                                maxLength={6}
                                inputMode="numeric"
                                autoComplete="one-time-code"
                                value={code}
                                onChange={setCode}
                            >
                                <InputOTPGroup>
                                    {[0, 1, 2, 3, 4, 5].map((index) => (
                                        <InputOTPSlot key={index} index={index} />
                                    ))}
                                </InputOTPGroup>
                            </InputOTP>
                        </div>

                        <Button type="submit" disabled={login.isPending || code.length !== 6}>
                            {login.isPending ? 'Signing in…' : 'Sign in'}
                        </Button>
                    </form>
                </CardContent>
            </Card>
        </main>
    )
}
