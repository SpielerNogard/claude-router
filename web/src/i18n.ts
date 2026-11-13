import { createI18n } from 'vue-i18n'
import en from './locales/en.json'
import de from './locales/de.json'

export const LOCALES = ['en', 'de'] as const
export type Locale = (typeof LOCALES)[number]
const LOCALE_KEY = 'router-locale'

function stored(): Locale {
  try {
    const l = localStorage.getItem(LOCALE_KEY)
    return (LOCALES as readonly string[]).includes(l ?? '') ? (l as Locale) : 'en'
  } catch {
    return 'en'
  }
}

export const i18n = createI18n({ legacy: false, locale: stored(), fallbackLocale: 'en', messages: { en, de } })

export function setLocale(l: Locale) {
  i18n.global.locale.value = l
  document.documentElement.lang = l
  try {
    localStorage.setItem(LOCALE_KEY, l)
  } catch {
    /* choice then only lasts until reload */
  }
}
