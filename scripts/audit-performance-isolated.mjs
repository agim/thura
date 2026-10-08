// Līdza's published audit, with a fresh browser process per route.
// Prepare .lidza/audit-{performance,common}.mjs with `lidza audit performance`.
// Run against a built app: BASE_URL=http://127.0.0.1:3002 node scripts/audit-performance-isolated.mjs
import { spawn } from 'node:child_process'
import { writeFile } from 'node:fs/promises'
import { pathToFileURL } from 'node:url'

const { staticPaths } = await import(pathToFileURL(`${process.cwd()}/dist/.server/entry-server.js`).href)
const routes = [...new Set(staticPaths())]
if (!routes.length) throw new Error('No prerendered routes to audit')
const budgets = { cls: 0.1, js: 300 * 1024, css: 100 * 1024, images: 1000 * 1024, total: 1600 * 1024 }
const samples = Number(process.env.AUDIT_SAMPLES || 1)
if (!Number.isInteger(samples) || samples < 1 || samples > 15) throw new Error('AUDIT_SAMPLES must be 1–15')
const results = []
const faults = []
let device
let llms
for (const route of routes) {
  console.error(`Auditing ${route} in a fresh browser process`)
  try {
    const output = await new Promise((resolve, reject) => {
      const child = spawn(process.execPath, ['.lidza/audit-performance.mjs'], {
        env: { ...process.env, AUDIT_ROUTES: JSON.stringify([route]), AUDIT_SAMPLES: String(samples), AUDIT_SIGN_IN: '0', AUDIT_LOGIN: '', AUDIT_STORAGE_STATE: '' },
        stdio: ['ignore', 'pipe', 'inherit'],
      })
      let stdout = ''
      child.stdout.on('data', chunk => { stdout += chunk })
      child.on('error', reject)
      child.on('close', code => code === 0 ? resolve(stdout) : reject(new Error(`runner exited ${code}`)))
    })
    const report = JSON.parse(output.trim().split('\n').at(-1))
    device = report.device
    llms = report.llms
    if (llms?.problem) faults.push(`${route}: llms.txt ${llms.problem}`)
    if (report.results?.length !== 1 || report.results[0].route !== route) throw new Error('runner omitted or substituted the route')
    const result = report.results[0]
    results.push(result)
    if (result.error) throw new Error(result.error)
    if (result.status !== 200 || result.samples !== samples) throw new Error('incomplete successful cold loads')
    const measured = { cls: result.cls, js: result.bytes?.script, css: result.bytes?.stylesheet, images: result.bytes?.image, total: result.bytes?.total }
    result.over = []
    for (const [name, limit] of Object.entries(budgets)) {
      if (!Number.isFinite(measured[name]) || measured[name] < 0) throw new Error(`missing ${name} measurement`)
      if (measured[name] > limit) result.over.push(`${name}: ${measured[name]} > ${limit}`)
    }
    faults.push(...result.over.map(message => `${route}: ${message}`))
  } catch (error) {
    faults.push(`${route}: ${error.message}`)
  }
}
const report = { status: faults.length ? 'fault' : 'ok', isolation: 'browser process per route', signedIn: false, device, budgets, llms, results, faults }
await writeFile('.lidza/audit-performance-isolated.json', JSON.stringify(report, null, 2) + '\n')
console.log(JSON.stringify(report))
if (faults.length) process.exitCode = 1
