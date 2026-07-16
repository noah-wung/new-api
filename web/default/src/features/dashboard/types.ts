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
import type { TimeGranularity } from '@/lib/time'

// ============================================================================
// Quota & Usage Data Types
// ============================================================================

export interface QuotaDataItem {
  id?: number
  user_id?: number
  username?: string
  display_name?: string
  model_name?: string
  created_at: number
  token_used?: number
  count?: number
  quota?: number
}

// ============================================================================
// Uptime Monitoring Types
// ============================================================================

export interface UptimeMonitor {
  name: string
  uptime: number
  status: number
  group?: string
}

export interface UptimeGroupResult {
  categoryName: string
  monitors: UptimeMonitor[]
}

// ============================================================================
// Dashboard Filter Types
// ============================================================================

export interface DashboardFilters {
  start_timestamp?: Date
  end_timestamp?: Date
  time_granularity?: TimeGranularity
  username?: string
}

export type ConsumptionDistributionChartType = 'bar' | 'area'

export type ModelAnalyticsChartTab = 'trend' | 'proportion' | 'top'

export interface DashboardChartPreferences {
  consumptionDistributionChart: ConsumptionDistributionChartType
  modelAnalyticsChart: ModelAnalyticsChartTab
  defaultTimeRangeDays: number
  defaultTimeGranularity: TimeGranularity
}

// ============================================================================
// User Analytics Types
// ============================================================================

export type UserAnalyticsMetric = 'quota' | 'tokens'

export type UserAnalyticsSortMode = 'metric' | 'manual'

export type UserModelUsageSortField =
  | 'model_name'
  | 'token_usage'
  | 'quota'
  | 'gateway_requests'
  | 'external_events'

export type UserModelUsageSortOrder = 'asc' | 'desc'

export type UserAnalyticsPageSize = 20 | 50 | 100

export interface UserAnalyticsSearch {
  user_id?: number
  start_timestamp?: number
  end_timestamp?: number
  metric?: UserAnalyticsMetric
  sort_mode?: UserAnalyticsSortMode
  sources?: string
  model_search?: string
  sort_by?: UserModelUsageSortField
  sort_order?: UserModelUsageSortOrder
  p?: number
  page_size?: UserAnalyticsPageSize
}

export interface GetUserModelUsageParams {
  start_timestamp: number
  end_timestamp: number
  sources?: string[]
  model_search?: string
  sort_by?: UserModelUsageSortField
  sort_order?: UserModelUsageSortOrder
  p?: number
  page_size?: UserAnalyticsPageSize
}

export interface UserModelUsageTarget {
  id: number
  username: string
  display_name: string
  status: number
  deleted: boolean
}

export interface UserModelUsageTotals {
  token_usage: number
  quota: number
  gateway_requests: number
  external_events: number
}

export interface UserModelUsageSourceItem {
  source: string
  token_usage: number
  quota: number | null
  gateway_requests: number | null
  external_events: number | null
}

export interface UserModelUsageItem {
  model_name: string
  unmapped: boolean
  token_usage: number
  quota: number
  gateway_requests: number
  external_events: number
  sources: UserModelUsageSourceItem[]
}

export interface UserModelUsageCollectionStatus {
  data_export_enabled: boolean
  consume_log_enabled: boolean
  external_usage_enabled: boolean
  refresh_interval_minutes: number
}

export interface UserModelUsageResponse {
  user: UserModelUsageTarget
  available_sources: string[]
  totals: UserModelUsageTotals
  items: UserModelUsageItem[]
  collection_status: UserModelUsageCollectionStatus
  page: number
  page_size: number
  total: number
}

export interface UserModelUsageApiResponse {
  success: boolean
  message?: string
  data?: UserModelUsageResponse
}

// ============================================================================
// API Info Types
// ============================================================================

export interface ApiInfoItem {
  url: string
  route: string
  description: string
  color: string
}

export interface PingStatus {
  latency: number | null
  testing: boolean
  error: boolean
}

export type PingStatusMap = Record<string, PingStatus>

// ============================================================================
// Chart Types
// ============================================================================

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type VChartSpec = Record<string, any>

export interface ProcessedChartData {
  spec_pie: VChartSpec
  spec_line: VChartSpec
  spec_area: VChartSpec
  spec_token_line: VChartSpec
  spec_token_area: VChartSpec
  spec_model_line: VChartSpec
  spec_rank_bar: VChartSpec
  totalQuotaDisplay: string
  totalTokensDisplay: string
  totalCountDisplay: string
}

export interface ProcessedUserChartData {
  spec_user_rank: VChartSpec
  spec_user_trend: VChartSpec
}

// ============================================================================
// Announcement Types
// ============================================================================

export interface AnnouncementItem {
  id?: number
  content: string
  publishDate?: string
  type?: 'default' | 'ongoing' | 'success' | 'warning' | 'error'
  extra?: string
}

// ============================================================================
// FAQ Types
// ============================================================================

export interface FAQItem {
  id?: number
  question: string
  answer: string
}
