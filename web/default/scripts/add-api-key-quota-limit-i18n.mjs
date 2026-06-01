import fs from 'node:fs/promises'
import path from 'node:path'

const LOCALES_DIR = path.resolve('src/i18n/locales')

const newKeys = {
  en: {
    'Daily Quota Limit ({{currency}})': 'Daily Quota Limit ({{currency}})',
    'Quota Limits': 'Quota Limits',
    'View limits': 'View limits',
    'Weekly Quota Limit ({{currency}})': 'Weekly Quota Limit ({{currency}})',
  },
  zh: {
    'Daily Quota Limit ({{currency}})': '每日额度限制（{{currency}}）',
    'Quota Limits': '额度限制',
    'View limits': '查看限制',
    'Weekly Quota Limit ({{currency}})': '每周额度限制（{{currency}}）',
  },
  fr: {
    'Daily Quota Limit ({{currency}})': 'Limite de quota quotidienne ({{currency}})',
    'Quota Limits': 'Limites de quota',
    'View limits': 'Voir les limites',
    'Weekly Quota Limit ({{currency}})': 'Limite de quota hebdomadaire ({{currency}})',
  },
  ja: {
    'Daily Quota Limit ({{currency}})': '1日のクォータ上限 ({{currency}})',
    'Quota Limits': 'クォータ上限',
    'View limits': '上限を表示',
    'Weekly Quota Limit ({{currency}})': '1週間のクォータ上限 ({{currency}})',
  },
  ru: {
    'Daily Quota Limit ({{currency}})': 'Дневной лимит квоты ({{currency}})',
    'Quota Limits': 'Лимиты квоты',
    'View limits': 'Показать лимиты',
    'Weekly Quota Limit ({{currency}})': 'Недельный лимит квоты ({{currency}})',
  },
  vi: {
    'Daily Quota Limit ({{currency}})': 'Giới hạn hạn mức hàng ngày ({{currency}})',
    'Quota Limits': 'Giới hạn hạn mức',
    'View limits': 'Xem giới hạn',
    'Weekly Quota Limit ({{currency}})': 'Giới hạn hạn mức hàng tuần ({{currency}})',
  },
}

function stableStringify(obj) {
  return JSON.stringify(obj, null, 2) + '\n'
}

for (const [locale, trans] of Object.entries(newKeys)) {
  const filePath = path.join(LOCALES_DIR, `${locale}.json`)
  const json = JSON.parse(await fs.readFile(filePath, 'utf8'))
  json.translation = {
    ...json.translation,
    ...trans,
  }
  json.translation = Object.fromEntries(
    Object.entries(json.translation).sort(([a], [b]) => a.localeCompare(b))
  )
  await fs.writeFile(filePath, stableStringify(json), 'utf8')
}
