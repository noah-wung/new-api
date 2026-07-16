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
import { useMemo, useState } from 'react'
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
  type PaginationState,
  type SortingState,
} from '@tanstack/react-table'
import {
  ArrowDown01Icon,
  ArrowLeft01Icon,
  ArrowRight01Icon,
  ArrowUp01Icon,
  ArrowUpDownIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'
import { formatNumber, formatQuota, formatTokens } from '@/lib/format'
import { getPageNumbers } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import type {
  UserAnalyticsPageSize,
  UserModelUsageItem,
  UserModelUsageSortField as UserModelUsageSortBy,
  UserModelUsageSortOrder as SortOrder,
} from '@/features/dashboard/types'

const PAGE_SIZE_OPTIONS = [20, 50, 100] as const

export interface UserModelUsageTableProps {
  items: UserModelUsageItem[]
  page: number
  pageSize: number
  total: number
  sortBy: UserModelUsageSortBy
  sortOrder: SortOrder
  onSortChange: (sortBy: UserModelUsageSortBy, order: SortOrder) => void
  onPageChange: (page: number) => void
  onPageSizeChange: (size: 20 | 50 | 100) => void
}

// eslint-disable-next-line react-refresh/only-export-components
export function formatSourceMetric(
  value: number | null,
  formatter: (metric: number) => string
): string {
  if (value == null) return '—'
  if (value === 0) return '0'
  return formatter(value)
}

export function UserModelUsageTable(props: UserModelUsageTableProps) {
  const { t } = useTranslation()
  const [expandedModels, setExpandedModels] = useState<Set<string>>(new Set())

  const columns = useMemo<ColumnDef<UserModelUsageItem>[]>(
    () => [
      {
        accessorKey: 'model_name',
        header: ({ column }) => (
          <Button
            type='button'
            variant='ghost'
            size='sm'
            onClick={() =>
              column.toggleSorting(
                column.getIsSorted()
                  ? column.getIsSorted() === 'asc'
                  : column.id !== 'model_name'
              )
            }
          >
            {t('Model')}
            {column.getIsSorted() === 'asc' ? (
              <HugeiconsIcon
                icon={ArrowUp01Icon}
                strokeWidth={2}
                data-icon='inline-end'
              />
            ) : column.getIsSorted() === 'desc' ? (
              <HugeiconsIcon
                icon={ArrowDown01Icon}
                strokeWidth={2}
                data-icon='inline-end'
              />
            ) : (
              <HugeiconsIcon
                icon={ArrowUpDownIcon}
                strokeWidth={2}
                data-icon='inline-end'
              />
            )}
          </Button>
        ),
        cell: ({ row }) => {
          const item = row.original
          const modelName = item.model_name.toLocaleLowerCase()
          const expanded = expandedModels.has(item.model_name)

          return (
            <div className='flex min-w-56 items-center gap-2'>
              <CollapsibleTrigger
                render={<Button type='button' variant='ghost' size='icon-sm' />}
                aria-label={t(
                  expanded
                    ? 'Collapse sources for {{model}}'
                    : 'Expand sources for {{model}}',
                  { model: modelName }
                )}
                aria-expanded={expanded}
              >
                <HugeiconsIcon
                  icon={expanded ? ArrowUp01Icon : ArrowDown01Icon}
                  strokeWidth={2}
                />
              </CollapsibleTrigger>
              <span className='font-medium'>{modelName}</span>
              {item.unmapped && (
                <Badge variant='outline'>{t('Unmapped')}</Badge>
              )}
            </div>
          )
        },
      },
      {
        accessorKey: 'token_usage',
        header: ({ column }) => (
          <Button
            type='button'
            variant='ghost'
            size='sm'
            onClick={() =>
              column.toggleSorting(
                column.getIsSorted() ? column.getIsSorted() === 'asc' : true
              )
            }
          >
            {t('Token usage')}
            {column.getIsSorted() === 'asc' ? (
              <HugeiconsIcon
                icon={ArrowUp01Icon}
                strokeWidth={2}
                data-icon='inline-end'
              />
            ) : column.getIsSorted() === 'desc' ? (
              <HugeiconsIcon
                icon={ArrowDown01Icon}
                strokeWidth={2}
                data-icon='inline-end'
              />
            ) : (
              <HugeiconsIcon
                icon={ArrowUpDownIcon}
                strokeWidth={2}
                data-icon='inline-end'
              />
            )}
          </Button>
        ),
        cell: ({ row }) => formatTokens(row.original.token_usage),
      },
      {
        accessorKey: 'quota',
        header: ({ column }) => (
          <Button
            type='button'
            variant='ghost'
            size='sm'
            onClick={() =>
              column.toggleSorting(
                column.getIsSorted() ? column.getIsSorted() === 'asc' : true
              )
            }
          >
            {t('Quota')}
            {column.getIsSorted() === 'asc' ? (
              <HugeiconsIcon
                icon={ArrowUp01Icon}
                strokeWidth={2}
                data-icon='inline-end'
              />
            ) : column.getIsSorted() === 'desc' ? (
              <HugeiconsIcon
                icon={ArrowDown01Icon}
                strokeWidth={2}
                data-icon='inline-end'
              />
            ) : (
              <HugeiconsIcon
                icon={ArrowUpDownIcon}
                strokeWidth={2}
                data-icon='inline-end'
              />
            )}
          </Button>
        ),
        cell: ({ row }) => formatQuota(row.original.quota),
      },
      {
        accessorKey: 'gateway_requests',
        header: ({ column }) => (
          <Button
            type='button'
            variant='ghost'
            size='sm'
            onClick={() =>
              column.toggleSorting(
                column.getIsSorted() ? column.getIsSorted() === 'asc' : true
              )
            }
          >
            {t('Gateway requests')}
            {column.getIsSorted() === 'asc' ? (
              <HugeiconsIcon
                icon={ArrowUp01Icon}
                strokeWidth={2}
                data-icon='inline-end'
              />
            ) : column.getIsSorted() === 'desc' ? (
              <HugeiconsIcon
                icon={ArrowDown01Icon}
                strokeWidth={2}
                data-icon='inline-end'
              />
            ) : (
              <HugeiconsIcon
                icon={ArrowUpDownIcon}
                strokeWidth={2}
                data-icon='inline-end'
              />
            )}
          </Button>
        ),
        cell: ({ row }) => formatNumber(row.original.gateway_requests),
      },
      {
        accessorKey: 'external_events',
        header: ({ column }) => (
          <Button
            type='button'
            variant='ghost'
            size='sm'
            onClick={() =>
              column.toggleSorting(
                column.getIsSorted() ? column.getIsSorted() === 'asc' : true
              )
            }
          >
            {t('External events')}
            {column.getIsSorted() === 'asc' ? (
              <HugeiconsIcon
                icon={ArrowUp01Icon}
                strokeWidth={2}
                data-icon='inline-end'
              />
            ) : column.getIsSorted() === 'desc' ? (
              <HugeiconsIcon
                icon={ArrowDown01Icon}
                strokeWidth={2}
                data-icon='inline-end'
              />
            ) : (
              <HugeiconsIcon
                icon={ArrowUpDownIcon}
                strokeWidth={2}
                data-icon='inline-end'
              />
            )}
          </Button>
        ),
        cell: ({ row }) => formatNumber(row.original.external_events),
      },
    ],
    [expandedModels, t]
  )

  const pagination: PaginationState = {
    pageIndex: Math.max(0, props.page - 1),
    pageSize: props.pageSize,
  }
  const sorting: SortingState = [
    { id: props.sortBy, desc: props.sortOrder === 'desc' },
  ]
  const pageCount = Math.ceil(props.total / props.pageSize)

  const table = useReactTable({
    data: props.items,
    columns,
    state: { pagination, sorting },
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    manualSorting: true,
    pageCount,
    onPaginationChange: (updater) => {
      const next = typeof updater === 'function' ? updater(pagination) : updater
      if (next.pageSize !== pagination.pageSize) {
        if (
          PAGE_SIZE_OPTIONS.includes(next.pageSize as UserAnalyticsPageSize)
        ) {
          props.onPageSizeChange(next.pageSize as UserAnalyticsPageSize)
        }
        return
      }
      if (next.pageIndex !== pagination.pageIndex) {
        props.onPageChange(next.pageIndex + 1)
      }
    },
    onSortingChange: (updater) => {
      const next = typeof updater === 'function' ? updater(sorting) : updater
      const first = next[0]
      if (!first) return
      props.onSortChange(
        first.id as UserModelUsageSortBy,
        first.desc ? 'desc' : 'asc'
      )
    },
  })

  const pageNumbers = getPageNumbers(props.page, pageCount)

  return (
    <div className='flex flex-col gap-4'>
      <Table>
        <TableHeader>
          {table.getHeaderGroups().map((headerGroup) => (
            <TableRow key={headerGroup.id}>
              {headerGroup.headers.map((header) => (
                <TableHead
                  key={header.id}
                  aria-sort={
                    header.column.getIsSorted() === 'asc'
                      ? 'ascending'
                      : header.column.getIsSorted() === 'desc'
                        ? 'descending'
                        : 'none'
                  }
                >
                  {header.isPlaceholder
                    ? null
                    : flexRender(
                        header.column.columnDef.header,
                        header.getContext()
                      )}
                </TableHead>
              ))}
            </TableRow>
          ))}
        </TableHeader>
        {table.getRowModel().rows.map((row) => {
          const modelName = row.original.model_name
          const expanded = expandedModels.has(modelName)

          return (
            <Collapsible
              key={row.id}
              render={<tbody />}
              open={expanded}
              onOpenChange={(open) =>
                setExpandedModels((current) => {
                  const next = new Set(current)
                  if (open) next.add(modelName)
                  else next.delete(modelName)
                  return next
                })
              }
            >
              <TableRow>
                {row.getVisibleCells().map((cell) => (
                  <TableCell key={cell.id}>
                    {flexRender(cell.column.columnDef.cell, cell.getContext())}
                  </TableCell>
                ))}
              </TableRow>
              <CollapsibleContent
                render={<tr className='bg-muted/30 hover:bg-muted/30' />}
              >
                <TableCell colSpan={5} className='p-0'>
                  <Table
                    className='table-fixed'
                    aria-label={t('Sources for {{model}}', {
                      model: modelName.toLocaleLowerCase(),
                    })}
                  >
                    <TableBody>
                      {row.original.sources.map((source) => (
                        <TableRow
                          key={`${row.original.model_name}:${source.source}`}
                        >
                          <TableCell className='w-1/5'>
                            <div className='flex items-center gap-2 pl-9'>
                              <Badge variant='secondary'>{source.source}</Badge>
                            </div>
                          </TableCell>
                          <TableCell className='w-1/5'>
                            {formatSourceMetric(
                              source.token_usage,
                              formatTokens
                            )}
                          </TableCell>
                          <TableCell className='w-1/5'>
                            {formatSourceMetric(source.quota, formatQuota)}
                          </TableCell>
                          <TableCell className='w-1/5'>
                            {formatSourceMetric(
                              source.gateway_requests,
                              formatNumber
                            )}
                          </TableCell>
                          <TableCell className='w-1/5'>
                            {formatSourceMetric(
                              source.external_events,
                              formatNumber
                            )}
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </TableCell>
              </CollapsibleContent>
            </Collapsible>
          )
        })}
      </Table>

      {pageCount > 0 && (
        <div className='flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
          <div className='flex items-center gap-2'>
            <Select
              items={PAGE_SIZE_OPTIONS.map((size) => ({
                value: String(size),
                label: String(size),
              }))}
              value={String(props.pageSize)}
              onValueChange={(value) => {
                const size = Number(value)
                if (PAGE_SIZE_OPTIONS.includes(size as UserAnalyticsPageSize)) {
                  table.setPageSize(size)
                }
              }}
            >
              <SelectTrigger className='w-20'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent side='top' alignItemWithTrigger={false}>
                <SelectGroup>
                  {PAGE_SIZE_OPTIONS.map((size) => (
                    <SelectItem key={size} value={String(size)}>
                      {size}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            <span className='text-muted-foreground text-sm'>
              {t('Rows per page')}
            </span>
          </div>

          <div className='flex items-center gap-2'>
            <span className='text-muted-foreground text-sm'>
              {t('Page {{current}} of {{total}}', {
                current: props.page,
                total: pageCount,
              })}
            </span>
            <Button
              type='button'
              variant='outline'
              size='icon'
              aria-label={t('Go to previous page')}
              disabled={!table.getCanPreviousPage()}
              onClick={() => table.previousPage()}
            >
              <HugeiconsIcon icon={ArrowLeft01Icon} strokeWidth={2} />
            </Button>
            {pageNumbers.map((pageNumber, index) =>
              pageNumber === '...' ? (
                <span
                  key={`ellipsis-${index}`}
                  className='text-muted-foreground px-1'
                  aria-hidden='true'
                >
                  …
                </span>
              ) : (
                <Button
                  key={pageNumber}
                  type='button'
                  variant={pageNumber === props.page ? 'default' : 'outline'}
                  size='icon'
                  aria-label={t('Go to page {{page}}', { page: pageNumber })}
                  aria-current={pageNumber === props.page ? 'page' : undefined}
                  onClick={() => table.setPageIndex((pageNumber as number) - 1)}
                >
                  {pageNumber}
                </Button>
              )
            )}
            <Button
              type='button'
              variant='outline'
              size='icon'
              aria-label={t('Go to next page')}
              disabled={!table.getCanNextPage()}
              onClick={() => table.nextPage()}
            >
              <HugeiconsIcon icon={ArrowRight01Icon} strokeWidth={2} />
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}
