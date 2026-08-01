/*
  错误日志告警 - 按密钥场景里"选一个用户"的下拉选择器。
  行为：
    - 打开输入框 / 输入 keyword → 拉一页；空 keyword 即列出全库用户
    - 下拉滚到底部 → 加载下一页并追加（无限滚动）
    - 已选中 → 显示 chip + 清除按钮
  接口：GET /api/error-log-alerts/lookup/users?keyword=&page=&page_size=
  返回：{ items: [{id,username,display_name}], page, page_size, total }

  并发/竞态防护：
    1. seqRef      每次发请求 seq++，响应回来对比 seqRef.current 不等就丢，防止旧词的响应污染新词的列表
    2. abortRef    保存"当前 in-flight 请求"的 AbortController；发新请求前 abort 上一个，节省带宽 + 双重防止过期响应
    3. loadingRef  同步 boolean，onScroll 密集触发时立即生效，防止 setLoading 异步导致同一 nextPage 被重复请求
*/
import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import axios from 'axios'

import { Input } from '@/components/ui/input'
import { api } from '@/lib/api'

interface UserOption {
  id: number
  username: string
  display_name?: string
}

const PAGE_SIZE = 20

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
  const [page, setPage] = useState(1)
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [open, setOpen] = useState(false)
  const containerRef = useRef<HTMLDivElement>(null)
  const listRef = useRef<HTMLDivElement>(null)

  const seqRef = useRef(0)
  const abortRef = useRef<AbortController | null>(null)
  const loadingRef = useRef(false)

  const hasMore = results.length < total

  // fetchPage 内部把并发/竞态一起管掉：抢占式 abort + 递增 seq + 同步 loadingRef。
  // 拒绝把这些散在调用方，否则每个调用点都要重复写一遍还容易漏。
  const fetchPage = useCallback((keyword: string, pageNum: number) => {
    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller
    const mySeq = ++seqRef.current

    loadingRef.current = true
    setLoading(true)

    return api
      .get('/api/error-log-alerts/lookup/users', {
        params: { keyword, page: pageNum, page_size: PAGE_SIZE },
        signal: controller.signal,
        // 每次都是新请求，不能命中 GET dedup（会共享上一个词的 Promise）
        disableDuplicate: true,
        // AbortController 主动取消是常规行为，不让全局 interceptor 弹"canceled"
        skipErrorHandler: true,
      })
      .then((res) => {
        if (mySeq !== seqRef.current) return // 过期响应，丢
        const data = res.data?.data ?? {}
        const items: UserOption[] = data.items ?? []
        const totalNum: number = data.total ?? 0
        setResults((prev) => (pageNum === 1 ? items : [...prev, ...items]))
        setTotal(totalNum)
      })
      .catch((err) => {
        if (axios.isCancel(err)) return
        if (mySeq !== seqRef.current) return
        if (pageNum === 1) {
          setResults([])
          setTotal(0)
        }
      })
      .finally(() => {
        // 只有"我"还是最新请求时才 reset loading，避免过期请求把新请求的 loading 状态误关
        if (mySeq === seqRef.current) {
          loadingRef.current = false
          setLoading(false)
        }
      })
  }, [])

  // keyword 变化 / 首次打开：防抖 300ms 拉首页
  useEffect(() => {
    if (!open) return
    const timer = setTimeout(() => {
      setPage(1)
      fetchPage(query.trim(), 1)
    }, 300)
    return () => clearTimeout(timer)
    // fetchPage 里已经 abort + seq 双保险，cleanup 只清 timer 即可
  }, [query, open, fetchPage])

  // 滚动到底：加载下一页
  // loadingRef 同步判断，避免 setLoading 未 flush 时同一 nextPage 被重复请求
  const onScroll = useCallback(
    (e: React.UIEvent<HTMLDivElement>) => {
      if (loadingRef.current || !hasMore) return
      const el = e.currentTarget
      // 距底 <= 24px 触发，避免必须"严丝合缝滚到底"才响应
      if (el.scrollHeight - el.scrollTop - el.clientHeight <= 24) {
        const nextPage = page + 1
        setPage(nextPage)
        fetchPage(query.trim(), nextPage)
      }
    },
    [hasMore, page, query, fetchPage]
  )

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
    setTotal(0)
    setOpen(false)
  }

  const clear = () => {
    onChange(null, '')
    setQuery('')
    setResults([])
    setTotal(0)
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
          {open && (
            <div
              ref={listRef}
              onScroll={onScroll}
              className='absolute left-0 top-full z-20 mt-1 max-h-48 w-full overflow-y-auto rounded border bg-popover shadow'
            >
              {results.length === 0 && !loading ? (
                <div className='p-2 text-xs text-muted-foreground'>
                  {t('No matching users')}
                </div>
              ) : (
                <>
                  {results.map((u) => (
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
                  ))}
                  {loading && (
                    <div className='p-2 text-xs text-muted-foreground'>
                      {t('Loading')}…
                    </div>
                  )}
                  {!loading && !hasMore && results.length > 0 && (
                    <div className='p-2 text-center text-xs text-muted-foreground'>
                      {t('No more users')}
                    </div>
                  )}
                </>
              )}
            </div>
          )}
        </>
      )}
    </div>
  )
}
