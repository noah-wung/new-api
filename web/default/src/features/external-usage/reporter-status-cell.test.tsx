import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import i18next from 'i18next'
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider } from 'react-i18next'
import { ReporterStatusCell, type ReporterStatus } from './reporter-status-cell'

function renderStatus(rows: ReporterStatus[], canView = true) {
  const i18n = i18next.createInstance()
  void i18n.init({
    lng: 'en',
    resources: { en: { translation: {} } },
    initAsync: false,
  })
  const client = new QueryClient()
  client.setQueryData(['external-usage-reporter-status', undefined], rows)
  return renderToStaticMarkup(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <ReporterStatusCell deviceId={7} canView={canView} />
      </QueryClientProvider>
    </I18nextProvider>
  )
}

test('missing update status does not imply the device is current', () => {
  const html = renderStatus([])
  assert.match(html, /Automatic update support not reported/)
  assert.doesNotMatch(html, /Up to date/)
})

test('device status shows available version, recovery and stale observations', () => {
  const rows: ReporterStatus[] = [
    {
      device_id: 7,
      reporter_version: '1.0.0',
      available_version: '1.1.0',
      auto_update: false,
      phase: 'recovery_required',
      error_code: 'stop_failed',
      last_check_at: 1,
      received_at: 1,
    },
  ]
  const html = renderStatus(rows)
  assert.match(html, /1\.0\.0/)
  assert.match(html, /Available version: 1\.1\.0/)
  assert.match(html, /Automatic updates off/)
  assert.match(html, /Update recovery required/)
  assert.match(html, /Update status is stale/)
  assert.match(html, /stop_failed/)
  assert.doesNotMatch(renderStatus(rows, false), /1\.0\.0|stop_failed/)
})
