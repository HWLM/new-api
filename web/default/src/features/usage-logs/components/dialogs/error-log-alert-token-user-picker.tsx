/*
  错误日志告警 - 按密钥场景里"选一个用户"的搜索型下拉。
  行为：
    - 未选中时：输入框；打字防抖 300ms → 调 /lookup/users?keyword=... （后端 SearchUsers 已同时匹配 username / email / display_name / id）
    - 已选中时：显示 chip + 清除按钮（清除后回到搜索态）
  仅返回 user_id 和 username，父组件用来 loadTokensFor 联动密钥列表。
*/
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import axios from 'axios'

import { Input } from '@/components/ui/input'
import { api } from '@/lib/api'

interface UserOption {
  id: number
  username: string
  display_name?: string
}

export function ErrorLogAlertTokenUserPicker({
  value,
  valueLabel,
  onChange,
}: {
  value: number | null
  valueLabel: string
  onChange: (userId: number | null, username: string) => void
}) {
  const { t } = useTranslation()
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<UserOption[]>([])
  const [loading, setLoading] = useState(false)
  const [open, setOpen] = useState(false)
  const containerRef = useRef<HTMLDivElement>(null)

  // 防抖搜索 + 请求取消：
  // 快速改词时旧请求会被 AbortController 取消，避免"旧请求后返回覆盖新结果"，
  // 防止管理员看到与当前输入词不符的下拉项而选错人。
  useEffect(() => {
    const q = query.trim()
    if (!q) {
      setResults([])
      return
    }
    setLoading(true)
    const controller = new AbortController()
    const timer = setTimeout(() => {
      api
        .get('/api/error-log-alerts/lookup/users', {
          params: { keyword: q, limit: 50 },
          signal: controller.signal,
          // 关掉 GET 去重：搜索链路希望"每次新词都是新请求"，命中 dedup 会共享
          // 上一个词的 Promise，abort 时相互干扰。
          disableDuplicate: true,
          // 关掉全局 toast：搜索请求被 AbortController 主动取消是常规行为，
          // 不能让全局 interceptor 弹"canceled"红条。真正的错误交给下面的 catch。
          skipErrorHandler: true,
        })
        .then((res) => setResults(res.data?.data ?? []))
        .catch((err) => {
          if (axios.isCancel(err)) return
          setResults([])
        })
        .finally(() => setLoading(false))
    }, 300)
    return () => {
      clearTimeout(timer)
      controller.abort()
    }
  }, [query])

  // 点外面收起下拉
  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      if (
        containerRef.current &&
        !containerRef.current.contains(e.target as Node)
      ) {
        setOpen(false)
      }
    }
    window.addEventListener('mousedown', handler)
    return () => window.removeEventListener('mousedown', handler)
  }, [open])

  const pick = (u: UserOption) => {
    onChange(u.id, u.username)
    setQuery('')
    setResults([])
    setOpen(false)
  }

  const clear = () => {
    onChange(null, '')
    setQuery('')
    setResults([])
    setOpen(false)
  }

  return (
    <div ref={containerRef} className='relative w-48'>
      {value != null ? (
        <div className='flex h-9 items-center justify-between rounded border bg-background px-2 text-sm'>
          <span className='truncate' title={valueLabel}>
            {valueLabel || `#${value}`}
          </span>
          <button
            type='button'
            className='ms-1 shrink-0 text-muted-foreground hover:text-destructive'
            onClick={clear}
            aria-label='clear'
          >
            ×
          </button>
        </div>
      ) : (
        <>
          <Input
            className='h-9 text-sm'
            placeholder={t('Search by username or display name')}
            value={query}
            onChange={(e) => {
              setQuery(e.target.value)
              setOpen(true)
            }}
            onFocus={() => setOpen(true)}
          />
          {open && query.trim() !== '' && (
            <div className='absolute left-0 top-full z-20 mt-1 max-h-48 w-full overflow-y-auto rounded border bg-popover shadow'>
              {loading ? (
                <div className='p-2 text-xs text-muted-foreground'>
                  {t('Loading')}…
                </div>
              ) : results.length === 0 ? (
                <div className='p-2 text-xs text-muted-foreground'>
                  {t('No matching users')}
                </div>
              ) : (
                results.map((u) => (
                  <div
                    key={u.id}
                    className='cursor-pointer px-2 py-1 text-sm hover:bg-muted'
                    onClick={() => pick(u)}
                  >
                    {u.username}
                    {u.display_name ? (
                      <span className='ms-1 text-xs text-muted-foreground'>
                        ({u.display_name})
                      </span>
                    ) : null}
                  </div>
                ))
              )}
            </div>
          )}
        </>
      )}
    </div>
  )
}
