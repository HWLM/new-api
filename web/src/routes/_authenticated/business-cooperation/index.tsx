import { createFileRoute } from '@tanstack/react-router'

import { BusinessCooperations } from '@/features/business-cooperations'

export const Route = createFileRoute('/_authenticated/business-cooperation/')({
  component: BusinessCooperations,
})
