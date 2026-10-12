/*
Copyright (C) 2023-2026 QuantumNous

For commercial licensing, please contact support@quantumnous.com
*/
import { useTranslation } from 'react-i18next'
import { Skeleton } from '@/components/ui/skeleton'
import { TableCell, TableRow } from '@/components/ui/table'

// 骨架行用固定的 key，避免用数组下标做 key
const SKELETON_ROW_KEYS = ['s1', 's2', 's3', 's4', 's5']

/**
 * 表格的「加载中 / 空数据」占位行。
 * 三张表（汇总、明细、操作记录）共用，避免每张表各写一份嵌套三元。
 */
export function TablePlaceholderRows({
  isLoading,
  colSpan,
  rowCount = 5,
}: {
  isLoading: boolean
  colSpan: number
  rowCount?: number
}) {
  const { t } = useTranslation()

  if (isLoading) {
    return (
      <>
        {SKELETON_ROW_KEYS.slice(0, rowCount).map((key) => (
          <TableRow key={key}>
            <TableCell colSpan={colSpan}>
              <Skeleton className='h-5 w-full' />
            </TableCell>
          </TableRow>
        ))}
      </>
    )
  }

  return (
    <TableRow>
      <TableCell colSpan={colSpan} className='text-muted-foreground text-center'>
        {t('No data')}
      </TableCell>
    </TableRow>
  )
}
