import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ApiError, api } from '@/lib/api'
import { profileDescription, profileLabel } from '@/lib/profileLabels'
import { useAuthStore } from '@/stores/auth'
import type {
  CreateTenantSkillRequest,
  ProfileListResponse,
  ProfileSkillBinding,
  TenantSkill,
  TenantSkillListResponse,
  UpdateTenantSkillRequest,
} from '@/types/api'

const DEFAULT_PROFILE = 'coding'

export function SkillsAdmin() {
  const token = useAuthStore((s) => s.token)
  const qc = useQueryClient()
  const [draft, setDraft] = useState<CreateTenantSkillRequest>({
    skill_key: '',
    description: '',
    body: '',
    enabled: true,
  })
  const [profile, setProfile] = useState(DEFAULT_PROFILE)
  const [bindingDraft, setBindingDraft] = useState('')
  const [error, setError] = useState<string | null>(null)

  const profilesQ = useQuery({
    queryKey: ['profiles'],
    queryFn: () => api<ProfileListResponse>('/agent/profiles', { token }),
    enabled: !!token,
    staleTime: 5 * 60 * 1000,
  })

  const { data, isLoading, error: listErr } = useQuery({
    queryKey: ['admin-skills'],
    queryFn: () =>
      api<TenantSkillListResponse>('/admin/skills?include=body', { token }),
    enabled: !!token,
  })

  const bindingQ = useQuery({
    queryKey: ['admin-profile-binding', profile],
    queryFn: () =>
      api<ProfileSkillBinding>(
        `/admin/profiles/${encodeURIComponent(profile)}/skills`,
        { token },
      ),
    enabled: !!token,
  })

  const createMut = useMutation({
    mutationFn: (body: CreateTenantSkillRequest) =>
      api<TenantSkill>('/admin/skills', {
        method: 'POST',
        token,
        body: JSON.stringify(body),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['admin-skills'] })
      setDraft({ skill_key: '', description: '', body: '', enabled: true })
      setError(null)
    },
    onError: (e) => setError(humanError(e)),
  })

  const updateMut = useMutation({
    mutationFn: ({ key, body }: { key: string; body: UpdateTenantSkillRequest }) =>
      api<TenantSkill>(`/admin/skills/${encodeURIComponent(key)}`, {
        method: 'PUT',
        token,
        body: JSON.stringify(body),
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['admin-skills'] }),
    onError: (e) => setError(humanError(e)),
  })

  const deleteMut = useMutation({
    mutationFn: (key: string) =>
      api<void>(`/admin/skills/${encodeURIComponent(key)}`, {
        method: 'DELETE',
        token,
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['admin-skills'] }),
    onError: (e) => setError(humanError(e)),
  })

  const bindMut = useMutation({
    mutationFn: (skillKeys: string[]) =>
      api<ProfileSkillBinding>(
        `/admin/profiles/${encodeURIComponent(profile)}/skills`,
        {
          method: 'PUT',
          token,
          body: JSON.stringify({ skill_keys: skillKeys }),
        },
      ),
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: ['admin-profile-binding', profile] }),
    onError: (e) => setError(humanError(e)),
  })

  const skills = data?.skills ?? []
  const profiles = profilesQ.data?.profiles ?? []
  const selectedProfile = profiles.find((p) => p.name === profile)

  useEffect(() => {
    if (profiles.length === 0) return
    if (!profiles.some((p) => p.name === profile)) {
      setProfile(profiles[0].name)
      setBindingDraft('')
    }
  }, [profiles, profile])

  function submitCreate() {
    if (!draft.skill_key.trim() || !draft.body.trim()) return
    createMut.mutate(draft)
  }

  function submitBinding() {
    const keys = bindingDraft
      .split(/[\s,]+/)
      .map((s) => s.trim())
      .filter((s) => s.length > 0)
    bindMut.mutate(keys)
  }

  return (
    <div className="flex h-full flex-col gap-4 overflow-auto p-6">
      {error && (
        <Card className="border-destructive">
          <CardContent className="flex items-center justify-between py-3 text-sm text-destructive">
            <span>{error}</span>
            <Button size="sm" variant="ghost" onClick={() => setError(null)}>
              关闭
            </Button>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle>新建 Skill</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <div className="flex flex-wrap gap-3">
            <div className="flex flex-col gap-1">
              <Label htmlFor="skill-key">Skill Key</Label>
              <Input
                id="skill-key"
                placeholder="lowercase-hyphen-only"
                value={draft.skill_key}
                onChange={(e) => setDraft({ ...draft, skill_key: e.target.value })}
                className="w-64"
              />
            </div>
            <div className="flex flex-1 flex-col gap-1 min-w-[200px]">
              <Label htmlFor="skill-desc">描述</Label>
              <Input
                id="skill-desc"
                placeholder="一句话描述"
                value={draft.description ?? ''}
                onChange={(e) => setDraft({ ...draft, description: e.target.value })}
              />
            </div>
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="skill-body">Skill Body</Label>
            <textarea
              id="skill-body"
              className="min-h-[120px] rounded-md border bg-background p-2 font-mono text-sm"
              value={draft.body}
              onChange={(e) => setDraft({ ...draft, body: e.target.value })}
              placeholder="注入到 system prompt 的纯文本"
            />
          </div>
          <Button
            size="sm"
            className="w-fit"
            disabled={!draft.skill_key.trim() || !draft.body.trim() || createMut.isPending}
            onClick={submitCreate}
          >
            保存
          </Button>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>智能体绑定</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <div className="flex flex-wrap items-end gap-3">
            <div className="flex flex-col gap-1">
              <Label htmlFor="profile-select">智能体类型</Label>
              <select
                id="profile-select"
                className="h-9 rounded-md border bg-background px-2 text-sm"
                value={profile}
                onChange={(e) => {
                  setProfile(e.target.value)
                  setBindingDraft('')
                }}
                disabled={profilesQ.isLoading || profiles.length === 0}
              >
                {profiles.map((p) => (
                  <option key={p.name} value={p.name}>
                    {profileLabel(p.name)}
                  </option>
                ))}
              </select>
              {selectedProfile &&
                profileDescription(selectedProfile.name, selectedProfile.description) && (
                  <p className="max-w-xs text-xs text-muted-foreground">
                    {profileDescription(
                      selectedProfile.name,
                      selectedProfile.description,
                    )}
                  </p>
                )}
            </div>
            <div className="flex flex-1 flex-col gap-1 min-w-[260px]">
              <Label htmlFor="binding-keys">Skill Keys (空格或逗号分隔)</Label>
              <Input
                id="binding-keys"
                value={bindingDraft}
                onChange={(e) => setBindingDraft(e.target.value)}
                placeholder={(bindingQ.data?.skill_keys ?? []).join(' ')}
              />
            </div>
            <Button size="sm" disabled={bindMut.isPending} onClick={submitBinding}>
              保存绑定
            </Button>
          </div>
          <p className="text-xs text-muted-foreground">
            当前绑定:{' '}
            {bindingQ.data?.skill_keys?.length
              ? bindingQ.data.skill_keys.join(', ')
              : '（未配置，使用智能体默认）'}
          </p>
        </CardContent>
      </Card>

      <Card className="flex-1">
        <CardHeader>
          <CardTitle>Skill 列表</CardTitle>
        </CardHeader>
        <CardContent>
          {isLoading && <p className="text-sm text-muted-foreground">加载中…</p>}
          {listErr && (
            <p className="text-sm text-destructive">加载失败：{(listErr as Error).message}</p>
          )}
          {!isLoading && skills.length === 0 && (
            <p className="text-sm text-muted-foreground">暂无租户 Skill</p>
          )}
          <ul className="flex flex-col gap-3">
            {skills.map((s) => (
              <SkillRow
                key={s.id}
                skill={s}
                onSave={(body) => updateMut.mutate({ key: s.skill_key, body })}
                onDelete={() => deleteMut.mutate(s.skill_key)}
                saving={updateMut.isPending}
              />
            ))}
          </ul>
        </CardContent>
      </Card>
    </div>
  )
}

function SkillRow({
  skill,
  onSave,
  onDelete,
  saving,
}: {
  skill: TenantSkill
  onSave: (body: UpdateTenantSkillRequest) => void
  onDelete: () => void
  saving: boolean
}) {
  const [body, setBody] = useState(skill.body)
  const [description, setDescription] = useState(skill.description)

  const dirty = body !== skill.body || description !== skill.description

  return (
    <li className="rounded-md border p-3 text-sm">
      <div className="mb-1 flex items-center justify-between gap-2">
        <div className="flex flex-col">
          <span className="font-mono text-xs text-muted-foreground">{skill.skill_key}</span>
          <span className="text-xs text-muted-foreground">
            hash {skill.content_hash.slice(0, 12)} ·{' '}
            {new Date(skill.updated_at).toLocaleString()}
          </span>
        </div>
        <div className="flex gap-2">
          <Button
            size="sm"
            variant={skill.enabled ? 'secondary' : 'default'}
            disabled={saving}
            onClick={() => onSave({ enabled: !skill.enabled })}
          >
            {skill.enabled ? '禁用' : '启用'}
          </Button>
          <Button
            size="sm"
            variant="secondary"
            disabled={saving || !dirty}
            onClick={() => onSave({ body, description })}
          >
            更新
          </Button>
          <Button size="sm" variant="ghost" onClick={onDelete}>
            删除
          </Button>
        </div>
      </div>
      <Input
        className="mb-2"
        value={description}
        onChange={(e) => setDescription(e.target.value)}
        placeholder="描述"
      />
      <textarea
        className="w-full min-h-[100px] rounded border bg-background p-2 font-mono text-xs"
        value={body}
        onChange={(e) => setBody(e.target.value)}
      />
    </li>
  )
}

function humanError(e: unknown): string {
  if (e instanceof ApiError) {
    try {
      const j = JSON.parse(e.body) as { error?: string; detail?: string }
      return j.error ? `${j.error}${j.detail ? ': ' + j.detail : ''}` : e.message
    } catch {
      return e.body || e.message
    }
  }
  return e instanceof Error ? e.message : String(e)
}
