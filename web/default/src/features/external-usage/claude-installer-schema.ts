import { z } from 'zod'

export const claudeInstallerSchema = z.object({
  platform: z.enum(['macos', 'windows']),
})

export type ClaudeInstallerInput = z.infer<typeof claudeInstallerSchema>
