export const THEMES = ['auto', 'light', 'dark'] as const
export type Theme = (typeof THEMES)[number]

export const parseTheme = (v: string | null): Theme =>
  (THEMES as readonly string[]).includes(v ?? '') ? (v as Theme) : 'auto'
