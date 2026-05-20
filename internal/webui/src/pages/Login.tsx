import { useMutation } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'

import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ApiError, api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth'
import type { LoginResponse, MeResponse } from '@/types/api'

export function Login() {
  const navigate = useNavigate()
  const setAuth = useAuthStore((s) => s.setAuth)

  const [tenant, setTenant] = useState('default')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [errorMsg, setErrorMsg] = useState<string | null>(null)

  const mutation = useMutation({
    mutationFn: async () => {
      const { token } = await api<LoginResponse>('/auth/login', {
        method: 'POST',
        body: JSON.stringify({ tenant, email, password }),
      })
      const me = await api<MeResponse>('/me', { token })
      return { token, me }
    },
    onSuccess: ({ token, me }) => {
      setAuth(token, {
        id: me.user_id,
        tenant_id: me.tenant_id,
        email,
        role: me.role,
      })
      navigate('/', { replace: true })
    },
    onError: (err) => {
      if (err instanceof ApiError && err.status === 401) {
        setErrorMsg('登录失败：账号或密码错误')
      } else {
        setErrorMsg('登录失败，请稍后重试')
      }
    },
  })

  function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    setErrorMsg(null)
    mutation.mutate()
  }

  return (
    <div className="flex h-screen w-screen items-center justify-center bg-background">
      <Card className="w-[360px]">
        <CardHeader>
          <CardTitle>登录</CardTitle>
          <CardDescription>Private Coding Agent</CardDescription>
        </CardHeader>
        <CardContent>
          <form className="flex flex-col gap-4" onSubmit={onSubmit}>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="tenant">租户</Label>
              <Input
                id="tenant"
                value={tenant}
                onChange={(e) => setTenant(e.target.value)}
                autoComplete="organization"
                required
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="email">邮箱</Label>
              <Input
                id="email"
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                autoComplete="username"
                required
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="password">密码</Label>
              <Input
                id="password"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete="current-password"
                required
              />
            </div>
            {errorMsg && (
              <div
                role="alert"
                className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive"
              >
                {errorMsg}
              </div>
            )}
            <Button type="submit" disabled={mutation.isPending} className="mt-2">
              {mutation.isPending ? '登录中…' : '登录'}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}
