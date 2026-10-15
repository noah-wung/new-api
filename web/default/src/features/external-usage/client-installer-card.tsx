import { useEffect, useRef, useState } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation } from '@tanstack/react-query'
import { Download04Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'
import { formatTimestamp } from '@/lib/format'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldSet,
  FieldLegend,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { CopyButton } from '@/components/copy-button'
import { createExternalUsageClientInstallerCommand } from './api'
import {
  clientInstallerSchema,
  clientCollectorSources,
  type ClientInstallerInput,
} from './client-installer-schema'
import { normalizeInstallerCommandExpiry } from './installer-command'

export function ClientInstallerCard(props: {
  canRequest: boolean
  enabledSources: string[]
}) {
  const { t } = useTranslation()
  const availableSources = clientCollectorSources.filter((source) =>
    props.enabledSources.includes(source)
  )
  const form = useForm<ClientInstallerInput>({
    resolver: zodResolver(clientInstallerSchema),
    defaultValues: {
      platform: 'macos',
      sources: availableSources,
      upstream_url: 'https://api.anthropic.com',
      upstream_is_gateway: false,
      auto_update: true,
    },
  })
  const sources = useWatch({ control: form.control, name: 'sources' })
  const autoUpdate = useWatch({ control: form.control, name: 'auto_update' })
  const includeClaude = sources.includes('claude_code')
  const platform = useWatch({ control: form.control, name: 'platform' })
  const isGateway = useWatch({
    control: form.control,
    name: 'upstream_is_gateway',
  })
  const [command, setCommand] = useState('')
  const [expiresAt, setExpiresAt] = useState<number | null>(null)
  const requestGeneration = useRef(0)
  const mutation = useMutation({
    mutationFn: createExternalUsageClientInstallerCommand,
  })
  const allowed = props.canRequest && availableSources.length > 0

  function invalidateCommand() {
    requestGeneration.current++
    setCommand('')
    setExpiresAt(null)
  }

  useEffect(() => {
    if (!allowed) {
      requestGeneration.current++
      setCommand('')
      setExpiresAt(null)
    }
  }, [allowed])

  async function generateCommand(values: ClientInstallerInput) {
    if (!allowed) return
    invalidateCommand()
    const generation = requestGeneration.current
    try {
      const result = await mutation.mutateAsync(values)
      if (generation === requestGeneration.current && result.success) {
        setCommand(result.data?.command ?? '')
        setExpiresAt(normalizeInstallerCommandExpiry(result.data?.expires_at))
      }
    } catch {
      // The shared API interceptor presents request and business errors.
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('Client Usage Collector')}</CardTitle>
        <CardDescription>
          {t(
            'Select your clients and run one command to install their collectors. Reports are sent every 5 minutes in the background.'
          )}
        </CardDescription>
      </CardHeader>
      <CardContent className='flex flex-col gap-4'>
        {availableSources.length === 0 ? (
          <Alert>
            <AlertDescription>
              {t(
                'Client usage reporting must be enabled by the administrator.'
              )}
            </AlertDescription>
          </Alert>
        ) : null}
        {!props.canRequest ? (
          <Alert>
            <AlertDescription>
              {t(
                'Sign in as the selected user to generate an installer command.'
              )}
            </AlertDescription>
          </Alert>
        ) : null}
        <form onSubmit={form.handleSubmit(generateCommand)}>
          <FieldGroup>
            <Field data-disabled={!allowed}>
              <FieldLabel>{t('Platform')}</FieldLabel>
              <ToggleGroup
                value={[platform]}
                variant='outline'
                disabled={!allowed}
                aria-label={t('Platform')}
                onValueChange={(values) => {
                  const next = values[values.length - 1]
                  if (next === 'macos' || next === 'windows') {
                    invalidateCommand()
                    form.setValue('platform', next)
                  }
                }}
              >
                <ToggleGroupItem value='macos'>{t('macOS')}</ToggleGroupItem>
                <ToggleGroupItem value='windows'>
                  {t('Windows')}
                </ToggleGroupItem>
              </ToggleGroup>
            </Field>
            <FieldSet>
              <FieldLegend>{t('Clients to collect')}</FieldLegend>
              <FieldGroup>
                {clientCollectorSources.map((source) => (
                  <Field
                    key={source}
                    orientation='horizontal'
                    data-disabled={
                      !allowed || !availableSources.includes(source)
                    }
                  >
                    <Checkbox
                      id={`collector-${source}`}
                      disabled={!allowed || !availableSources.includes(source)}
                      checked={sources.includes(source)}
                      onCheckedChange={(checked) => {
                        invalidateCommand()
                        form.setValue(
                          'sources',
                          checked
                            ? [...sources, source]
                            : sources.filter((value) => value !== source),
                          { shouldValidate: true }
                        )
                      }}
                    />
                    <FieldLabel htmlFor={`collector-${source}`}>
                      {source === 'codex'
                        ? 'Codex'
                        : t('Claude Code (CLI and VS Code)')}
                      {!availableSources.includes(source)
                        ? ` — ${t('Not enabled')}`
                        : ''}
                    </FieldLabel>
                  </Field>
                ))}
              </FieldGroup>
              {sources.length === 0 ? (
                <FieldError>{t('Select at least one client.')}</FieldError>
              ) : null}
            </FieldSet>
            <Field orientation='horizontal' data-disabled={!allowed}>
              <Checkbox
                id='collector-auto-update'
                disabled={!allowed}
                checked={autoUpdate ?? true}
                onCheckedChange={(checked) => {
                  invalidateCommand()
                  form.setValue('auto_update', checked)
                }}
              />
              <FieldLabel htmlFor='collector-auto-update'>
                {t('Automatically check and silently update')}
              </FieldLabel>
            </Field>
            <FieldDescription>
              {t(
                'Checks daily. Updates apply to all managed collectors on this device and preserve reporting credentials and queued usage.'
              )}
            </FieldDescription>
            <FieldDescription>
              {t(
                'Signed releases and HTTPS are required. Existing devices must run the new installation command once to enable automatic updates.'
              )}
            </FieldDescription>
            {includeClaude ? (
              <>
                <Field
                  data-disabled={!allowed}
                  data-invalid={!!form.formState.errors.upstream_url}
                >
                  <FieldLabel htmlFor='claude-upstream-url'>
                    {t('Claude API address')}
                  </FieldLabel>
                  <Input
                    id='claude-upstream-url'
                    type='url'
                    required
                    disabled={!allowed}
                    aria-describedby='claude-upstream-help'
                    aria-invalid={!!form.formState.errors.upstream_url}
                    {...form.register('upstream_url', {
                      onChange: invalidateCommand,
                    })}
                  />
                  <FieldDescription id='claude-upstream-help'>
                    {t(
                      'Use the address configured in Claude Code. Installation stops if it does not match. Do not include API keys.'
                    )}
                  </FieldDescription>
                  {form.formState.errors.upstream_url ? (
                    <FieldError>
                      {t(
                        'Enter a valid HTTP(S) Claude API address without credentials, query parameters, or fragments.'
                      )}
                    </FieldError>
                  ) : null}
                </Field>
                <Field orientation='horizontal' data-disabled={!allowed}>
                  <Checkbox
                    id='claude-upstream-gateway'
                    disabled={!allowed}
                    checked={isGateway}
                    onCheckedChange={(checked) => {
                      invalidateCommand()
                      form.setValue('upstream_is_gateway', checked)
                    }}
                  />
                  <FieldLabel htmlFor='claude-upstream-gateway'>
                    {t('This address is already metered by this gateway.')}
                  </FieldLabel>
                </Field>
                <FieldDescription>
                  {t(
                    'Gateway usage is excluded. After installation, restart Claude Code CLI and VS Code sessions.'
                  )}
                </FieldDescription>
              </>
            ) : null}
            <Button
              type='submit'
              disabled={!allowed || sources.length === 0 || mutation.isPending}
            >
              <HugeiconsIcon icon={Download04Icon} data-icon='inline-start' />
              {t('Install Selected Collectors')}
            </Button>
          </FieldGroup>
        </form>
        {command ? (
          <div className='flex flex-col gap-2'>
            {expiresAt ? (
              <p className='text-muted-foreground text-sm'>
                {t('Expires At')}: {formatTimestamp(expiresAt)}
              </p>
            ) : null}
            <div className='flex items-center justify-between gap-2'>
              <span>{t('Installer Command')}</span>
              <CopyButton
                value={command}
                tooltip={t('Installer Command')}
                aria-label={t('Copy installer command')}
              />
            </div>
            <pre className='bg-muted overflow-x-auto rounded-md border p-3 text-xs break-all whitespace-pre-wrap'>
              {command}
            </pre>
          </div>
        ) : (
          <p className='text-muted-foreground text-sm'>
            {t(
              'Generate a command, then run it once on the device where your clients run.'
            )}
          </p>
        )}
      </CardContent>
    </Card>
  )
}
