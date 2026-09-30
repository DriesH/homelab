import { useState, type FormEvent } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { getRouteApi, useRouter } from '@tanstack/react-router'
import { REGEXP_ONLY_DIGITS } from 'input-otp'
import { ArrowLeftIcon, ServerIcon } from 'lucide-react'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { InputOTP, InputOTPGroup, InputOTPSlot } from '@/components/ui/input-otp'
import { Label } from '@/components/ui/label'
import { api } from '@/lib/api'
import { sessionQuery } from '@/lib/queries'

const route = getRouteApi('/login')

const codeLength = 6

// Only allow redirects within this app.
function safeRedirect(target: string | undefined) {
    if (target && target.startsWith('/') && !target.startsWith('//')) {
        return target
    }

    return '/'
}

// The server checks the password and the code together in the last step, so
// the first step can't be used to test passwords without a code.
export function LoginPage() {
    const router = useRouter()
    const queryClient = useQueryClient()
    const { redirect } = route.useSearch()

    const [step, setStep] = useState<'credentials' | 'code'>('credentials')
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

    function continueToCode(event: FormEvent) {
        event.preventDefault()
        login.reset()
        setStep('code')
    }

    function signIn(value = code) {
        if (value.length === codeLength && !login.isPending) {
            login.mutate({ username, password, code: value })
        }
    }

    function backToCredentials() {
        login.reset()
        setCode('')
        setStep('credentials')
    }

    return (
        <main className="flex min-h-svh items-center justify-center bg-muted/40 p-6">
            <Card className="w-full max-w-sm">
                <CardHeader>
                    <div className="mb-2 flex size-10 items-center justify-center rounded-lg bg-primary text-primary-foreground">
                        <ServerIcon className="size-5" />
                    </div>
                    <CardTitle className="text-xl">Homelab</CardTitle>
                    <CardDescription>
                        {step === 'credentials'
                            ? 'Sign in to manage your server.'
                            : 'Enter the 6-digit code from your authenticator app.'}
                    </CardDescription>
                </CardHeader>
                <CardContent>
                    {step === 'credentials' ? (
                        <form onSubmit={continueToCode} className="flex flex-col gap-5">
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

                            <Button type="submit">Continue</Button>
                        </form>
                    ) : (
                        <form
                            onSubmit={(event) => {
                                event.preventDefault()
                                signIn()
                            }}
                            className="flex flex-col gap-5"
                        >
                            {login.error && (
                                <Alert variant="destructive">
                                    <AlertDescription>{login.error.message}</AlertDescription>
                                </Alert>
                            )}

                            <InputOTP
                                maxLength={codeLength}
                                pattern={REGEXP_ONLY_DIGITS}
                                inputMode="numeric"
                                autoComplete="one-time-code"
                                autoFocus
                                aria-label="Authenticator code"
                                value={code}
                                onChange={setCode}
                                onComplete={signIn}
                                containerClassName="justify-center"
                            >
                                <InputOTPGroup className="gap-2">
                                    {Array.from({ length: codeLength }, (_, index) => (
                                        <InputOTPSlot
                                            key={index}
                                            index={index}
                                            className="size-11 rounded-md border text-lg first:rounded-md last:rounded-md"
                                        />
                                    ))}
                                </InputOTPGroup>
                            </InputOTP>

                            <Button type="submit" disabled={login.isPending || code.length !== codeLength}>
                                {login.isPending ? 'Signing in…' : 'Sign in'}
                            </Button>
                            <Button type="button" variant="ghost" onClick={backToCredentials}>
                                <ArrowLeftIcon />
                                Back
                            </Button>
                        </form>
                    )}
                </CardContent>
            </Card>
        </main>
    )
}
