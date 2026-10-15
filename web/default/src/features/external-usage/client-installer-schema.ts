import { z } from 'zod'
import { claudeInstallerSchema } from './claude-installer-schema'

export const clientCollectorSources = ['codex', 'claude_code'] as const

export const clientInstallerSchema = z
  .object({
    platform: claudeInstallerSchema.shape.platform,
    sources: z
      .array(z.enum(clientCollectorSources))
      .min(1)
      .max(2)
      .refine((sources) => new Set(sources).size === sources.length),
    upstream_url: z.string().trim(),
    upstream_is_gateway: z.boolean(),
  })
  .superRefine((values, context) => {
    if (
      values.sources.includes('claude_code') &&
      !claudeInstallerSchema.shape.upstream_url.safeParse(values.upstream_url)
        .success
    ) {
      context.addIssue({
        code: 'custom',
        path: ['upstream_url'],
        message: 'Invalid Claude upstream URL',
      })
    }
  })

export type ClientInstallerInput = z.infer<typeof clientInstallerSchema>
