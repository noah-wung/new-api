import { z } from 'zod'
import { claudeInstallerSchema } from './claude-installer-schema'

export const clientCollectorSources = ['codex', 'claude_code'] as const

export const clientInstallerSchema = z.object({
  platform: claudeInstallerSchema.shape.platform,
  auto_update: z.boolean().optional(),
  managed_install: z.boolean().optional(),
  sources: z
    .array(z.enum(clientCollectorSources))
    .min(1)
    .max(2)
    .refine((sources) => new Set(sources).size === sources.length),
})

export type ClientInstallerInput = z.infer<typeof clientInstallerSchema>
