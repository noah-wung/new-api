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
import { api } from '@/lib/api'
import type {
  GetUserModelUsageParams,
  QuotaDataItem,
  UptimeGroupResult,
  UserModelUsageApiResponse,
} from './types'

// ============================================================================
// Dashboard APIs
// ============================================================================

// ----------------------------------------------------------------------------
// Quota & Usage Data
// ----------------------------------------------------------------------------

// Get user quota data within a time range
// Admin users get all users' data by default (matching classic frontend behavior)
export async function getUserQuotaDates(
  params: {
    start_timestamp: number
    end_timestamp: number
    default_time?: string
    username?: string
  },
  isAdmin = false
) {
  const endpoint = isAdmin ? '/api/data' : '/api/data/self'
  const res = await api.get<{
    success: boolean
    message?: string
    data?: QuotaDataItem[]
  }>(endpoint, { params })
  return res.data
}

// ----------------------------------------------------------------------------
// System Monitoring
// ----------------------------------------------------------------------------

export async function getUserQuotaDataByUsers(params: {
  start_timestamp: number
  end_timestamp: number
}) {
  const res = await api.get<{
    success: boolean
    message?: string
    data?: QuotaDataItem[]
  }>('/api/data/users', { params })
  return res.data
}

export async function getUserModelUsage(
  userId: number,
  params: GetUserModelUsageParams
): Promise<UserModelUsageApiResponse> {
  const query = new URLSearchParams()
  query.set('start_timestamp', String(params.start_timestamp))
  query.set('end_timestamp', String(params.end_timestamp))
  if (params.sources?.length) {
    query.set('sources', params.sources.join(','))
  }
  if (params.model_search != null) {
    query.set('model_search', params.model_search)
  }
  if (params.sort_by != null) query.set('sort_by', params.sort_by)
  if (params.sort_order != null) query.set('sort_order', params.sort_order)
  if (params.p != null) query.set('p', String(params.p))
  if (params.page_size != null) {
    query.set('page_size', String(params.page_size))
  }

  const res = await api.get<UserModelUsageApiResponse>(
    `/api/data/users/${userId}/models?${query.toString()}`
  )
  return res.data
}

// Get uptime monitoring status for all services
export async function getUptimeStatus() {
  const res = await api.get<{ success: boolean; data: UptimeGroupResult[] }>(
    '/api/uptime/status'
  )
  return res.data
}
