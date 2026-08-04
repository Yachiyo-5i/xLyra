import assert from 'node:assert/strict'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { test } from 'node:test'
import ts from 'typescript'

async function loadHTTPModule() {
  const sourcePath = new URL('./http.ts', import.meta.url)
  let source = await readFile(sourcePath, 'utf8')
  source = source.replace(
    "const API_BASE = (import.meta.env.VITE_API_BASE_URL ?? '').replace(/\\/$/, '')",
    "const API_BASE = ''",
  )
  const transpiled = ts.transpileModule(source, {
    compilerOptions: {
      module: ts.ModuleKind.ESNext,
      target: ts.ScriptTarget.ES2023,
      verbatimModuleSyntax: true,
    },
  }).outputText
  const testDir = path.join(tmpdir(), `xlyra-http-test-${process.pid}-${Date.now()}`)
  await mkdir(testDir, { recursive: true })
  const modulePath = path.join(testDir, 'http.mjs')
  await writeFile(modulePath, transpiled)
  return import(modulePath)
}

test('apiFetch attaches CSRF token to admin logout DELETE requests', async () => {
  const requests = []
  const originalFetch = globalThis.fetch
  globalThis.fetch = async (url, init) => {
    requests.push({ url, init })
    return new Response(null, { status: 204 })
  }

  try {
    const { apiFetch, setCSRFToken } = await loadHTTPModule()
    setCSRFToken('logout-csrf-token')

    await apiFetch('/api/v1/auth/session', { method: 'DELETE' })

    assert.equal(requests.length, 1)
    assert.equal(requests[0].url, '/api/v1/auth/session')
    assert.equal(requests[0].init.headers.get('X-CSRF-Token'), 'logout-csrf-token')
  } finally {
    globalThis.fetch = originalFetch
  }
})

test('apiFetch still does not attach CSRF token to admin login POST requests', async () => {
  const requests = []
  const originalFetch = globalThis.fetch
  globalThis.fetch = async (url, init) => {
    requests.push({ url, init })
    return new Response(JSON.stringify({ expires_at: null, admin: {} }), {
      status: 201,
      headers: { 'content-type': 'application/json' },
    })
  }

  try {
    const { apiFetch, setCSRFToken } = await loadHTTPModule()
    setCSRFToken('existing-csrf-token')

    await apiFetch('/api/v1/auth/session', { method: 'POST', body: { username: 'alice', password: 'secret' } })

    assert.equal(requests.length, 1)
    assert.equal(requests[0].url, '/api/v1/auth/session')
    assert.equal(requests[0].init.headers.get('X-CSRF-Token'), null)
  } finally {
    globalThis.fetch = originalFetch
  }
})
