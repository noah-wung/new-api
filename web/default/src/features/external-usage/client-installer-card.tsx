import { useEffect, useRef, useState } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery } from '@tanstack/react-query'
import { Download04Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
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
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { CopyButton } from '@/components/copy-button'
import {
  createExternalUsageClientInstallerCommand,
  getExternalUsageClientInstallerOptions,
} from './api'
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
      auto_update: true,
    },
  })
  const sources = useWatch({ control: form.control, name: 'sources' })
  const includeClaude = sources.includes('claude_code')
  const platform = useWatch({ control: form.control, name: 'platform' })
  const autoUpdate = useWatch({ control: form.control, name: 'auto_update' })
  const userId = useAuthStore((state) => state.auth.user?.id)
  const options = useQuery({
    queryKey: ['external-usage-client-installer-options', userId],
    queryFn: getExternalUsageClientInstallerOptions,
    enabled: props.canRequest && availableSources.length > 0,
    staleTime: 30_000,
  })
  const signedReady = options.data?.automatic_updates_available === true
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
    requestGeneration.current++
    setCommand('')
    setExpiresAt(null)
  }, [allowed, signedReady])

  async function generateCommand(values: ClientInstallerInput) {
    if (!allowed) return
    invalidateCommand()
    const generation = requestGeneration.current
    try {
      const result = await mutation.mutateAsync(
        signedReady
          ? { ...values, managed_install: true }
          : { platform: values.platform, sources: values.sources }
      )
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
            {signedReady ? (
              <>
                <Field orientation='horizontal' data-disabled={!allowed}>
                  <Checkbox
                    id='collector-auto-update'
                    checked={autoUpdate === true}
                    disabled={!allowed}
                    onCheckedChange={(checked) => {
                      invalidateCommand()
                      form.setValue('auto_update', checked)
                    }}
                  />
                  <FieldLabel htmlFor='collector-auto-update'>
                    {t('Automatically check and silently update collectors')}
                  </FieldLabel>
                </Field>
                <FieldDescription>
                  {t(
                    'Checks daily. Updates are verified and installed in the background; failed updates restore the previous version.'
                  )}
                </FieldDescription>
                <FieldDescription>
                  {t(
                    'Existing devices must run the signed installation command once to enable automatic updates.'
                  )}
                </FieldDescription>
              </>
            ) : (
              <FieldDescription>
                {t(
                  'Automatic updates require HTTPS and an active signed release. Ordinary installation remains available with daily manual-update notifications.'
                )}
              </FieldDescription>
            )}
            {options.isError ? (
              <Alert>
                <AlertDescription>
                  {t(
                    'Automatic update availability could not be checked. Ordinary installation remains available.'
                  )}
                </AlertDescription>
              </Alert>
            ) : null}
            {includeClaude ? (
              <FieldDescription>
                {t(
                  'Claude Code detects its API address automatically. Non-gateway usage is reported; this gateway and its configured aliases are excluded. Restart Claude Code CLI and VS Code sessions after installation.'
                )}
              </FieldDescription>
            ) : null}
            <Button
              type='submit'
              disabled={
                !allowed ||
                sources.length === 0 ||
                mutation.isPending ||
                (options.isPending && options.isFetching)
              }
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
