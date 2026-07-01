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
import { searchUsers } from '@/features/users/api'
import type { User } from '@/features/users/types'
import type { ExternalUsageInstallerPlatform } from './installer-command'

export async function getExternalUsageOverview(userId?: number) {
  const query = new URLSearchParams()
  if (userId != null) query.set('user_id', String(userId))
  const suffix = query.toString()
  const res = await api.get(
    `/api/external-usage/self/overview${suffix ? `?${suffix}` : ''}`
  )
  return res.data
}

export async function getExternalUsageDevices(userId?: number) {
  const query = new URLSearchParams()
  if (userId != null) query.set('user_id', String(userId))
  const suffix = query.toString()
  const res = await api.get(
    `/api/external-usage/self/devices${suffix ? `?${suffix}` : ''}`
  )
  return res.data
}

export async function createExternalUsageDeviceCredential(payload: {
  device_name: string
  device_fingerprint: string
  allowed_sources: string[]
}) {
  const res = await api.post('/api/external-usage/self/devices', payload)
  return res.data
}

export async function revokeExternalUsageDevice(id: number) {
  const res = await api.delete(`/api/external-usage/self/devices/${id}`)
  return res.data
}

export async function createExternalUsageCodexInstallerCommand(payload: {
  platform: ExternalUsageInstallerPlatform
}) {
  const res = await api.post('/api/external-usage/self/codex-installer-command', payload)
  return res.data
}

export async function listExternalUsageCursorImportBatches(params?: {
  p?: number
  size?: number
  userId?: number
  admin?: boolean
}) {
  const query = new URLSearchParams()
  if (params?.p != null) query.set('p', String(params.p))
  if (params?.size != null) query.set('size', String(params.size))
  if (params?.userId != null) query.set('user_id', String(params.userId))
  const prefix = params?.admin
    ? '/api/external-usage/admin/cursor-import-batches'
    : '/api/external-usage/self/cursor-import-batches'
  const res = await api.get(`${prefix}?${query.toString()}`)
  return res.data
}

export async function listExternalUsageReportBatches(params?: {
  p?: number
  size?: number
  userId?: number
  admin?: boolean
}) {
  const query = new URLSearchParams()
  if (params?.p != null) query.set('p', String(params.p))
  if (params?.size != null) query.set('size', String(params.size))
  if (params?.userId != null) query.set('user_id', String(params.userId))
  const prefix = params?.admin
    ? '/api/external-usage/admin/report-batches'
    : '/api/external-usage/self/report-batches'
  const res = await api.get(`${prefix}?${query.toString()}`)
  return res.data
}

export async function importExternalUsageCursorCSV(payload: {
  userId: number
  file: File
}) {
  const formData = new FormData()
  formData.append('user_id', String(payload.userId))
  formData.append('file', payload.file)
  const res = await api.post('/api/external-usage/admin/cursor-import', formData, {
    headers: {
      'Content-Type': 'multipart/form-data',
    },
  })
  return res.data
}

export async function deleteExternalUsageCursorImportBatch(id: number) {
  const res = await api.delete(`/api/external-usage/admin/cursor-import-batches/${id}`)
  return res.data
}

export async function replayExternalUsageCursorImportBatch(id: number) {
  const res = await api.post(`/api/external-usage/admin/cursor-import-batches/${id}/replay`)
  return res.data
}

export async function rebuildExternalUsageAggregates(payload: {
  user_id: number
  source?: string
  start_time?: number
  end_time?: number
}) {
  const res = await api.post('/api/external-usage/admin/aggregates/rebuild', payload)
  return res.data
}

export async function listExternalUsageDetails(params: {
  userId: number
  source?: string
  p?: number
  size?: number
}) {
  const query = new URLSearchParams()
  query.set('user_id', String(params.userId))
  if (params.source) query.set('source', params.source)
  if (params.p != null) query.set('p', String(params.p))
  if (params.size != null) query.set('size', String(params.size))
  const res = await api.get(`/api/external-usage/admin/details?${query.toString()}`)
  return res.data
}

export async function listExternalUsageModelMappings(source?: string) {
  const query = new URLSearchParams()
  if (source) query.set('source', source)
  const suffix = query.toString()
  const res = await api.get(
    `/api/external-usage/admin/model-mappings${suffix ? `?${suffix}` : ''}`
  )
  return res.data
}

export async function upsertExternalUsageModelMapping(payload: {
  source: string
  source_model_name: string
  normalized_model_name: string
}) {
  const res = await api.post('/api/external-usage/admin/model-mappings', payload)
  return res.data
}

export async function deleteExternalUsageModelMapping(id: number) {
  const res = await api.delete(`/api/external-usage/admin/model-mappings/${id}`)
  return res.data
}

export async function deleteExternalUsageModelData(payload: {
  user_id: number
  source: string
  normalized_model_name: string
}) {
  const res = await api.post('/api/external-usage/admin/model-data/delete', payload)
  return res.data
}

export async function searchExternalUsageTargetUsers(keyword: string) {
  const res = await searchUsers({
    keyword,
    p: 1,
    page_size: 50,
  })
  return (res.data?.items ?? []) as User[]
}
