<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getJSON } from '../lib/api'
import { docTabs, isTruncated, prettyBody, type DocKind } from '../lib/docs'

const props = defineProps<{ requestId: string }>()
const { t } = useI18n()

const docs = ref<Record<string, string> | null>(null)
const error = ref('')
const active = ref<DocKind | null>(null)
const copied = ref<'' | 'ok' | 'failed'>('')

async function load() {
  if (!props.requestId) {
    docs.value = {}
    return
  }
  try {
    docs.value = await getJSON<Record<string, string>>(`/api/messages/by-request/${encodeURIComponent(props.requestId)}`)
  } catch (e) {
    error.value = (e as Error).message
    return
  }
  active.value = docTabs(docs.value)[0] ?? null
}
load()

const tabs = computed(() => (docs.value ? docTabs(docs.value) : []))
const body = computed(() => (docs.value && active.value ? docs.value[active.value] : ''))

// navigator.clipboard is missing outside a secure context (http on a LAN address).
async function copy() {
  try {
    await navigator.clipboard.writeText(body.value)
    copied.value = 'ok'
  } catch {
    copied.value = 'failed'
  }
  setTimeout(() => (copied.value = ''), 1500)
}
</script>

<template>
  <div class="docs">
    <p v-if="error" class="error">{{ error }}</p>
    <p v-else-if="!docs" class="note">{{ t('docs.loading') }}</p>
    <p v-else-if="tabs.length === 0" class="note">{{ t('docs.nothing') }}</p>
    <template v-else>
      <div class="toolbar">
        <span class="segmented">
          <button v-for="k in tabs" :key="k" :class="{ active: active === k }" @click="active = k">{{ t(`docs.${k}`) }}</button>
        </span>
        <span v-if="isTruncated(body)" class="chip blocked">{{ t('docs.truncated') }}</span>
        <button @click="copy">{{ copied === 'ok' ? t('docs.copied') : copied === 'failed' ? t('docs.copyFailed') : t('docs.copy') }}</button>
      </div>
      <pre>{{ prettyBody(body) }}</pre>
    </template>
  </div>
</template>
