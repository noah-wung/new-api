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
import * as z from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useResetForm } from '../hooks/use-reset-form'
import { useUpdateOption } from '../hooks/use-update-option'
import { safeNumberFieldProps } from '../utils/numeric-field'

const schema = z.object({
  'external_usage_setting.enabled': z.boolean(),
  'external_usage_setting.max_devices_per_user': z.coerce.number().int().min(1).max(3),
  'external_usage_setting.detail_retention_days': z.coerce.number().int().min(0).max(3650),
  'external_usage_setting.accept_window_days': z.coerce.number().int().min(0).max(3650),
  allowed_sources_text: z.string(),
})

type ExternalUsageSettingsFormValues = z.infer<typeof schema>

type ExternalUsageSettingsSectionProps = {
  defaultValues: {
    'external_usage_setting.enabled': boolean
    'external_usage_setting.max_devices_per_user': number
    'external_usage_setting.detail_retention_days': number
    'external_usage_setting.accept_window_days': number
    'external_usage_setting.allowed_sources': string[]
  }
}

function normalizeAllowedSources(value: string) {
  const seen = new Set<string>()
  return value
    .split('\n')
    .map((item) => item.trim().toLowerCase())
    .filter((item) => {
      if (!item || seen.has(item)) return false
      seen.add(item)
      return true
    })
}

export function ExternalUsageSettingsSection({
  defaultValues,
}: ExternalUsageSettingsSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const form = useForm<ExternalUsageSettingsFormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      'external_usage_setting.enabled':
        defaultValues['external_usage_setting.enabled'],
      'external_usage_setting.max_devices_per_user':
        defaultValues['external_usage_setting.max_devices_per_user'],
      'external_usage_setting.detail_retention_days':
        defaultValues['external_usage_setting.detail_retention_days'],
      'external_usage_setting.accept_window_days':
        defaultValues['external_usage_setting.accept_window_days'],
      allowed_sources_text:
        defaultValues['external_usage_setting.allowed_sources'].join('\n'),
    },
  })

  useResetForm(form, {
    'external_usage_setting.enabled':
      defaultValues['external_usage_setting.enabled'],
    'external_usage_setting.max_devices_per_user':
      defaultValues['external_usage_setting.max_devices_per_user'],
    'external_usage_setting.detail_retention_days':
      defaultValues['external_usage_setting.detail_retention_days'],
    'external_usage_setting.accept_window_days':
      defaultValues['external_usage_setting.accept_window_days'],
    allowed_sources_text:
      defaultValues['external_usage_setting.allowed_sources'].join('\n'),
  })

  const onSubmit = async (values: ExternalUsageSettingsFormValues) => {
    const normalizedAllowedSources = normalizeAllowedSources(
      values.allowed_sources_text
    )
    const initialAllowedSources = normalizeAllowedSources(
      defaultValues['external_usage_setting.allowed_sources'].join('\n')
    )

    const updates: Array<{ key: string; value: string | boolean | number }> = []

    if (
      values['external_usage_setting.enabled'] !==
      defaultValues['external_usage_setting.enabled']
    ) {
      updates.push({
        key: 'external_usage_setting.enabled',
        value: values['external_usage_setting.enabled'],
      })
    }

    if (
      values['external_usage_setting.max_devices_per_user'] !==
      defaultValues['external_usage_setting.max_devices_per_user']
    ) {
      updates.push({
        key: 'external_usage_setting.max_devices_per_user',
        value: values['external_usage_setting.max_devices_per_user'],
      })
    }

    if (
      values['external_usage_setting.detail_retention_days'] !==
      defaultValues['external_usage_setting.detail_retention_days']
    ) {
      updates.push({
        key: 'external_usage_setting.detail_retention_days',
        value: values['external_usage_setting.detail_retention_days'],
      })
    }

    if (
      values['external_usage_setting.accept_window_days'] !==
      defaultValues['external_usage_setting.accept_window_days']
    ) {
      updates.push({
        key: 'external_usage_setting.accept_window_days',
        value: values['external_usage_setting.accept_window_days'],
      })
    }

    if (
      JSON.stringify(normalizedAllowedSources) !==
      JSON.stringify(initialAllowedSources)
    ) {
      updates.push({
        key: 'external_usage_setting.allowed_sources',
        value: JSON.stringify(normalizedAllowedSources),
      })
    }

    for (const update of updates) {
      await updateOption.mutateAsync(update)
    }
  }

  return (
    <SettingsSection title={t('External Usage')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
            saveLabel='Save external usage settings'
          />
          <FormField
            control={form.control}
            name='external_usage_setting.enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable External Usage Reporting')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Allow administrator Cursor imports and dedicated client-side usage reporting outside the API gateway.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          <FormField
            control={form.control}
            name='external_usage_setting.max_devices_per_user'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Max Devices Per User')}</FormLabel>
                <FormControl>
                  <Input type='number' min='1' max='3' {...safeNumberFieldProps(field)} />
                </FormControl>
                <FormDescription>
                  {t('Each user can register up to {{count}} reporting devices.', {
                    count: 3,
                  })}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='external_usage_setting.detail_retention_days'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Detail Retention Days')}</FormLabel>
                <FormControl>
                  <Input type='number' min='0' max='3650' {...safeNumberFieldProps(field)} />
                </FormControl>
                <FormDescription>
                  {t('Number of days to retain imported and client-reported usage details. 0 keeps them permanently.')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='external_usage_setting.accept_window_days'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Accept Window Days')}</FormLabel>
                <FormControl>
                  <Input type='number' min='0' max='3650' {...safeNumberFieldProps(field)} />
                </FormControl>
                <FormDescription>
                  {t('Reject imported or reported events older than this many days. 0 accepts any event age.')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='allowed_sources_text'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Allowed Sources')}</FormLabel>
                <FormControl>
                  <Textarea
                    rows={5}
                    placeholder={t('codex\nzcode\nminimax_code')}
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t('One source per line. Values are normalized to lowercase and duplicates are removed before saving.')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
