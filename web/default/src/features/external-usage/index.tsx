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
import { useEffect, useMemo, useRef, useState } from 'react'
import {
  HardDriveUpload,
  KeyRound,
  RefreshCcw,
  Save,
  Shield,
  Trash2,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { getStatus } from '@/lib/api'
import { formatTimestamp } from '@/lib/format'
import { getPageNumbers } from '@/lib/utils'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import {
  Pagination,
  PaginationContent,
  PaginationEllipsis,
  PaginationItem,
  PaginationLink,
  PaginationNext,
  PaginationPrevious,
} from '@/components/ui/pagination'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { CopyButton } from '@/components/copy-button'
import { Main } from '@/components/layout'
import type { User } from '@/features/users/types'
import {
  createExternalUsageCodexInstallerCommand,
  createExternalUsageDeviceCredential,
  deleteExternalUsageModelMapping,
  rebuildExternalUsageAggregates,
  deleteExternalUsageCursorImportBatch,
  deleteExternalUsageModelData,
  getExternalUsageOverview,
  importExternalUsageCursorCSV,
  listExternalUsageCursorImportBatches,
  listExternalUsageDetails,
  listExternalUsageModelMappings,
  listExternalUsageReportBatches,
  replayExternalUsageCursorImportBatch,
  revokeExternalUsageDevice,
  searchExternalUsageTargetUsers,
  upsertExternalUsageModelMapping,
} from './api'
import {
  buildInstallerCommandCardCopy,
  normalizeInstallerCommandExpiry,
  type ExternalUsageInstallerPlatform,
} from './installer-command'
import {
  canRequestExternalUsageInstallerCommand,
  initializeExternalUsageSelection,
  shouldLoadExternalUsageData,
  syncExternalUsageSelection,
} from './selection'
import {
  buildExternalUsageSourceOptions,
  getDefaultDeviceAllowedSources,
  getExternalUsageMaxDevicesPerUser,
} from './source-options'

type ExternalUsageDevice = {
  id: number
  device_name: string
  allowed_sources: string
  status: string
  last_reported_at: number
  last_seen_at: number
  created_at: number
}

type ExternalUsageSummaryRow = {
  source: string
  normalized_model_name: string
  total_tokens: number
  input_tokens: number
  cached_input_tokens: number
  output_tokens: number
  event_count: number
}

type ExternalUsageOverviewData = {
  devices: ExternalUsageDevice[]
  summary: ExternalUsageSummaryRow[]
  total_tokens: number
  total_events: number
}

type CursorBatchRow = {
  id: number
  user_id: number
  file_name: string
  status: string
  total_rows: number
  imported_rows: number
  duplicate_rows: number
  rejected_rows: number
  ignored_rows: number
  occurred_from: number
  occurred_to: number
  created_at: number
}

type ExternalUsageModelMappingRow = {
  id: number
  source: string
  source_model_name: string
  normalized_model_name: string
  status: string
  created_at: number
}

type ReportBatchRow = {
  id: number
  user_id: number
  device_id: number
  device_name: string
  source: string
  status: string
  client_version: string
  accepted_count: number
  duplicate_count: number
  rejected_count: number
  occurred_from: number
  occurred_to: number
  created_at: number
}

type ExternalUsageDetailRow = {
  origin: string
  source: string
  status: string
  batch_id: number
  device_id: number
  occurred_at: number
  event_identity: string
  source_model_name: string
  normalized_model_name: string
  input_tokens: number
  cached_input_tokens: number
  output_tokens: number
  reasoning_output_token: number
  total_tokens: number
}

type PagedData<T> = {
  page: number
  page_size: number
  total: number
  items: T[]
}

function buildUserLabel(user: User) {
  const displayName = user.display_name?.trim()
  if (displayName && displayName !== user.username) {
    return `${displayName} (${user.username})`
  }
  return user.username
}

function getExternalUsageSourceLabel(
  t: (key: string) => string,
  source: string
) {
  switch (source.trim().toLowerCase()) {
    case 'cursor':
      return t('Cursor')
    case 'codex':
      return t('Codex')
    case 'zcode':
      return t('ZCode')
    case 'minimax_code':
      return t('MiniMax Code')
    default:
      return source
  }
}

function getExternalUsageOriginLabel(
  t: (key: string) => string,
  origin: string
) {
  switch (origin.trim().toLowerCase()) {
    case 'cursor_import':
      return t('Cursor Import')
    case 'client_report':
      return t('Client Report')
    default:
      return origin
  }
}

function getExternalUsageStatusLabel(
  t: (key: string) => string,
  status: string
) {
  const normalized = status.trim().toLowerCase()
  switch (normalized) {
    case 'active':
    case 'revoked':
    case 'reverted':
    case 'accepted':
    case 'rejected':
      return t(normalized)
    default:
      return status
  }
}

function parseAllowedSources(raw: string, t: (key: string) => string) {
  try {
    const parsed = JSON.parse(raw)
    return Array.isArray(parsed)
      ? parsed
          .map((item) =>
            typeof item === 'string'
              ? getExternalUsageSourceLabel(t, item)
              : String(item)
          )
          .join(', ')
      : raw
  } catch {
    return raw
  }
}

function toUnixTimestamp(value: string) {
  if (!value) return undefined
  const timestamp = Math.floor(new Date(value).getTime() / 1000)
  return Number.isFinite(timestamp) && timestamp > 0 ? timestamp : undefined
}

function SetupCommandBlock(props: {
  title: string
  value: string
  copiedLabel: string
}) {
  return (
    <div className='grid gap-2'>
      <div className='flex items-center justify-between gap-3'>
        <Label>{props.title}</Label>
        <CopyButton
          value={props.value}
          tooltip={props.title}
          successTooltip={props.copiedLabel}
          aria-label={props.title}
        />
      </div>
      <pre className='bg-muted/40 overflow-x-auto rounded-md border px-3 py-2 font-mono text-xs leading-6 whitespace-pre-wrap'>
        {props.value}
      </pre>
    </div>
  )
}

export function ExternalUsage() {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const isAdmin = (user?.role ?? 0) >= 10

  const [enabled, setEnabled] = useState<boolean | null>(null)
  const [overview, setOverview] = useState<ExternalUsageOverviewData | null>(
    null
  )
  const [batches, setBatches] = useState<CursorBatchRow[]>([])
  const [reportBatches, setReportBatches] = useState<ReportBatchRow[]>([])
  const [details, setDetails] = useState<ExternalUsageDetailRow[]>([])
  const [modelMappings, setModelMappings] = useState<
    ExternalUsageModelMappingRow[]
  >([])
  const [credentialValue, setCredentialValue] = useState('')
  const [deviceName, setDeviceName] = useState('')
  const [deviceFingerprint, setDeviceFingerprint] = useState('')
  const [configuredClientSources, setConfiguredClientSources] = useState<
    string[]
  >(getDefaultDeviceAllowedSources({}))
  const [selectedDeviceSources, setSelectedDeviceSources] = useState<string[]>(
    getDefaultDeviceAllowedSources({})
  )
  const [maxDevicesPerUser, setMaxDevicesPerUser] = useState(3)
  const [selectedUserId, setSelectedUserId] = useState<number | null>(null)
  const [importTargetUserId, setImportTargetUserId] = useState<number | null>(
    null
  )
  const [userKeyword, setUserKeyword] = useState('')
  const [targetUsers, setTargetUsers] = useState<User[]>([])
  const [importFile, setImportFile] = useState<File | null>(null)
  const [rebuildSource, setRebuildSource] = useState('')
  const [rebuildStart, setRebuildStart] = useState('')
  const [rebuildEnd, setRebuildEnd] = useState('')
  const [mappingSource, setMappingSource] = useState('cursor')
  const [mappingSourceModelName, setMappingSourceModelName] = useState('')
  const [mappingNormalizedModelName, setMappingNormalizedModelName] =
    useState('')
  const [detailSource, setDetailSource] = useState('')
  const [detailPage, setDetailPage] = useState(1)
  const [detailTotal, setDetailTotal] = useState(0)
  const [installerPlatform, setInstallerPlatform] =
    useState<ExternalUsageInstallerPlatform>('macos')
  const [installerCommand, setInstallerCommand] = useState('')
  const [installerCommandExpiresAt, setInstallerCommandExpiresAt] = useState<
    number | null
  >(null)
  const [loading, setLoading] = useState(false)
  const loadRequestIdRef = useRef(0)
  const importFileInputRef = useRef<HTMLInputElement | null>(null)
  const canIssueCredential = !isAdmin || selectedUserId === user?.id
  const canRequestInstallerCommand = canRequestExternalUsageInstallerCommand({
    isAdmin,
    currentUserId: user?.id,
    selectedUserId,
  })
  const detailPageSize = 20

  const applyTargetUserSelection = (userId: number | null) => {
    const next = syncExternalUsageSelection(userId)
    setSelectedUserId(next.selectedUserId)
    setImportTargetUserId(next.importTargetUserId)
    setDetailPage(1)
  }

  const loadUsers = async (keyword: string) => {
    if (!isAdmin) return
    const items = await searchExternalUsageTargetUsers(keyword).catch(() => [])
    setTargetUsers(items)
    if (items.length > 0) {
      const next = initializeExternalUsageSelection(
        {
          selectedUserId,
          importTargetUserId,
        },
        items.map((item) => item.id)
      )
      setSelectedUserId(next.selectedUserId)
      setImportTargetUserId(next.importTargetUserId)
    }
  }

  const loadData = async () => {
    if (!shouldLoadExternalUsageData(isAdmin, selectedUserId)) {
      setOverview(null)
      setBatches([])
      setReportBatches([])
      setDetails([])
      setModelMappings([])
      setDetailTotal(0)
      return
    }

    const requestId = ++loadRequestIdRef.current
    const status = await getStatus().catch(() => null)
    if (requestId !== loadRequestIdRef.current) return
    setEnabled(Boolean(status?.external_usage_enabled))
    const nextConfiguredClientSources = getDefaultDeviceAllowedSources(
      status ?? {}
    )
    setConfiguredClientSources(nextConfiguredClientSources)
    setMaxDevicesPerUser(getExternalUsageMaxDevicesPerUser(status ?? {}))
    if (!status?.external_usage_enabled) {
      setOverview(null)
      setBatches([])
      setReportBatches([])
      setDetails([])
      setModelMappings([])
      setDetailTotal(0)
      return
    }

    const overviewRes = await getExternalUsageOverview(
      selectedUserId ?? undefined
    ).catch(() => null)
    if (requestId !== loadRequestIdRef.current) return
    if (overviewRes?.success) {
      setOverview(overviewRes.data as ExternalUsageOverviewData)
    }

    const reportBatchRes = await listExternalUsageReportBatches({
      admin: isAdmin,
      p: 1,
      size: 20,
      userId: selectedUserId ?? undefined,
    }).catch(() => null)
    if (requestId !== loadRequestIdRef.current) return
    if (reportBatchRes?.success) {
      setReportBatches(reportBatchRes.data?.items ?? [])
    }

    if (isAdmin) {
      const batchRes = await listExternalUsageCursorImportBatches({
        admin: true,
        p: 1,
        size: 20,
        userId: selectedUserId ?? undefined,
      }).catch(() => null)
      if (requestId !== loadRequestIdRef.current) return
      if (batchRes?.success) {
        setBatches(batchRes.data?.items ?? [])
      }

      const mappingRes = await listExternalUsageModelMappings().catch(
        () => null
      )
      if (requestId !== loadRequestIdRef.current) return
      if (mappingRes?.success) {
        setModelMappings(mappingRes.data ?? [])
      }

      const detailRes = await listExternalUsageDetails({
        userId: selectedUserId ?? 0,
        source: detailSource || undefined,
        p: detailPage,
        size: detailPageSize,
      }).catch(() => null)
      if (requestId !== loadRequestIdRef.current) return
      if (detailRes?.success) {
        const paged = (detailRes.data ??
          {}) as PagedData<ExternalUsageDetailRow>
        setDetails(paged.items ?? [])
        setDetailTotal(paged.total ?? 0)
      }
    }
  }

  useEffect(() => {
    if (isAdmin) {
      void loadUsers('')
    }
  }, [isAdmin])

  useEffect(() => {
    void loadData()
  }, [detailPage, detailSource, isAdmin, selectedUserId])

  useEffect(() => {
    setSelectedDeviceSources((current) => {
      const retained = current.filter((item) =>
        configuredClientSources.includes(item)
      )
      return retained.length > 0 ? retained : [...configuredClientSources]
    })
  }, [configuredClientSources])

  useEffect(() => {
    setInstallerCommand('')
    setInstallerCommandExpiresAt(null)
  }, [installerPlatform, selectedUserId])

  const deviceRows = overview?.devices ?? []
  const summaryRows = overview?.summary ?? []
  const detailTotalPages = Math.max(1, Math.ceil(detailTotal / detailPageSize))
  const detailPageNumbers = getPageNumbers(detailPage, detailTotalPages)
  const sourceOptions = useMemo(
    () =>
      buildExternalUsageSourceOptions({
        external_usage_allowed_sources: configuredClientSources,
      }),
    [configuredClientSources]
  )
  const installerCommandCopy = useMemo(
    () => buildInstallerCommandCardCopy((key) => t(key)),
    [t]
  )

  const summaryCards = useMemo(
    () => [
      {
        title: t('External Tokens'),
        value: overview?.total_tokens?.toLocaleString() ?? '0',
        icon: HardDriveUpload,
      },
      {
        title: t('External Events'),
        value: overview?.total_events?.toLocaleString() ?? '0',
        icon: Shield,
      },
      {
        title: t('Devices'),
        value: String(deviceRows.length),
        icon: KeyRound,
      },
    ],
    [deviceRows.length, overview?.total_events, overview?.total_tokens, t]
  )

  const handleCreateCredential = async () => {
    if (!deviceName.trim() || !deviceFingerprint.trim()) return
    setLoading(true)
    try {
      const res = await createExternalUsageDeviceCredential({
        device_name: deviceName.trim(),
        device_fingerprint: deviceFingerprint.trim(),
        allowed_sources: selectedDeviceSources,
      })
      if (res?.success) {
        setCredentialValue(res.data?.reporting_credential ?? '')
        setDeviceName('')
        setDeviceFingerprint('')
        setSelectedDeviceSources([...configuredClientSources])
        await loadData()
      }
    } finally {
      setLoading(false)
    }
  }

  const handleRevokeDevice = async (id: number) => {
    setLoading(true)
    try {
      await revokeExternalUsageDevice(id)
      await loadData()
    } finally {
      setLoading(false)
    }
  }

  const handleCreateInstallerCommand = async () => {
    if (!canRequestInstallerCommand) return
    setLoading(true)
    try {
      const res = await createExternalUsageCodexInstallerCommand({
        platform: installerPlatform,
      })
      if (res?.success) {
        setInstallerCommand(res.data?.command ?? '')
        setInstallerCommandExpiresAt(
          normalizeInstallerCommandExpiry(res.data?.expires_at)
        )
      }
    } finally {
      setLoading(false)
    }
  }

  const handleImport = async () => {
    if (!importFile || !importTargetUserId) return
    setLoading(true)
    try {
      const res = await importExternalUsageCursorCSV({
        userId: importTargetUserId,
        file: importFile,
      })
      if (res?.success) {
        setImportFile(null)
        await loadData()
      }
    } finally {
      setLoading(false)
    }
  }

  const handleDeleteModelData = async (row: ExternalUsageSummaryRow) => {
    if (!isAdmin || !selectedUserId) return
    setLoading(true)
    try {
      await deleteExternalUsageModelData({
        user_id: selectedUserId,
        source: row.source,
        normalized_model_name: row.normalized_model_name,
      })
      await loadData()
    } finally {
      setLoading(false)
    }
  }

  const handleSaveModelMapping = async () => {
    if (
      !mappingSource.trim() ||
      !mappingSourceModelName.trim() ||
      !mappingNormalizedModelName.trim()
    ) {
      return
    }
    setLoading(true)
    try {
      await upsertExternalUsageModelMapping({
        source: mappingSource.trim(),
        source_model_name: mappingSourceModelName.trim(),
        normalized_model_name: mappingNormalizedModelName.trim(),
      })
      setMappingSourceModelName('')
      setMappingNormalizedModelName('')
      await loadData()
    } finally {
      setLoading(false)
    }
  }

  const handleDeleteModelMapping = async (id: number) => {
    setLoading(true)
    try {
      await deleteExternalUsageModelMapping(id)
      await loadData()
    } finally {
      setLoading(false)
    }
  }

  const handleDeleteBatch = async (row: CursorBatchRow) => {
    if (!isAdmin || row.status !== 'active') return
    setLoading(true)
    try {
      await deleteExternalUsageCursorImportBatch(row.id)
      await loadData()
    } finally {
      setLoading(false)
    }
  }

  const handleReplayBatch = async (row: CursorBatchRow) => {
    if (!isAdmin || row.status !== 'reverted') return
    setLoading(true)
    try {
      await replayExternalUsageCursorImportBatch(row.id)
      await loadData()
    } finally {
      setLoading(false)
    }
  }

  const handleRebuildAggregates = async () => {
    if (!isAdmin || !selectedUserId) return
    setLoading(true)
    try {
      await rebuildExternalUsageAggregates({
        user_id: selectedUserId,
        source: rebuildSource || undefined,
        start_time: toUnixTimestamp(rebuildStart),
        end_time: toUnixTimestamp(rebuildEnd),
      })
      await loadData()
    } finally {
      setLoading(false)
    }
  }

  return (
    <Main>
      <div className='min-h-0 flex-1 overflow-auto'>
        <div className='mx-auto flex w-full max-w-7xl flex-col gap-6 px-4 py-6 md:px-6'>
          <div className='flex items-center justify-between gap-3'>
            <div>
              <h1 className='text-2xl font-semibold'>{t('External Usage')}</h1>
              <p className='text-muted-foreground text-sm'>
                {t(
                  'External coding tool usage collected outside the API gateway.'
                )}
              </p>
            </div>
            <Button variant='outline' size='sm' onClick={() => void loadData()}>
              <RefreshCcw />
              {t('Refresh')}
            </Button>
          </div>

          {enabled === false ? (
            <Alert>
              <AlertTitle>{t('Feature Disabled')}</AlertTitle>
              <AlertDescription>
                {t(
                  'External usage reporting is currently disabled at the site level.'
                )}
              </AlertDescription>
            </Alert>
          ) : null}

          {isAdmin ? (
            <Card>
              <CardHeader>
                <CardTitle>{t('Target User')}</CardTitle>
                <CardDescription>
                  {t(
                    'Choose an existing user for summary inspection and Cursor imports.'
                  )}
                </CardDescription>
              </CardHeader>
              <CardContent className='grid gap-3 md:grid-cols-[minmax(0,220px)_minmax(0,1fr)]'>
                <Input
                  value={userKeyword}
                  onChange={(e) => setUserKeyword(e.target.value)}
                  placeholder={t('Search username or display name')}
                />
                <div className='flex gap-3'>
                  <NativeSelect
                    className='w-full'
                    value={selectedUserId != null ? String(selectedUserId) : ''}
                    onChange={(e) => {
                      const value = Number(e.target.value)
                      applyTargetUserSelection(
                        Number.isFinite(value) ? value : null
                      )
                    }}
                  >
                    {targetUsers.map((item) => (
                      <NativeSelectOption key={item.id} value={String(item.id)}>
                        {buildUserLabel(item)}
                      </NativeSelectOption>
                    ))}
                  </NativeSelect>
                  <Button
                    variant='outline'
                    onClick={() => void loadUsers(userKeyword)}
                  >
                    <RefreshCcw />
                    {t('Search')}
                  </Button>
                </div>
              </CardContent>
            </Card>
          ) : null}

          {isAdmin ? (
            <Card>
              <CardHeader>
                <CardTitle>{t('Cursor Imports')}</CardTitle>
                <CardDescription>
                  {t(
                    'Only administrators can import Cursor-exported usage CSV files.'
                  )}
                </CardDescription>
              </CardHeader>
              <CardContent className='flex flex-col gap-4'>
                <div className='grid gap-3 md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto]'>
                  <NativeSelect
                    className='w-full'
                    value={
                      importTargetUserId != null
                        ? String(importTargetUserId)
                        : ''
                    }
                    onChange={(e) => {
                      const value = Number(e.target.value)
                      applyTargetUserSelection(
                        Number.isFinite(value) ? value : null
                      )
                    }}
                  >
                    {targetUsers.map((item) => (
                      <NativeSelectOption key={item.id} value={String(item.id)}>
                        {buildUserLabel(item)}
                      </NativeSelectOption>
                    ))}
                  </NativeSelect>
                  <div className='grid gap-2'>
                    <input
                      ref={importFileInputRef}
                      type='file'
                      accept='.csv,text/csv'
                      className='hidden'
                      onChange={(e) =>
                        setImportFile(e.target.files?.[0] ?? null)
                      }
                    />
                    <div className='flex items-center gap-2'>
                      <Button
                        type='button'
                        variant='outline'
                        onClick={() => importFileInputRef.current?.click()}
                      >
                        <HardDriveUpload />
                        {t('Upload')}
                      </Button>
                      {importFile ? (
                        <Button
                          type='button'
                          variant='outline'
                          onClick={() => {
                            setImportFile(null)
                            if (importFileInputRef.current) {
                              importFileInputRef.current.value = ''
                            }
                          }}
                        >
                          {t('Clear')}
                        </Button>
                      ) : null}
                    </div>
                    <div className='text-muted-foreground min-h-5 text-sm'>
                      {importFile?.name || t('No file selected')}
                    </div>
                  </div>
                  <Button
                    disabled={loading || !importFile || !importTargetUserId}
                    onClick={() => void handleImport()}
                  >
                    <HardDriveUpload />
                    {t('Import CSV')}
                  </Button>
                </div>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t('ID')}</TableHead>
                      <TableHead>{t('User')}</TableHead>
                      <TableHead>{t('File')}</TableHead>
                      <TableHead>{t('Status')}</TableHead>
                      <TableHead>{t('Imported')}</TableHead>
                      <TableHead>{t('Duplicate')}</TableHead>
                      <TableHead>{t('Rejected')}</TableHead>
                      <TableHead>{t('Ignored')}</TableHead>
                      <TableHead>{t('Range')}</TableHead>
                      <TableHead>{t('Action')}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {batches.length === 0 ? (
                      <TableRow>
                        <TableCell
                          colSpan={10}
                          className='text-muted-foreground py-6 text-center'
                        >
                          {t('No import batches')}
                        </TableCell>
                      </TableRow>
                    ) : (
                      batches.map((row) => (
                        <TableRow key={row.id}>
                          <TableCell>{row.id}</TableCell>
                          <TableCell>{row.user_id}</TableCell>
                          <TableCell>{row.file_name}</TableCell>
                          <TableCell>
                            <Badge variant='secondary'>
                              {getExternalUsageStatusLabel(t, row.status)}
                            </Badge>
                          </TableCell>
                          <TableCell>
                            {row.imported_rows}/{row.total_rows}
                          </TableCell>
                          <TableCell>{row.duplicate_rows}</TableCell>
                          <TableCell>{row.rejected_rows}</TableCell>
                          <TableCell>{row.ignored_rows}</TableCell>
                          <TableCell>
                            {row.occurred_from > 0
                              ? formatTimestamp(row.occurred_from)
                              : '-'}{' '}
                            ~{' '}
                            {row.occurred_to > 0
                              ? formatTimestamp(row.occurred_to)
                              : '-'}
                          </TableCell>
                          <TableCell>
                            <div className='flex items-center gap-1'>
                              <Button
                                size='icon-sm'
                                variant='ghost'
                                disabled={loading || row.status !== 'active'}
                                onClick={() => void handleDeleteBatch(row)}
                              >
                                <Trash2 />
                              </Button>
                              <Button
                                size='icon-sm'
                                variant='ghost'
                                disabled={loading || row.status !== 'reverted'}
                                onClick={() => void handleReplayBatch(row)}
                              >
                                <RefreshCcw />
                              </Button>
                            </div>
                          </TableCell>
                        </TableRow>
                      ))
                    )}
                  </TableBody>
                </Table>
              </CardContent>
            </Card>
          ) : null}

          {isAdmin ? (
            <Card>
              <CardHeader>
                <CardTitle>{t('Model Mappings')}</CardTitle>
                <CardDescription>
                  {t(
                    'Override source model names before rebuilding aggregates and details.'
                  )}
                </CardDescription>
              </CardHeader>
              <CardContent className='flex flex-col gap-4'>
                <div className='grid gap-3 md:grid-cols-[180px_minmax(0,1fr)_minmax(0,1fr)_auto]'>
                  <NativeSelect
                    className='w-full'
                    value={mappingSource}
                    onChange={(e) => setMappingSource(e.target.value)}
                  >
                    {sourceOptions.map((source) => (
                      <NativeSelectOption key={source} value={source}>
                        {getExternalUsageSourceLabel(t, source)}
                      </NativeSelectOption>
                    ))}
                  </NativeSelect>
                  <Input
                    value={mappingSourceModelName}
                    onChange={(e) => setMappingSourceModelName(e.target.value)}
                    placeholder={t('Source model name')}
                  />
                  <Input
                    value={mappingNormalizedModelName}
                    onChange={(e) =>
                      setMappingNormalizedModelName(e.target.value)
                    }
                    placeholder={t('Normalized model name')}
                  />
                  <Button
                    disabled={
                      loading ||
                      !mappingSource.trim() ||
                      !mappingSourceModelName.trim() ||
                      !mappingNormalizedModelName.trim()
                    }
                    onClick={() => void handleSaveModelMapping()}
                  >
                    <Save />
                    {t('Save Mapping')}
                  </Button>
                </div>
                <p className='text-muted-foreground text-sm'>
                  {t(
                    'Saving the same source and source model updates the existing mapping.'
                  )}
                </p>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t('ID')}</TableHead>
                      <TableHead>{t('Source')}</TableHead>
                      <TableHead>{t('Source Model')}</TableHead>
                      <TableHead>{t('Normalized Model')}</TableHead>
                      <TableHead>{t('Status')}</TableHead>
                      <TableHead>{t('Created')}</TableHead>
                      <TableHead>{t('Action')}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {modelMappings.length === 0 ? (
                      <TableRow>
                        <TableCell
                          colSpan={7}
                          className='text-muted-foreground py-6 text-center'
                        >
                          {t('No model mappings')}
                        </TableCell>
                      </TableRow>
                    ) : (
                      modelMappings.map((row) => (
                        <TableRow key={row.id}>
                          <TableCell>{row.id}</TableCell>
                          <TableCell>
                            <Badge variant='secondary'>
                              {getExternalUsageSourceLabel(t, row.source)}
                            </Badge>
                          </TableCell>
                          <TableCell>{row.source_model_name}</TableCell>
                          <TableCell>{row.normalized_model_name}</TableCell>
                          <TableCell>
                            <Badge variant='outline'>
                              {getExternalUsageStatusLabel(t, row.status)}
                            </Badge>
                          </TableCell>
                          <TableCell>
                            {formatTimestamp(row.created_at)}
                          </TableCell>
                          <TableCell>
                            <Button
                              size='icon-sm'
                              variant='ghost'
                              disabled={loading}
                              onClick={() =>
                                void handleDeleteModelMapping(row.id)
                              }
                            >
                              <Trash2 />
                            </Button>
                          </TableCell>
                        </TableRow>
                      ))
                    )}
                  </TableBody>
                </Table>
              </CardContent>
            </Card>
          ) : null}

          {isAdmin ? (
            <Card>
              <CardHeader>
                <CardTitle>{t('Aggregate Rebuild')}</CardTitle>
                <CardDescription>
                  {t(
                    'Rebuild source-aware external usage aggregates from retained facts for the selected user.'
                  )}
                </CardDescription>
              </CardHeader>
              <CardContent className='grid gap-3 md:grid-cols-[180px_minmax(0,1fr)_minmax(0,1fr)_auto]'>
                <NativeSelect
                  className='w-full'
                  value={rebuildSource}
                  onChange={(e) => setRebuildSource(e.target.value)}
                >
                  <NativeSelectOption value=''>
                    {t('All Sources')}
                  </NativeSelectOption>
                  {sourceOptions.map((source) => (
                    <NativeSelectOption key={source} value={source}>
                      {getExternalUsageSourceLabel(t, source)}
                    </NativeSelectOption>
                  ))}
                </NativeSelect>
                <Input
                  type='datetime-local'
                  value={rebuildStart}
                  onChange={(e) => setRebuildStart(e.target.value)}
                />
                <Input
                  type='datetime-local'
                  value={rebuildEnd}
                  onChange={(e) => setRebuildEnd(e.target.value)}
                />
                <Button
                  disabled={loading || !selectedUserId}
                  onClick={() => void handleRebuildAggregates()}
                >
                  <RefreshCcw />
                  {t('Rebuild')}
                </Button>
              </CardContent>
            </Card>
          ) : null}

          <div className='grid gap-4 md:grid-cols-3'>
            {summaryCards.map((item) => (
              <Card key={item.title}>
                <CardHeader>
                  <CardTitle className='flex items-center gap-2 text-sm'>
                    <item.icon className='size-4' />
                    {item.title}
                  </CardTitle>
                </CardHeader>
                <CardContent>
                  <div className='text-2xl font-semibold'>{item.value}</div>
                </CardContent>
              </Card>
            ))}
          </div>

          <div className='flex flex-col gap-6'>
            <Card>
              <CardHeader>
                <CardTitle>{t('Usage Summary')}</CardTitle>
                <CardDescription>
                  {t(
                    'Aggregated external token usage by source and normalized model.'
                  )}
                </CardDescription>
              </CardHeader>
              <CardContent>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t('Source')}</TableHead>
                      <TableHead>{t('Model')}</TableHead>
                      <TableHead>{t('Tokens')}</TableHead>
                      <TableHead>{t('Input')}</TableHead>
                      <TableHead>{t('Cache')}</TableHead>
                      <TableHead>{t('Output')}</TableHead>
                      <TableHead>{t('Events')}</TableHead>
                      {isAdmin ? <TableHead>{t('Action')}</TableHead> : null}
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {summaryRows.length === 0 ? (
                      <TableRow>
                        <TableCell
                          colSpan={isAdmin ? 8 : 7}
                          className='text-muted-foreground py-6 text-center'
                        >
                          {t('No external usage yet')}
                        </TableCell>
                      </TableRow>
                    ) : (
                      summaryRows.map((row) => (
                        <TableRow
                          key={`${row.source}-${row.normalized_model_name}`}
                        >
                          <TableCell>
                            <Badge variant='secondary'>
                              {getExternalUsageSourceLabel(t, row.source)}
                            </Badge>
                          </TableCell>
                          <TableCell>{row.normalized_model_name}</TableCell>
                          <TableCell>
                            {row.total_tokens.toLocaleString()}
                          </TableCell>
                          <TableCell>
                            {row.input_tokens.toLocaleString()}
                          </TableCell>
                          <TableCell>
                            {row.cached_input_tokens.toLocaleString()}
                          </TableCell>
                          <TableCell>
                            {row.output_tokens.toLocaleString()}
                          </TableCell>
                          <TableCell>
                            {row.event_count.toLocaleString()}
                          </TableCell>
                          {isAdmin ? (
                            <TableCell>
                              <Button
                                size='icon-sm'
                                variant='ghost'
                                disabled={loading || !selectedUserId}
                                onClick={() => void handleDeleteModelData(row)}
                              >
                                <Trash2 />
                              </Button>
                            </TableCell>
                          ) : null}
                        </TableRow>
                      ))
                    )}
                  </TableBody>
                </Table>
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle>{t('Reporting Devices')}</CardTitle>
                <CardDescription>
                  {t('Issue one dedicated reporting credential per device.')}
                  {isAdmin && !canIssueCredential
                    ? ` ${t('Reporting credentials must be issued by the target user.')}`
                    : ''}
                </CardDescription>
              </CardHeader>
              <CardContent className='flex flex-col gap-4'>
                <div className='grid gap-3 md:grid-cols-2'>
                  <div className='grid gap-2'>
                    <Label htmlFor='device-name'>{t('Device Name')}</Label>
                    <Input
                      id='device-name'
                      value={deviceName}
                      onChange={(e) => setDeviceName(e.target.value)}
                      placeholder={t('MacBook Pro')}
                    />
                  </div>
                  <div className='grid gap-2'>
                    <Label htmlFor='device-fingerprint'>
                      {t('Device Fingerprint')}
                    </Label>
                    <Input
                      id='device-fingerprint'
                      value={deviceFingerprint}
                      onChange={(e) => setDeviceFingerprint(e.target.value)}
                      placeholder={t('codex-device-01')}
                    />
                  </div>
                </div>
                <div className='grid gap-2'>
                  <Label>{t('Allowed Sources')}</Label>
                  <div className='flex flex-wrap gap-3'>
                    {configuredClientSources.map((source) => {
                      const checked = selectedDeviceSources.includes(source)
                      return (
                        <label
                          key={source}
                          className='flex items-center gap-2 text-sm'
                        >
                          <Checkbox
                            checked={checked}
                            onCheckedChange={(nextChecked) => {
                              setSelectedDeviceSources((current) => {
                                if (nextChecked) {
                                  return current.includes(source)
                                    ? current
                                    : [...current, source]
                                }
                                return current.filter((item) => item !== source)
                              })
                            }}
                          />
                          <span>{getExternalUsageSourceLabel(t, source)}</span>
                        </label>
                      )
                    })}
                  </div>
                  <p className='text-muted-foreground text-sm'>
                    {t(
                      'Each user can register up to {{count}} reporting devices.',
                      {
                        count: maxDevicesPerUser,
                      }
                    )}
                  </p>
                </div>
                <div className='flex justify-end'>
                  <Button
                    disabled={
                      loading ||
                      !canIssueCredential ||
                      selectedDeviceSources.length === 0
                    }
                    onClick={() => void handleCreateCredential()}
                  >
                    <KeyRound />
                    {t('Issue Credential')}
                  </Button>
                </div>
                {credentialValue ? (
                  <Alert>
                    <AlertTitle>{t('Reporting Credential')}</AlertTitle>
                    <AlertDescription className='font-mono break-all'>
                      {credentialValue}
                    </AlertDescription>
                  </Alert>
                ) : null}
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t('Device')}</TableHead>
                      <TableHead>{t('Sources')}</TableHead>
                      <TableHead>{t('Status')}</TableHead>
                      <TableHead>{t('Last Report')}</TableHead>
                      <TableHead>{t('Created')}</TableHead>
                      <TableHead>{t('Action')}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {deviceRows.length === 0 ? (
                      <TableRow>
                        <TableCell
                          colSpan={6}
                          className='text-muted-foreground py-6 text-center'
                        >
                          {t('No reporting devices')}
                        </TableCell>
                      </TableRow>
                    ) : (
                      deviceRows.map((row) => (
                        <TableRow key={row.id}>
                          <TableCell>{row.device_name}</TableCell>
                          <TableCell>
                            {parseAllowedSources(row.allowed_sources, t)}
                          </TableCell>
                          <TableCell>
                            <Badge
                              variant={
                                row.status === 'active'
                                  ? 'secondary'
                                  : 'outline'
                              }
                            >
                              {getExternalUsageStatusLabel(t, row.status)}
                            </Badge>
                          </TableCell>
                          <TableCell>
                            {formatTimestamp(row.last_reported_at)}
                          </TableCell>
                          <TableCell>
                            {formatTimestamp(row.created_at)}
                          </TableCell>
                          <TableCell>
                            <Button
                              size='icon-sm'
                              variant='ghost'
                              disabled={loading || row.status !== 'active'}
                              onClick={() => void handleRevokeDevice(row.id)}
                            >
                              <Trash2 />
                            </Button>
                          </TableCell>
                        </TableRow>
                      ))
                    )}
                  </TableBody>
                </Table>
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle>{installerCommandCopy.title}</CardTitle>
                <CardDescription>
                  {installerCommandCopy.description}
                </CardDescription>
              </CardHeader>
              <CardContent className='flex flex-col gap-4'>
                {isAdmin && !canRequestInstallerCommand ? (
                  <Alert>
                    <AlertTitle>
                      {installerCommandCopy.unavailableTitle}
                    </AlertTitle>
                    <AlertDescription>
                      {installerCommandCopy.unavailableDescription}
                    </AlertDescription>
                  </Alert>
                ) : null}
                <div className='grid gap-3 md:grid-cols-[minmax(0,220px)_auto] md:items-end'>
                  <div className='grid gap-2'>
                    <Label htmlFor='installer-platform'>
                      {installerCommandCopy.platformLabel}
                    </Label>
                    <NativeSelect
                      id='installer-platform'
                      value={installerPlatform}
                      onChange={(e) =>
                        setInstallerPlatform(
                          e.target.value as ExternalUsageInstallerPlatform
                        )
                      }
                    >
                      {installerCommandCopy.platformOptions.map((option) => (
                        <NativeSelectOption
                          key={option.value}
                          value={option.value}
                        >
                          {option.label}
                        </NativeSelectOption>
                      ))}
                    </NativeSelect>
                  </div>
                  <Button
                    disabled={loading || !canRequestInstallerCommand}
                    onClick={() => void handleCreateInstallerCommand()}
                  >
                    <HardDriveUpload />
                    {installerCommandCopy.actionLabel}
                  </Button>
                </div>
                {installerCommand ? (
                  <div className='grid gap-4'>
                    {installerCommandExpiresAt ? (
                      <div className='text-muted-foreground text-sm'>
                        {installerCommandCopy.expiresLabel}:{' '}
                        {formatTimestamp(installerCommandExpiresAt)}
                      </div>
                    ) : null}
                    <SetupCommandBlock
                      title={installerCommandCopy.commandLabel}
                      value={installerCommand}
                      copiedLabel={t('Copied!')}
                    />
                  </div>
                ) : (
                  <p className='text-muted-foreground text-sm'>
                    {installerCommandCopy.emptyState}
                  </p>
                )}
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle>{t('Recent Report Batches')}</CardTitle>
                <CardDescription>
                  {t(
                    'Recent client upload batches for external usage reporting.'
                  )}
                </CardDescription>
              </CardHeader>
              <CardContent>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t('ID')}</TableHead>
                      <TableHead>{t('Device')}</TableHead>
                      <TableHead>{t('Source')}</TableHead>
                      <TableHead>{t('Status')}</TableHead>
                      <TableHead>{t('Accepted')}</TableHead>
                      <TableHead>{t('Duplicate')}</TableHead>
                      <TableHead>{t('Rejected')}</TableHead>
                      <TableHead>{t('Range')}</TableHead>
                      <TableHead>{t('Created')}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {reportBatches.length === 0 ? (
                      <TableRow>
                        <TableCell
                          colSpan={9}
                          className='text-muted-foreground py-6 text-center'
                        >
                          {t('No report batches')}
                        </TableCell>
                      </TableRow>
                    ) : (
                      reportBatches.map((row) => (
                        <TableRow key={row.id}>
                          <TableCell>{row.id}</TableCell>
                          <TableCell>{row.device_name || '-'}</TableCell>
                          <TableCell>
                            <Badge variant='secondary'>
                              {getExternalUsageSourceLabel(t, row.source)}
                            </Badge>
                          </TableCell>
                          <TableCell>
                            <Badge variant='outline'>
                              {getExternalUsageStatusLabel(t, row.status)}
                            </Badge>
                          </TableCell>
                          <TableCell>{row.accepted_count}</TableCell>
                          <TableCell>{row.duplicate_count}</TableCell>
                          <TableCell>{row.rejected_count}</TableCell>
                          <TableCell>
                            {row.occurred_from > 0
                              ? formatTimestamp(row.occurred_from)
                              : '-'}{' '}
                            ~{' '}
                            {row.occurred_to > 0
                              ? formatTimestamp(row.occurred_to)
                              : '-'}
                          </TableCell>
                          <TableCell>
                            {formatTimestamp(row.created_at)}
                          </TableCell>
                        </TableRow>
                      ))
                    )}
                  </TableBody>
                </Table>
              </CardContent>
            </Card>

            {isAdmin ? (
              <Card>
                <CardHeader>
                  <CardTitle>{t('Usage Details')}</CardTitle>
                  <CardDescription>
                    {t(
                      'Imported or client-reported external usage facts for diagnostics.'
                    )}
                  </CardDescription>
                </CardHeader>
                <CardContent className='flex flex-col gap-4'>
                  <div className='flex justify-end'>
                    <NativeSelect
                      className='w-full max-w-48'
                      value={detailSource}
                      onChange={(e) => {
                        setDetailSource(e.target.value)
                        setDetailPage(1)
                      }}
                    >
                      <NativeSelectOption value=''>
                        {t('All Sources')}
                      </NativeSelectOption>
                      {sourceOptions.map((source) => (
                        <NativeSelectOption key={source} value={source}>
                          {getExternalUsageSourceLabel(t, source)}
                        </NativeSelectOption>
                      ))}
                    </NativeSelect>
                  </div>
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>{t('Origin')}</TableHead>
                        <TableHead>{t('Source')}</TableHead>
                        <TableHead>{t('Status')}</TableHead>
                        <TableHead>{t('Model')}</TableHead>
                        <TableHead>{t('Tokens')}</TableHead>
                        <TableHead>{t('Input')}</TableHead>
                        <TableHead>{t('Cache')}</TableHead>
                        <TableHead>{t('Output')}</TableHead>
                        <TableHead>{t('Occurred')}</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {details.length === 0 ? (
                        <TableRow>
                          <TableCell
                            colSpan={9}
                            className='text-muted-foreground py-6 text-center'
                          >
                            {t('No details found')}
                          </TableCell>
                        </TableRow>
                      ) : (
                        details.map((row) => (
                          <TableRow
                            key={`${row.origin}-${row.source}-${row.event_identity}-${row.batch_id}`}
                          >
                            <TableCell>
                              <Badge variant='outline'>
                                {getExternalUsageOriginLabel(t, row.origin)}
                              </Badge>
                            </TableCell>
                            <TableCell>
                              <Badge variant='secondary'>
                                {getExternalUsageSourceLabel(t, row.source)}
                              </Badge>
                            </TableCell>
                            <TableCell>
                              {getExternalUsageStatusLabel(t, row.status)}
                            </TableCell>
                            <TableCell>{row.normalized_model_name}</TableCell>
                            <TableCell>
                              {row.total_tokens.toLocaleString()}
                            </TableCell>
                            <TableCell>
                              {row.input_tokens.toLocaleString()}
                            </TableCell>
                            <TableCell>
                              {row.cached_input_tokens.toLocaleString()}
                            </TableCell>
                            <TableCell>
                              {row.output_tokens.toLocaleString()}
                            </TableCell>
                            <TableCell>
                              {formatTimestamp(row.occurred_at)}
                            </TableCell>
                          </TableRow>
                        ))
                      )}
                    </TableBody>
                  </Table>
                  <div className='flex flex-col gap-3 pt-2 sm:flex-row sm:items-center sm:justify-between'>
                    <div className='text-muted-foreground text-sm'>
                      {t('Total {{count}} details', {
                        count: detailTotal.toLocaleString(),
                      })}
                    </div>
                    {detailTotal > detailPageSize ? (
                      <Pagination className='justify-end'>
                        <PaginationContent>
                          <PaginationItem>
                            <PaginationPrevious
                              href='#'
                              text={t('Previous')}
                              className={
                                detailPage <= 1
                                  ? 'pointer-events-none opacity-50'
                                  : undefined
                              }
                              onClick={(e) => {
                                e.preventDefault()
                                if (detailPage > 1) {
                                  setDetailPage((prev) => prev - 1)
                                }
                              }}
                            />
                          </PaginationItem>
                          {detailPageNumbers.map((item, index) => (
                            <PaginationItem key={`${item}-${index}`}>
                              {item === '...' ? (
                                <PaginationEllipsis />
                              ) : (
                                <PaginationLink
                                  href='#'
                                  isActive={item === detailPage}
                                  onClick={(e) => {
                                    e.preventDefault()
                                    setDetailPage(Number(item))
                                  }}
                                >
                                  {item}
                                </PaginationLink>
                              )}
                            </PaginationItem>
                          ))}
                          <PaginationItem>
                            <PaginationNext
                              href='#'
                              text={t('Next')}
                              className={
                                detailPage >= detailTotalPages
                                  ? 'pointer-events-none opacity-50'
                                  : undefined
                              }
                              onClick={(e) => {
                                e.preventDefault()
                                if (detailPage < detailTotalPages) {
                                  setDetailPage((prev) => prev + 1)
                                }
                              }}
                            />
                          </PaginationItem>
                        </PaginationContent>
                      </Pagination>
                    ) : null}
                  </div>
                </CardContent>
              </Card>
            ) : null}
          </div>
        </div>
      </div>
    </Main>
  )
}
