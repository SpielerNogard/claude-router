<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import StatusBar from './components/StatusBar.vue'
import TokenDialog from './components/TokenDialog.vue'
import LiveView from './components/LiveView.vue'
import UsageView from './components/UsageView.vue'
import ActivityView from './components/ActivityView.vue'

const { t } = useI18n()
const tab = ref<'live' | 'usage' | 'activity'>('live')
</script>

<template>
  <StatusBar />
  <nav class="tabs">
    <button :class="{ active: tab === 'live' }" @click="tab = 'live'">{{ t('app.live') }}</button>
    <button :class="{ active: tab === 'usage' }" @click="tab = 'usage'">{{ t('app.usage') }}</button>
    <button :class="{ active: tab === 'activity' }" @click="tab = 'activity'">{{ t('app.activity') }}</button>
  </nav>
  <main>
    <LiveView v-if="tab === 'live'" />
    <UsageView v-else-if="tab === 'usage'" />
    <ActivityView v-else />
  </main>
  <TokenDialog />
</template>
