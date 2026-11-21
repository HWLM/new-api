/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'

import type { GroupRateLimitRow } from '../lib/group-rate-limits'

/**
 * 用户编辑「最大并发请求数」下方的分组速率表格（§21.3）。
 * 每行：分组（系统已配置的分组名）、并发上限（不单独限 / 不限 / 自定义）、是否走兜底。
 */
export function GroupRateLimitsEditor(props: {
  value: GroupRateLimitRow[]
  onChange: (rows: GroupRateLimitRow[]) => void
  groups: string[]
}) {
  const { t } = useTranslation()
  const rows = props.value
  const used = new Set(rows.map((r) => r.group))
  const available = props.groups.filter((g) => !used.has(g))

  const update = (index: number, patch: Partial<GroupRateLimitRow>) => {
    props.onChange(rows.map((r, i) => (i === index ? { ...r, ...patch } : r)))
  }

  const add = () => {
    props.onChange([
      ...rows,
      { group: available[0] ?? '', limit: 0, fallback: true },
    ])
  }

  return (
    <div className='flex flex-col gap-2'>
      {rows.length === 0 ? (
        <p className='text-muted-foreground text-xs'>
          {t(
            'No group limits: every group is only bound by the total concurrency above.'
          )}
        </p>
      ) : (
        <div className='flex flex-col gap-2'>
          <div className='text-muted-foreground hidden grid-cols-[minmax(8rem,1fr)_auto_auto_auto] gap-2 text-xs md:grid'>
            <span>{t('Group')}</span>
            <span>{t('Concurrency limit')}</span>
            <span>{t('Use fallback channel')}</span>
            <span />
          </div>
          {rows.map((row, index) => {
            const options = [
              ...(row.group && !props.groups.includes(row.group)
                ? [{ value: row.group, label: row.group }]
                : []),
              ...props.groups
                .filter((g) => g === row.group || !used.has(g))
                .map((g) => ({ value: g, label: g })),
            ]
            return (
              <div
                key={row.group || 'unselected'}
                className='grid grid-cols-1 items-center gap-2 rounded-md border p-2 md:grid-cols-[minmax(8rem,1fr)_auto_auto_auto] md:border-0 md:p-0'
              >
                <Combobox
                  options={options}
                  value={row.group}
                  onValueChange={(value) =>
                    update(index, { group: value ?? '' })
                  }
                  placeholder={t('Select a group')}
                  searchPlaceholder={t('Search groups...')}
                  emptyText={t('No group found.')}
                />
                <div className='flex items-center gap-1'>
                  <Button
                    type='button'
                    size='sm'
                    variant={row.limit === 0 ? 'default' : 'outline'}
                    onClick={() => update(index, { limit: 0 })}
                    title={t('Only the total concurrency applies')}
                  >
                    {t('No separate limit')}
                  </Button>
                  <Button
                    type='button'
                    size='sm'
                    variant={row.limit < 0 ? 'default' : 'outline'}
                    onClick={() => update(index, { limit: -1 })}
                  >
                    {t('Unlimited')}
                  </Button>
                  <Input
                    type='number'
                    min={1}
                    className='w-24'
                    value={row.limit > 0 ? row.limit : ''}
                    placeholder={t('Custom limit')}
                    onChange={(e) => {
                      const n = Number.parseInt(e.target.value, 10)
                      update(index, {
                        limit: Number.isFinite(n) && n > 0 ? n : 0,
                      })
                    }}
                  />
                </div>
                <label
                  className='flex items-center gap-2 text-sm'
                  title={
                    row.fallback
                      ? t(
                          "On: hop once to the fallback channel of the request's own channel (429 if none usable)"
                        )
                      : t(
                          'Off: 429 rate_limit_exceeded as soon as the group is full'
                        )
                  }
                >
                  <Switch
                    checked={row.fallback}
                    onCheckedChange={(checked) =>
                      update(index, { fallback: checked })
                    }
                  />
                  <span className='md:hidden'>{t('Use fallback channel')}</span>
                  {row.fallback ? null : (
                    <span className='text-destructive text-xs'>
                      {t('429 when full')}
                    </span>
                  )}
                </label>
                <Button
                  type='button'
                  size='icon'
                  variant='ghost'
                  className='text-destructive size-8'
                  onClick={() =>
                    props.onChange(rows.filter((_, i) => i !== index))
                  }
                  aria-label={t('Delete')}
                  title={t('Delete')}
                >
                  <Trash2 className='size-4' />
                </Button>
              </div>
            )
          })}
        </div>
      )}
      <div>
        <Button
          type='button'
          size='sm'
          variant='outline'
          onClick={add}
          disabled={available.length === 0}
        >
          <Plus className='size-4' />
          {t('Add group limit')}
        </Button>
      </div>
    </div>
  )
}
