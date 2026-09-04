// Turns the built page into HTML files, one per route.
//
// WHY, in one measurement: before this, a fetch of https://alpaca.swiftward.dev/
// returned 56 characters of text. Everything a reader came for was assembled by
// JavaScript afterwards. A person's browser runs it; a judge's agent, asked to
// "study this project", usually does not - it fetches the address and reads what
// comes back. So the page arrived empty at exactly the reader we most wanted it
// to reach.
//
// This runs after `vite build`: it renders each route with React on this machine,
// at build time, and writes the result into the template Vite produced. Nothing
// renders at request time - the server still hands over a file, and the file is
// now the finished page. The browser hydrates it and behaves as before.
//
// The live account is the one thing that cannot be baked in: /live is rendered in
// its loading state, so what an agent reads there is the page's structure and its
// prose, and the numbers come from `/api/*`, which llms.txt points at.
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { dirname, join } from 'node:path'

import { PAGES, render } from './dist-ssr/prerender.js'

const SITE = 'https://alpaca.swiftward.dev'
const OUT = 'dist'

const template = await readFile(join(OUT, 'index.html'), 'utf8')

// One machine-readable statement of what this is, on every page. An agent that
// understands nothing else about the markup can still read this.
const structured = (page) =>
  JSON.stringify({
    '@context': 'https://schema.org',
    '@type': 'SoftwareSourceCode',
    name: 'Swiftward Alpaca options agent',
    description: page.says,
    url: SITE + page.path,
    codeRepository: 'https://github.com/swiftward/swiftward-alpaca-options-agents',
    license: 'https://opensource.org/licenses/MIT',
    programmingLanguage: ['Go', 'TypeScript'],
    applicationCategory: 'Autonomous options trading agent',
    author: { '@type': 'Organization', name: 'Swiftward', url: 'https://swiftward.dev' },
  })

const head = (page) => `
    <link rel="canonical" href="${SITE}${page.path}" />
    <meta property="og:type" content="website" />
    <meta property="og:site_name" content="Swiftward Alpaca" />
    <meta property="og:url" content="${SITE}${page.path}" />
    <meta property="og:title" content="${escaped(page.title)}" />
    <meta property="og:description" content="${escaped(page.says)}" />
    <meta property="og:image" content="${SITE}/swiftward-mark.png" />
    <meta name="twitter:card" content="summary" />
    <script type="application/ld+json">${structured(page)}</script>
  `

for (const page of PAGES) {
  const html = template
    .replace(/<title>[^<]*<\/title>/, `<title>${escaped(page.title)}</title>`)
    .replace(/<meta name="description" content="[^"]*" \/>/, description(page))
    .replace('</head>', `${head(page)}</head>`)
    .replace('<div id="root"></div>', `<div id="root">${render(page.path)}</div>`)

  // "/" is the site's own index.html; every other route becomes a directory with
  // one inside it, which is the shape a file server answers without a redirect.
  const name = page.path === '/' ? 'index.html' : join(page.path.slice(1), 'index.html')
  await mkdir(dirname(join(OUT, name)), { recursive: true })
  await writeFile(join(OUT, name), html)
  console.log(`  ${page.path} -> ${name} (${(html.length / 1024).toFixed(1)} KB)`)
}

function description(page) {
  return `<meta name="description" content="${escaped(page.says)}" />`
}

// The four that matter inside an attribute. The text is ours and holds no markup,
// but a title written later might, and a broken tag is not the way to find out.
function escaped(text) {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}
