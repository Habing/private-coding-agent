import { readFileSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

import type { APIRequestContext, Page } from '@playwright/test'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

export const E2E_TENANT = process.env.PCA_E2E_TENANT ?? 'default'
export const E2E_EMAIL = process.env.PCA_E2E_EMAIL ?? 'demo@example.com'
export const E2E_PASSWORD = process.env.PCA_E2E_PASSWORD ?? 'demo123'
export const E2E_WORKFLOW_SLUG = process.env.PCA_E2E_WORKFLOW_SLUG ?? 'e2e-mock-chain'

export function e2eMockChainYaml(): string {
  const yamlPath = path.resolve(__dirname, '../../../deploy/compose/examples/e2e-mock-chain.yaml')
  return readFileSync(yamlPath, 'utf8')
}

export async function apiLogin(request: APIRequestContext, baseURL: string): Promise<string> {
  const res = await request.post(`${baseURL}/auth/login`, {
    data: { tenant: E2E_TENANT, email: E2E_EMAIL, password: E2E_PASSWORD },
  })
  if (!res.ok()) {
    throw new Error(`login failed: ${res.status()} ${await res.text()}`)
  }
  const body = (await res.json()) as { token?: string }
  if (!body.token) throw new Error('login response missing token')
  return body.token
}

/** Upsert workflow with golden mock-chain DSL for designer smoke. */
export async function ensureMockChainWorkflow(
  request: APIRequestContext,
  baseURL: string,
  token: string,
): Promise<void> {
  const slug = E2E_WORKFLOW_SLUG
  const dsl_yaml = e2eMockChainYaml()
  const headers = { Authorization: `Bearer ${token}` }

  const existing = await request.get(`${baseURL}/admin/workflows/${slug}`, { headers })
  if (existing.ok()) {
    await request.put(`${baseURL}/admin/workflows/${slug}`, {
      headers,
      data: {
        name: 'Mock 状态巡检',
        description: 'Playwright designer smoke',
        dsl_yaml,
      },
    })
    return
  }

  const created = await request.post(`${baseURL}/admin/workflows`, {
    headers,
    data: {
      slug,
      name: 'Mock 状态巡检',
      description: 'Playwright designer smoke',
      dsl_yaml,
    },
  })
  if (!created.ok()) {
    throw new Error(`create workflow failed: ${created.status()} ${await created.text()}`)
  }
}

export async function loginViaUi(page: Page): Promise<void> {
  await page.goto('/login')
  await page.getByLabel('租户').fill(E2E_TENANT)
  await page.getByLabel('邮箱').fill(E2E_EMAIL)
  await page.getByLabel('密码').fill(E2E_PASSWORD)
  await page.getByRole('button', { name: '登录' }).click()
  await page.waitForURL((url) => !url.pathname.endsWith('/login'), { timeout: 20_000 })
}

function isDesignDecompileResponse(url: string, method: string, ok: boolean): boolean {
  return url.includes('/admin/workflows/design/decompile') && method === 'POST' && ok
}

function isDesignCompileResponse(url: string, method: string, ok: boolean): boolean {
  return url.includes('/admin/workflows/design/compile') && method === 'POST' && ok
}

/** Call before UI action that triggers decompile; safe if decompile already completed. */
export function watchDesignDecompile(page: Page) {
  return page.waitForResponse(
    (r) => isDesignDecompileResponse(r.url(), r.request().method(), r.ok()),
    { timeout: 45_000 },
  )
}

/** Call before edit that triggers compile; safe if compile already completed. */
export function watchDesignCompile(page: Page) {
  return page.waitForResponse(
    (r) => isDesignCompileResponse(r.url(), r.request().method(), r.ok()),
    { timeout: 45_000 },
  )
}

export async function waitForDesignDecompile(page: Page): Promise<void> {
  try {
    const res = await watchDesignDecompile(page)
    await res.finished()
  } catch {
    // Listener attached after network completed — canvas should already show steps.
  }
}

export async function waitForDesignCompile(page: Page): Promise<void> {
  try {
    const res = await watchDesignCompile(page)
    await res.finished()
  } catch {
    // compile may have completed before wait attached
  }
}
