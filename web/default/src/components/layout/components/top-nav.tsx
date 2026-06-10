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
import { useMemo } from 'react'
import { Link, useRouterState } from '@tanstack/react-router'
import { Menu } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { type TopNavLink } from '../types'

type TopNavProps = React.HTMLAttributes<HTMLElement> & {
  links: TopNavLink[]
}

/**
 * 顶部导航栏组件
 * 在大屏幕显示水平导航，在小屏幕显示下拉菜单
 * 激活态从当前路由 pathname 推导,样式与 public-header 一致(药丸 pill)
 */
export function TopNav({ className, links, ...props }: TopNavProps) {
  const pathname = useRouterState({ select: (s) => s.location.pathname })

  const normalizedLinks = useMemo(
    () =>
      links.map((link) => ({
        disabled: false,
        external: false,
        ...link,
        isActive: pathname === link.href,
      })),
    [links, pathname]
  )

  return (
    <>
      {/* 移动端下拉菜单 */}
      <div className='lg:hidden'>
        <DropdownMenu modal={false}>
          <DropdownMenuTrigger
            render={<Button size='icon' variant='outline' className='size-7' />}
          >
            <Menu />
          </DropdownMenuTrigger>
          <DropdownMenuContent side='bottom' align='start'>
            {normalizedLinks.map(
              ({ title, href, isActive, disabled, external }) => (
                <DropdownMenuItem
                  key={`${title}-${href}`}
                  render={
                    external ? (
                      <a
                        href={href}
                        target='_blank'
                        rel='noopener noreferrer'
                        className={cn(
                          'text-muted-foreground',
                          isActive && 'text-foreground font-medium'
                        )}
                      >
                        {title}
                      </a>
                    ) : (
                      <Link
                        to={href}
                        className={cn(
                          'text-muted-foreground',
                          isActive && 'text-foreground font-medium'
                        )}
                        disabled={disabled}
                      >
                        {title}
                      </Link>
                    )
                  }
                ></DropdownMenuItem>
              )
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      {/* 桌面端水平导航 */}
      <nav
        className={cn(
          'hidden items-center gap-0.5 lg:flex',
          className
        )}
        {...props}
      >
        {normalizedLinks.map(({ title, href, isActive, disabled, external }) => {
          const linkClass = cn(
            'rounded-full px-3 py-1.5 text-[13px] font-medium transition-all duration-200',
            isActive
              ? 'text-foreground bg-foreground/[0.06] dark:bg-foreground/[0.09]'
              : 'text-muted-foreground hover:text-foreground hover:bg-foreground/[0.04] dark:hover:bg-foreground/[0.06]',
            disabled && 'pointer-events-none opacity-50'
          )
          if (external) {
            return (
              <a
                key={`${title}-${href}`}
                href={href}
                target='_blank'
                rel='noopener noreferrer'
                className={linkClass}
              >
                {title}
              </a>
            )
          }
          return (
            <Link
              key={`${title}-${href}`}
              to={href}
              disabled={disabled}
              className={linkClass}
            >
              {title}
            </Link>
          )
        })}
      </nav>
    </>
  )
}
