import { onMounted, onUnmounted } from 'vue'

export const nextDelay = (baseMs: number, failures: number, maxMs = 30_000) =>
  Math.min(baseMs * 2 ** failures, maxMs)

// The task reports failures by throwing; the next run then waits longer.
export function usePoll(task: () => Promise<void>, baseMs: number): void {
  let failures = 0
  let timer: ReturnType<typeof setTimeout> | undefined
  let stopped = false
  const run = async () => {
    try {
      await task()
      failures = 0
    } catch {
      failures++
    }
    if (!stopped) timer = setTimeout(run, nextDelay(baseMs, failures))
  }
  onMounted(run)
  onUnmounted(() => {
    stopped = true
    clearTimeout(timer)
  })
}
