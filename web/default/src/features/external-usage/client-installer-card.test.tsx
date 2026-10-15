import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import i18next from 'i18next'
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider } from 'react-i18next'
import { ClientInstallerCard } from './client-installer-card'

function renderInstaller(canRequest: boolean, enabledSources: string[]) {
  const i18n = i18next.createInstance()
  void i18n.init({
    lng: 'en',
    resources: { en: { translation: {} } },
    initAsync: false,
  })
  return renderToStaticMarkup(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={new QueryClient()}>
        <ClientInstallerCard
          canRequest={canRequest}
          enabledSources={enabledSources}
        />
      </QueryClientProvider>
    </I18nextProvider>
  )
}

test('one installer presents both clients and the Claude route when both are enabled', () => {
  const html = renderInstaller(true, ['codex', 'claude_code'])
  assert.match(html, /Client Usage Collector/)
  assert.match(html, /Install Selected Collectors/)
  assert.match(html, /Codex/)
  assert.match(html, /Claude Code \(CLI and VS Code\)/)
  assert.match(html, /Claude API address/)
  assert.match(html, /every 5 minutes/)
  assert.match(html, /Automatically check and silently update/)
  assert.match(
    html,
    /Existing devices must run the new installation command once/
  )
  assert.equal((html.match(/<form/g) ?? []).length, 1)
})

test('Codex-only installs do not require a Claude address', () => {
  const html = renderInstaller(true, ['codex'])
  assert.doesNotMatch(html, /Claude API address/)
  assert.match(html, /Not enabled/)
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
