<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { authNeeded, setToken } from '../lib/api'

const { t } = useI18n()
const value = ref('')

function save() {
  if (!value.value) return
  setToken(value.value)
  // Reload instead of waiting: after the 401 failures the polls are in backoff (up to 30 s).
  location.reload()
}
</script>

<template>
  <div v-if="authNeeded" class="overlay">
    <form class="dialog" @submit.prevent="save">
      <h2>{{ t('auth.title') }}</h2>
      <p>{{ t('auth.hint') }}</p>
      <input v-model="value" type="password" autocomplete="off" autofocus />
      <button type="submit">{{ t('auth.save') }}</button>
    </form>
  </div>
</template>
