<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { connected, getJSON, type Health } from '../lib/api'
import { usePoll } from '../lib/poll'
import { fmtReset } from '../lib/limits'
import { LOCALES, setLocale, type Locale } from '../i18n'
import { setTheme, theme } from '../charts'
import { THEMES, type Theme } from '../lib/theme'

const { t, locale } = useI18n()
const blocked = ref<Record<string, string>>({})

const now = ref(new Date())

usePoll(async () => {
  blocked.value = (await getJSON<Health>('/healthz')).blocked ?? {}
  now.value = new Date()
}, 10_000)

const chips = computed(() =>
  Object.entries(blocked.value).map(([provider, until]) =>
    t('status.blockedUntil', { provider, time: fmtReset(until, now.value, locale.value) }),
  ),
)
</script>

<template>
  <header class="status">
    <span :class="['dot', connected ? 'ok' : 'down']" :title="connected ? t('status.connected') : t('status.disconnected')" />
    <span class="logo">⇄</span>
    <strong>claude-router</strong>
    <span v-if="chips.length === 0" class="chip free">{{ t('status.allFree') }}</span>
    <span v-for="c in chips" :key="c" class="chip blocked">{{ c }}</span>
    <label class="lang">
      {{ t('status.language') }}
      <select :value="locale" @change="setLocale(($event.target as HTMLSelectElement).value as Locale)">
        <option v-for="l in LOCALES" :key="l" :value="l">{{ l.toUpperCase() }}</option>
      </select>
    </label>
    <label>
      {{ t('theme.label') }}
      <select :value="theme" @change="setTheme(($event.target as HTMLSelectElement).value as Theme)">
        <option v-for="th in THEMES" :key="th" :value="th">{{ t(`theme.${th}`) }}</option>
      </select>
    </label>
  </header>
</template>
