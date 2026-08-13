#!/usr/bin/env node
import fs from 'node:fs/promises'
import path from 'node:path'
import process from 'node:process'
import net from 'node:net'
import { createRequire } from 'node:module'

const DEFAULT_SKILL_DIR =
  'C:/Users/Administrator/.codex/skills/sub2api-request-investigator'
const DEFAULT_TEST_ENV = path.resolve('.env')

const OPTION_KEYS = [
  'GroupRatio',
  'GroupGroupRatio',
  'UserUsableGroups',
  'AutoGroups',
  'DefaultUseAutoGroup',
  'DisplayUserSelfGroup',
  'TopupGroupRatio',
  'ModelRequestRateLimitGroup',

  'ModelRatio',
  'ModelPrice',
  'CacheRatio',
  'CreateCacheRatio',
  'CompletionRatio',
  'ImageRatio',
  'AudioRatio',
  'AudioCompletionRatio',
  'ExposeRatioEnabled',
  'PricingDiscountColumnEnabled',
  'HeaderNavModules',

  'Price',
  'USDExchangeRate',
  'ConsumeUSDExchangeRate',

  'billing_setting.billing_mode',
  'billing_setting.billing_expr',
  'group_ratio_setting.group_special_usable_group',
  'group_ratio_setting.user_group_visible_groups',
  'tool_price_setting.prices',
]

const PRICE_ONLY_KEYS = new Set([
  'ModelRatio',
  'ModelPrice',
  'CacheRatio',
  'CreateCacheRatio',
  'CompletionRatio',
  'ImageRatio',
  'AudioRatio',
  'AudioCompletionRatio',
  'ExposeRatioEnabled',
  'PricingDiscountColumnEnabled',
  'HeaderNavModules',
  'Price',
  'USDExchangeRate',
  'ConsumeUSDExchangeRate',
  'billing_setting.billing_mode',
  'billing_setting.billing_expr',
  'tool_price_setting.prices',
])

function parseArgs(argv) {
  const args = {
    apply: false,
    includeAbilities: false,
    pricesOnly: false,
    showOnModelSquare: true,
    backupDir: path.resolve('backups', 'newapi-imports'),
    prodConfig: path.join(DEFAULT_SKILL_DIR, 'config', 'config.json'),
    skillDir: DEFAULT_SKILL_DIR,
    testEnv: DEFAULT_TEST_ENV,
  }

  for (let i = 0; i < argv.length; i += 1) {
    const token = argv[i]
    if (token === '--apply') {
      args.apply = true
      continue
    }
    if (token === '--include-abilities') {
      args.includeAbilities = true
      continue
    }
    if (token === '--prices-only') {
      args.pricesOnly = true
      continue
    }
    if (token === '--show-on-model-square') {
      args.showOnModelSquare = true
      continue
    }
    if (token === '--no-show-on-model-square') {
      args.showOnModelSquare = false
      continue
    }
    if (token === '--help' || token === '-h') {
      args.help = true
      continue
    }

    const valueFlags = new Set([
      '--prod-config',
      '--skill-dir',
      '--test-env',
      '--test-dsn',
      '--backup-dir',
    ])
    if (!valueFlags.has(token)) {
      throw new Error(`Unknown argument: ${token}`)
    }
    const value = argv[i + 1]
    if (!value || value.startsWith('--')) {
      throw new Error(`${token} requires a value`)
    }
    i += 1
    if (token === '--prod-config') args.prodConfig = path.resolve(value)
    if (token === '--skill-dir') args.skillDir = path.resolve(value)
    if (token === '--test-env') args.testEnv = path.resolve(value)
    if (token === '--test-dsn') args.testDsn = value
    if (token === '--backup-dir') args.backupDir = path.resolve(value)
  }

  return args
}

function usage() {
  return `
Import production new-api group/model/pricing data into test new-api.

Default behavior: dry-run only; no writes.

Usage:
  node scripts/import-prod-model-groups-to-test.mjs
  node scripts/import-prod-model-groups-to-test.mjs --apply
  node scripts/import-prod-model-groups-to-test.mjs --apply --include-abilities
  node scripts/import-prod-model-groups-to-test.mjs --apply --prices-only
  node scripts/import-prod-model-groups-to-test.mjs --apply --no-show-on-model-square

Options:
  --apply                    Write into test DB. Without it, only preview counts.
  --include-abilities        Sync abilities table. Use only when prod/test channel_id values match.
  --prices-only              Sync only model pricing/dynamic billing options; do not sync vendors/models/prefill_groups.
  --show-on-model-square     Force-enable model square navigation and pricing display. Enabled by default.
  --no-show-on-model-square  Do not change model square visibility/display options.
  --prod-config <path>       Production connection config. Defaults to the sub2api-request-investigator skill config.
  --skill-dir <path>         Skill directory used to reuse pg/ssh2 dependencies.
  --test-env <path>          Test .env file. Defaults to this project .env.
  --test-dsn <dsn>           Test DB DSN. If omitted, reads SQL_DSN from --test-env.
  --backup-dir <path>        Backup directory. Defaults to backups/newapi-imports.
`
}

async function readJson(filePath) {
  const raw = await fs.readFile(filePath, 'utf8')
  return JSON.parse(raw.replace(/,(\s*[}\]])/g, '$1'))
}

async function readTestDsn(args) {
  if (args.testDsn) return args.testDsn
  const raw = await fs.readFile(args.testEnv, 'utf8')
  const line = raw
    .split(/\r?\n/)
    .find((item) => item.trim().startsWith('SQL_DSN='))
  if (!line) throw new Error(`SQL_DSN not found in ${args.testEnv}`)
  return line.slice(line.indexOf('=') + 1).trim()
}

function maskDsn(dsn) {
  return dsn
    .replace(/([^:/@\s]+):([^@\s]+)@/, '$1:<secret>@')
    .replace(/(password|passwd|pwd)=([^&\s]+)/gi, '$1=<secret>')
}

function validateSshTunnelConfig(tunnel) {
  if (!tunnel || tunnel.enabled !== true) return null
  for (const key of ['host', 'username', 'password', 'remote_host']) {
    if (!tunnel[key]) throw new Error(`ssh_tunnel.${key} is required`)
  }
  return {
    host: String(tunnel.host),
    port: Number(tunnel.port || 22),
    username: String(tunnel.username),
    password: String(tunnel.password),
    localHost: String(tunnel.local_host || '127.0.0.1'),
    localPort: Number(tunnel.local_port || 15432),
    remoteHost: String(tunnel.remote_host),
    remotePort: Number(tunnel.remote_port || 5432),
    readyTimeout: Number(tunnel.ready_timeout_ms || 20000),
  }
}

async function openSshTunnel(tunnel, SSHClient) {
  const cfg = validateSshTunnelConfig(tunnel)
  if (!cfg) return async () => {}

  const ssh = new SSHClient()
  let server
  let closed = false

  const cleanup = async () => {
    if (closed) return
    closed = true
    await new Promise((resolve) => {
      if (!server) return resolve()
      server.close(() => resolve())
    }).catch(() => {})
    ssh.end()
  }

  await new Promise((resolve, reject) => {
    const fail = async (error) => {
      await cleanup()
      reject(error)
    }

    ssh
      .on('ready', () => {
        server = net.createServer((socket) => {
          ssh.forwardOut(
            socket.remoteAddress || cfg.localHost,
            socket.remotePort || 0,
            cfg.remoteHost,
            cfg.remotePort,
            (err, stream) => {
              if (err) {
                socket.destroy(err)
                return
              }
              socket.pipe(stream).pipe(socket)
            }
          )
        })

        server.on('error', fail)
        server.listen(cfg.localPort, cfg.localHost, () => {
          console.log(
            `SSH tunnel ready: ${cfg.localHost}:${cfg.localPort} -> ${cfg.remoteHost}:${cfg.remotePort}`
          )
          resolve()
        })
      })
      .on('error', fail)
      .connect({
        host: cfg.host,
        port: cfg.port,
        username: cfg.username,
        password: cfg.password,
        readyTimeout: cfg.readyTimeout,
      })
  })

  return cleanup
}

async function connectPg(Client, connectionString, label) {
  const client = new Client({ connectionString })
  await client.connect()
  const result = await client.query('SELECT current_database() AS db')
  console.log(`${label} connected: ${result.rows[0]?.db || 'unknown'}`)
  return client
}

async function tableExists(client, tableName) {
  const result = await client.query(
    `SELECT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = 'public' AND table_name = $1
    ) AS exists`,
    [tableName]
  )
  return result.rows[0]?.exists === true
}

async function readRows(client, tableName, orderBy) {
  if (!(await tableExists(client, tableName))) return []
  const result = await client.query(`SELECT * FROM ${tableName} ${orderBy}`)
  return result.rows
}

async function readOptions(client) {
  const result = await client.query(
    'SELECT key, value FROM options WHERE key = ANY($1::text[]) ORDER BY key',
    [OPTION_KEYS]
  )
  return result.rows
}

async function getCounts(client, includeAbilities) {
  const tables = ['vendors', 'models', 'prefill_groups']
  if (includeAbilities) tables.push('abilities')
  const counts = {}
  for (const table of tables) {
    if (!(await tableExists(client, table))) {
      counts[table] = 'missing'
      continue
    }
    const result = await client.query(
      `SELECT count(*)::bigint AS rows FROM ${table}`
    )
    counts[table] = result.rows[0].rows
  }
  const optionResult = await client.query(
    'SELECT count(*)::bigint AS rows FROM options WHERE key = ANY($1::text[])',
    [OPTION_KEYS]
  )
  counts.selected_options = optionResult.rows[0].rows
  return counts
}

function upsertOptionRow(rows, key, value) {
  const existing = rows.find((row) => row.key === key)
  if (existing) {
    existing.value = value
  } else {
    rows.push({ key, value })
  }
}

function forceModelSquareOptions(rows) {
  upsertOptionRow(rows, 'ExposeRatioEnabled', 'true')
  upsertOptionRow(rows, 'PricingDiscountColumnEnabled', 'true')

  const fallback = {
    home: true,
    console: true,
    pricing: { enabled: true, requireAuth: false },
    rankings: { enabled: true, requireAuth: false },
    docs: true,
    about: true,
  }

  const row = rows.find((item) => item.key === 'HeaderNavModules')
  let nav = fallback
  if (row?.value) {
    try {
      nav = { ...fallback, ...JSON.parse(row.value) }
    } catch {
      nav = fallback
    }
  }
  nav.pricing = {
    ...(typeof nav.pricing === 'object' && nav.pricing ? nav.pricing : {}),
    enabled: true,
    requireAuth: false,
  }
  upsertOptionRow(rows, 'HeaderNavModules', JSON.stringify(nav))
}

function getImportOptionRows(rows, args) {
  let next = args.pricesOnly
    ? rows.filter((row) => PRICE_ONLY_KEYS.has(row.key))
    : [...rows]
  if (args.showOnModelSquare) {
    next = next.map((row) => ({ ...row }))
    forceModelSquareOptions(next)
  }
  return next
}

async function backupTestData(client, args) {
  await fs.mkdir(args.backupDir, { recursive: true })
  const stamp = new Date().toISOString().replace(/[:.]/g, '-')
  const backupPath = path.join(
    args.backupDir,
    `test-before-model-group-import-${stamp}.json`
  )
  const payload = {
    created_at: new Date().toISOString(),
    options: await readOptions(client),
    vendors: await readRows(client, 'vendors', 'ORDER BY id'),
    models: await readRows(client, 'models', 'ORDER BY id'),
    prefill_groups: await readRows(client, 'prefill_groups', 'ORDER BY id'),
    abilities: args.includeAbilities
      ? await readRows(client, 'abilities', 'ORDER BY channel_id, "group", model')
      : undefined,
  }
  await fs.writeFile(backupPath, JSON.stringify(payload, null, 2), 'utf8')
  console.log(`test backup written: ${backupPath}`)
}

async function replaceOptions(client, rows) {
  if (rows.length === 0) return
  const values = []
  const placeholders = rows.map((row, index) => {
    const base = index * 2
    values.push(row.key, row.value)
    return `($${base + 1}, $${base + 2})`
  })
  await client.query(
    `INSERT INTO options (key, value)
     VALUES ${placeholders.join(', ')}
     ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`,
    values
  )
}

async function replaceTable(client, tableName, rows, options = {}) {
  if (!(await tableExists(client, tableName))) {
    console.log(`skip ${tableName}: table does not exist in test DB`)
    return
  }

  await client.query(`DELETE FROM ${tableName}`)
  if (rows.length === 0) return

  const columns = Object.keys(rows[0])
  const values = []
  const placeholders = rows.map((row, rowIndex) => {
    const cols = columns.map((col, colIndex) => {
      values.push(row[col])
      return `$${rowIndex * columns.length + colIndex + 1}`
    })
    return `(${cols.join(', ')})`
  })
  const quotedColumns = columns.map((col) => `"${col}"`).join(', ')
  await client.query(
    `INSERT INTO ${tableName} (${quotedColumns}) VALUES ${placeholders.join(', ')}`,
    values
  )

  if (options.sequenceColumn && columns.includes(options.sequenceColumn)) {
    await client.query(
      `SELECT setval(
        pg_get_serial_sequence($1, $2),
        COALESCE((SELECT MAX("${options.sequenceColumn}") FROM ${tableName}), 1),
        true
      )`,
      [tableName, options.sequenceColumn]
    )
  }
}

async function main() {
  const args = parseArgs(process.argv.slice(2))
  if (args.help) {
    console.log(usage())
    return
  }

  const skillRequire = createRequire(path.join(args.skillDir, 'package.json'))
  const { Client } = skillRequire('pg')
  const { Client: SSHClient } = skillRequire('ssh2')

  const prodConfig = await readJson(args.prodConfig)
  const prodDsn = prodConfig.newapi_dsn
  if (!prodDsn) throw new Error(`newapi_dsn not found in ${args.prodConfig}`)
  const testDsn = await readTestDsn(args)

  console.log(`prod DB: ${maskDsn(prodDsn)}`)
  console.log(`test DB: ${maskDsn(testDsn)}`)
  console.log(`mode: ${args.apply ? 'APPLY' : 'DRY-RUN'}`)
  console.log(`sync abilities: ${args.includeAbilities ? 'yes' : 'no'}`)
  console.log(`prices only: ${args.pricesOnly ? 'yes' : 'no'}`)
  console.log(`show on model square: ${args.showOnModelSquare ? 'yes' : 'no'}`)

  let closeTunnel = async () => {}
  let prod
  let test
  try {
    closeTunnel = await openSshTunnel(prodConfig.ssh_tunnel, SSHClient)
    prod = await connectPg(Client, prodDsn, 'prod')
    test = await connectPg(Client, testDsn, 'test')

    const prodCounts = await getCounts(prod, args.includeAbilities)
    const testCounts = await getCounts(test, args.includeAbilities)
    console.log('prod counts:')
    console.table(prodCounts)
    console.log('test current counts:')
    console.table(testCounts)

    if (!args.apply) {
      console.log('dry-run complete. Add --apply to write into test DB.')
      return
    }

    const prodData = {
      options: getImportOptionRows(await readOptions(prod), args),
      vendors: await readRows(prod, 'vendors', 'ORDER BY id'),
      models: await readRows(prod, 'models', 'ORDER BY id'),
      prefill_groups: await readRows(prod, 'prefill_groups', 'ORDER BY id'),
      abilities: args.includeAbilities
        ? await readRows(prod, 'abilities', 'ORDER BY channel_id, "group", model')
        : [],
    }

    await backupTestData(test, args)
    await test.query('BEGIN')
    try {
      await replaceOptions(test, prodData.options)
      if (!args.pricesOnly) {
        await replaceTable(test, 'vendors', prodData.vendors, {
          sequenceColumn: 'id',
        })
        await replaceTable(test, 'models', prodData.models, {
          sequenceColumn: 'id',
        })
        await replaceTable(test, 'prefill_groups', prodData.prefill_groups, {
          sequenceColumn: 'id',
        })
        if (args.includeAbilities) {
          await replaceTable(test, 'abilities', prodData.abilities)
        }
      }
      await test.query('COMMIT')
    } catch (error) {
      await test.query('ROLLBACK')
      throw error
    }

    const finalCounts = await getCounts(test, args.includeAbilities)
    console.log('import complete. test final counts:')
    console.table(finalCounts)
  } finally {
    if (test) await test.end().catch(() => {})
    if (prod) await prod.end().catch(() => {})
    await closeTunnel()
  }
}

main().catch((error) => {
  console.error(error)
  process.exitCode = 1
})
