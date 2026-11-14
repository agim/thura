// Writes dist/sw.js from scripts/sw.js after the build: the precache list
// (the app entry's hashed script and styles with their static imports, the
// offline page and the icons) and a version that changes with any of them,
// so a deploy installs a new worker and the page offers to reload. Run by
// `npm run build`, after the prerender.
import { createHash } from 'node:crypto'
import { existsSync, readFileSync, readdirSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'

const dist = 'dist'
const manifest = JSON.parse(readFileSync(join(dist, '.vite', 'manifest.json'), 'utf8'))
const entry = Object.keys(manifest).find(key => manifest[key].isEntry && key === 'index.html')
if (!entry) throw new Error('service worker: no index.html entry in dist/.vite/manifest.json')

const files = new Set()
const visit = (key, seen = new Set()) => {
  if (seen.has(key)) return
  seen.add(key)
  const chunk = manifest[key]
  if (!chunk) throw new Error(`service worker: missing chunk ${key}`)
  files.add('/' + chunk.file)
  for (const css of chunk.css ?? []) files.add('/' + css)
  for (const dependency of chunk.imports ?? []) visit(dependency, seen)
}
visit(entry)

for (const file of ['offline.html', 'offline.css', 'offline.js', 'manifest.webmanifest']) {
  if (!existsSync(join(dist, file))) throw new Error(`service worker: dist/${file} is missing (public/${file})`)
  files.add('/' + file)
}
for (const icon of readdirSync(join(dist, 'icons'))) files.add('/icons/' + icon)

const precache = [...files].sort()
const template = readFileSync('scripts/sw.js', 'utf8')
const hash = createHash('sha256').update(template)
for (const file of precache) hash.update(file).update(readFileSync(join(dist, file.slice(1))))
const version = hash.digest('hex').slice(0, 16)

const out = template.replace("'__VERSION__'", JSON.stringify(version)).replace('__PRECACHE__', JSON.stringify(precache))
writeFileSync(join(dist, 'sw.js'), out)
console.log(`service worker: ${precache.length} files precached, version ${version}`)
