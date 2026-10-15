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
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { CopyButton } from '@/components/copy-button'
import { createExternalUsageClaudeInstallerCommand } from './api'
import {
  claudeInstallerSchema,
  type ClaudeInstallerInput,
} from './claude-installer-schema'
import { normalizeInstallerCommandExpiry } from './installer-command'

export function ClaudeInstallerCard(props: {
  canRequest: boolean
  enabled: boolean
}) {
  const { t } = useTranslation()
  const form = useForm<ClaudeInstallerInput>({
    resolver: zodResolver(claudeInstallerSchema),
    defaultValues: {
      platform: 'macos',
      upstream_url: 'https://api.anthropic.com',
      upstream_is_gateway: false,
    },
  })
  const platform = useWatch({ control: form.control, name: 'platform' })
  const isGateway = useWatch({
    control: form.control,
    name: 'upstream_is_gateway',
  })
  const [command, setCommand] = useState('')
  const [expiresAt, setExpiresAt] = useState<number | null>(null)
  const requestGeneration = useRef(0)
  const mutation = useMutation({
    mutationFn: createExternalUsageClaudeInstallerCommand,
  })
  const allowed = props.canRequest && props.enabled

  function invalidateCommand() {
    requestGeneration.current++
    setCommand('')
    setExpiresAt(null)
  }

  useEffect(() => {
    if (!props.canRequest || !props.enabled) {
      requestGeneration.current++
      setCommand('')
      setExpiresAt(null)
    }
  }, [props.canRequest, props.enabled])

  async function generateCommand(values: ClaudeInstallerInput) {
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
        <CardTitle>{t('Claude Code Collector')}</CardTitle>
        <CardDescription>
          {t(
            'Install one collector for Claude Code CLI and the official VS Code extension.'
          )}
        </CardDescription>
      </CardHeader>
      <CardContent className='flex flex-col gap-4'>
        {!props.enabled ? (
          <Alert>
            <AlertDescription>
              {t(
                'Claude Code usage reporting must be enabled by the administrator.'
              )}
            </AlertDescription>
          </Alert>
        ) : null}
        {!props.canRequest ? (
          <Alert>
            <AlertDescription>
              {t(
                'Sign in as the selected user to generate a Claude Code installer command.'
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
                aria-label={t('Claude Code platform')}
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
            <Button type='submit' disabled={!allowed || mutation.isPending}>
              <HugeiconsIcon icon={Download04Icon} data-icon='inline-start' />
              {t('Install Claude Code Collector')}
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
                aria-label={t('Copy Claude Code installer command')}
              />
            </div>
            <pre className='bg-muted overflow-x-auto rounded-md border p-3 text-xs break-all whitespace-pre-wrap'>
              {command}
            </pre>
          </div>
        ) : (
          <p className='text-muted-foreground text-sm'>
            {t(
              'Generate a command, then run it on the device where Claude Code runs.'
            )}
          </p>
        )}
      </CardContent>
    </Card>
  )
}
