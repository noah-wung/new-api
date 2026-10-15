import { z } from 'zod'

export const claudeInstallerSchema = z.object({
  platform: z.enum(['macos', 'windows']),
  upstream_url: z
    .string()
    .trim()
    .url()
    .max(2048)
    .refine((value) => {
      try {
        const url = new URL(value)
        return (
          ['http:', 'https:'].includes(url.protocol) &&
          !url.username &&
          !url.password &&
          !url.search &&
          !url.hash
        )
      } catch {
        return false
      }
    }),
  upstream_is_gateway: z.boolean(),
})

export type ClaudeInstallerInput = z.infer<typeof claudeInstallerSchema>
