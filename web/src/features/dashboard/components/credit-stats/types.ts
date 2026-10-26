/*
Copyright (C) 2023-2026 QuantumNous

For commercial licensing, please contact support@quantumnous.com
*/

// 后端 controller/credit_stats.go 的响应类型对应。
// 所有金额单位都是人民币 ¥（管理员页面录入即是 ¥）。

// 顶部 7 张卡片
export type CreditSummary = {
  recharge_amount: number
  gift_amount: number
  credit_amount: number
  repayment_amount: number
  credit_consumption: number
  receivable: number
  outstanding_credit: number
}

// 趋势图：天 / 月两种粒度，一次只取一个金额维度
export type CreditTrendGranularity = 'day' | 'month'
export type CreditTrendDimension = 'recharge' | 'gift' | 'credit' | 'repayment'

// 卡片「查看明细」弹窗的维度：比趋势图多一个「消耗授信金额」
export type AmountDetailsDimension = CreditTrendDimension | 'consumption'

export type CreditTrendResp = {
  granularity: CreditTrendGranularity
  buckets: string[] // day → "YYYY-MM-DD"；month → "YYYY-MM"
  values: number[] // 与 buckets 一一对应的金额
}

// 汇总标签：用户维度
// 可排序列，与后端 creditSummarySortKeys 白名单一致
export type CreditSummarySortKey =
  | 'total_credit'
  | 'total_repayment'
  | 'outstanding'
  | 'receivable'

export type CreditSummaryFilter = {
  username?: string
  channel?: string[]
  sales?: string[]
  sort_by?: CreditSummarySortKey | ''
  sort_dir?: 'asc' | 'desc'
  page?: number
  page_size?: number
}

export type CreditSummaryRow = {
  user_id: number
  username: string
  display_name: string
  business_channel: string
  inviter_display_name: string
  total_credit: number
  total_repayment: number
  outstanding: number
  total_consumption: number // 总授信消耗金额（全时间）
  receivable: number // 剩余应收金额 = 总授信消耗 - 总回款，不为负
}

export type CreditSummaryListResp = {
  rows: CreditSummaryRow[]
  total: number
  page: number
  page_size: number
  // 整个筛选范围（未分页）的合计，用于表格上方的橙色汇总条
  summary_credit: number
  summary_repayment: number
  summary_outstanding: number
  summary_consumption: number
  summary_receivable: number
}

// 明细标签：用户 + 月份维度
export type CreditMonthlyFilter = {
  start_month: string // YYYY-MM，必填
  end_month: string // YYYY-MM，必填
  username?: string
  channel?: string[]
  sales?: string[]
  page?: number
  page_size?: number
}

export type CreditMonthlyRow = {
  month: string // YYYY-MM
  user_id: number
  username: string
  display_name: string
  business_channel: string
  inviter_display_name: string
  beginning_outstanding: number
  month_credit: number
  month_repayment: number
  ending_outstanding: number
  month_consumption: number // 本月授信消耗金额
}

export type CreditMonthlyListResp = {
  rows: CreditMonthlyRow[]
  total: number
  page: number
  page_size: number
  // 整个筛选范围（未分页）的合计，用于表格上方的橙色汇总条
  summary_credit: number
  summary_repayment: number
  summary_consumption: number
}

// 操作记录弹窗：授信记录 / 回款记录两个标签
export type CreditRecordType = 'credit' | 'repayment'

export type CreditRecordRow = {
  created_at: number // unix 秒
  quota_type: string // 授信 / 回款
  amount: number
  remark: string
}

export type CreditRecordsResp = {
  rows: CreditRecordRow[]
  total: number
  page: number
  page_size: number
}

export type CreditRecordsFilter = {
  user_id: number
  type: CreditRecordType
  start_month?: string // YYYY-MM
  end_month?: string // YYYY-MM
  page?: number
  page_size?: number
}

// 回款弹窗提交体
export type CreditRepaymentPayload = {
  user_id: number
  amount: number
  remark?: string
}

// 卡片明细弹窗
export type AmountDetailRow = {
  user_id: number
  username: string
  display_name: string
  amount: number
}

export type AmountDetailsResponse = {
  rows: AmountDetailRow[]
  total: number
  page: number
  page_size: number
}

export type AmountDetailsFilter = {
  dimension: AmountDetailsDimension
  start_date: string // YYYY-MM-DD
  end_date: string // YYYY-MM-DD
  page?: number
  page_size?: number
}

// 历史数据补录：只写流水，不改用户额度
export type CreditBackfillPayload = {
  user_id: number
  type: 1 | 2 | number // 1=授信 2=回款
  amount: number
  occurred_at: string // YYYY-MM-DD
  remark?: string
}
