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
import { createFileRoute } from '@tanstack/react-router'

import { DistillHits } from '@/features/distill-detect'

// §17：个人中心页面，所有登录用户可进。数据范围由后端 UserAuth + 控制器
// 按角色收窄（非管理员强制只看自己），前端不承担鉴权。
export const Route = createFileRoute('/_authenticated/distill-hits/')({
  component: DistillHits,
})
