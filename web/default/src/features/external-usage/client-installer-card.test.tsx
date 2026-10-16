import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import i18next from 'i18next'
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider } from 'react-i18next'
import { ClientInstallerCard } from './client-installer-card'

function renderInstaller(
  canRequest: boolean,
  enabledSources: string[],
  ready = true
) {
  const i18n = i18next.createInstance()
  void i18n.init({
    lng: 'en',
    resources: { en: { translation: {} } },
    initAsync: false,
  })
  const client = new QueryClient()
  client.setQueryData(['external-usage-client-installer-options', undefined], {
    automatic_updates_available: ready,
  })
  return renderToStaticMarkup(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <ClientInstallerCard
          canRequest={canRequest}
          enabledSources={enabledSources}
        />
      </QueryClientProvider>
    </I18nextProvider>
  )
}

test('one installer presents both clients without requiring an API address when both are enabled', () => {
  const html = renderInstaller(true, ['codex', 'claude_code'])
  assert.match(html, /Client Usage Collector/)
  assert.match(html, /Generate Installer Command/)
  assert.match(html, /Codex/)
  assert.match(html, /Claude Code \(CLI and VS Code\)/)
  assert.doesNotMatch(html, /Claude API address/)
  assert.doesNotMatch(html, /already metered/)
  assert.match(html, /detects its API address automatically/)
  assert.match(html, /every 5 minutes/)
  assert.match(html, /choose your collection sources in the terminal/)
  assert.match(html, /Run the command again to add a collection source/)
  assert.doesNotMatch(html, /id="collector-codex"|id="collector-claude_code"/)
  assert.match(html, /Checks daily/)
  assert.match(html, /failed updates restore the previous version/)
  assert.match(html, /Automatically check and silently update/)
  const control = html.match(/<[^>]*id="collector-auto-update"[^>]*>/)?.[0]
  assert.ok(control)
  assert.match(control, /data-checked=""|checked=""|aria-checked="true"/)
  assert.match(
    html,
    /Existing devices must run the signed installation command once/
  )
  assert.equal((html.match(/<form/g) ?? []).length, 1)
})

test('Codex-only installs do not require a Claude address', () => {
  const html = renderInstaller(true, ['codex'])
  assert.doesNotMatch(html, /Claude API address/)
  assert.match(html, /Available collectors: Codex/)
  assert.doesNotMatch(html, /Claude Code \(CLI and VS Code\)/)
})

test('disabled sources and viewing another user cannot generate a command', () => {
  const disabled = renderInstaller(true, [])
  assert.match(disabled, /must be enabled by the administrator/)
  assert.match(
    disabled,
    /<button[^>]*disabled[^>]*type="submit"|<button[^>]*type="submit"[^>]*disabled/
  )
  const anotherUser = renderInstaller(false, ['codex', 'claude_code'])
  assert.match(anotherUser, /Sign in as the selected user/)
  assert.match(
    anotherUser,
    /<button[^>]*disabled[^>]*type="submit"|<button[^>]*type="submit"[^>]*disabled/
  )
})

test('unconfigured signed updates retain ordinary installation', () => {
  const html = renderInstaller(true, ['codex', 'claude_code'], false)
  assert.doesNotMatch(html, /id="collector-auto-update"/)
  assert.match(html, /Ordinary installation remains available/)
  assert.doesNotMatch(html, /Claude API address/)
})
