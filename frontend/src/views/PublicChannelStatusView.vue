<template>
  <div class="relative min-h-screen overflow-hidden bg-slate-950 text-white">
    <div class="pointer-events-none absolute inset-0">
      <div class="absolute -left-32 top-24 h-80 w-80 rounded-full bg-cyan-500/10 blur-3xl"></div>
      <div class="absolute right-0 top-0 h-96 w-96 rounded-full bg-emerald-500/10 blur-3xl"></div>
      <div class="absolute inset-0 bg-[radial-gradient(circle_at_top_left,rgba(20,184,166,0.12),transparent_32rem),linear-gradient(180deg,rgba(2,6,23,0)_0%,rgba(2,6,23,0.96)_72%)]"></div>
    </div>

    <header class="relative z-20 border-b border-white/10 bg-slate-950/55 px-6 py-4 backdrop-blur-xl">
      <nav class="mx-auto flex max-w-7xl items-center justify-between">
        <router-link to="/home" class="flex items-center gap-3">
          <div class="h-10 w-10 overflow-hidden rounded-xl bg-white/5 shadow-lg shadow-cyan-500/10 ring-1 ring-white/10">
            <img :src="siteLogo || '/logo.png'" alt="Logo" class="h-full w-full object-contain" />
          </div>
          <div>
            <div class="text-lg font-semibold tracking-tight text-white">{{ siteName }}</div>
            <div class="font-mono text-[11px] uppercase tracking-[0.24em] text-cyan-200/70">channel status</div>
          </div>
        </router-link>

        <div class="flex items-center gap-3">
          <LocaleSwitcher />
          <button
            @click="toggleTheme"
            class="rounded-lg p-2 text-slate-300 transition-colors hover:bg-white/10 hover:text-white"
            :title="isDark ? t('home.switchToLight') : t('home.switchToDark')"
          >
            <Icon v-if="isDark" name="sun" size="md" />
            <Icon v-else name="moon" size="md" />
          </button>
        </div>
      </nav>
    </header>

    <main class="relative z-10 mx-auto w-full max-w-7xl px-6 py-8 md:py-12">
      <section class="mb-8 flex flex-col gap-5 lg:flex-row lg:items-end lg:justify-between">
        <div class="max-w-2xl">
          <p class="mb-3 inline-flex rounded-full border border-cyan-300/20 bg-cyan-300/10 px-3 py-1 font-mono text-xs uppercase tracking-[0.22em] text-cyan-100">
            live public monitor
          </p>
          <h1 class="text-3xl font-bold tracking-tight text-white md:text-5xl">
            {{ t('nav.channelStatus') }}
          </h1>
          <p class="mt-3 text-sm leading-6 text-slate-300 md:text-base">
            {{ t('channelStatus.publicSubtitle') }}
          </p>
        </div>

        <MonitorHero
          class="lg:min-w-[34rem]"
          :overall-status="overallStatus"
          :interval-seconds="DEFAULT_INTERVAL_SECONDS"
          :window="currentWindow"
          :loading="loading"
          :auto-refresh="autoRefresh"
          @update:window="handleWindowChange"
          @refresh="manualReload"
        />
      </section>

      <MonitorCardGrid
        :items="items"
        :window="currentWindow"
        :countdown-seconds="countdown"
        :loading="loading"
        :detail-cache="detailCache"
        @card-click="openDetail"
      />

      <MonitorDetailDialog
        :show="showDetail"
        :monitor-id="detailTarget?.id ?? null"
        :title="detailTitle"
        @close="closeDetail"
      />
    </main>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, onBeforeUnmount, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import {
  list as listChannelMonitorViews,
  status as fetchChannelMonitorDetail,
  type UserMonitorView,
  type UserMonitorDetail,
} from '@/api/channelMonitor'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'
import MonitorHero, {
  type MonitorWindow,
  type OverallStatus,
} from '@/components/user/monitor/MonitorHero.vue'
import MonitorCardGrid from '@/components/user/monitor/MonitorCardGrid.vue'
import MonitorDetailDialog from '@/components/user/MonitorDetailDialog.vue'
import { DEFAULT_INTERVAL_SECONDS, STATUS_OPERATIONAL } from '@/constants/channelMonitor'
import { useAutoRefresh } from '@/composables/useAutoRefresh'

const { t } = useI18n()
const appStore = useAppStore()

const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'Sub2API')
const siteLogo = computed(() => appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '')
const isDark = ref(document.documentElement.classList.contains('dark'))

const items = ref<UserMonitorView[]>([])
const loading = ref(false)
const currentWindow = ref<MonitorWindow>('7d')
const detailCache = reactive<Record<number, UserMonitorDetail>>({})
const showDetail = ref(false)
const detailTarget = ref<UserMonitorView | null>(null)

let abortController: AbortController | null = null

const autoRefresh = useAutoRefresh({
  storageKey: 'channel-status-auto-refresh',
  intervals: [30, 60, 120] as const,
  defaultInterval: DEFAULT_INTERVAL_SECONDS,
  onRefresh: () => reload(true),
  shouldPause: () => document.hidden || loading.value,
})
const countdown = autoRefresh.countdown

const overallStatus = computed<OverallStatus>(() => {
  if (items.value.length === 0) return 'operational'
  for (const it of items.value) {
    if (it.primary_status === 'failed' || it.primary_status === 'error') return 'degraded'
    if (it.primary_status !== STATUS_OPERATIONAL) return 'degraded'
  }
  return 'operational'
})

const detailTitle = computed(() => {
  return detailTarget.value?.name || t('channelStatus.detailTitle')
})

function toggleTheme() {
  isDark.value = !isDark.value
  document.documentElement.classList.toggle('dark', isDark.value)
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
}

async function reload(silent = false) {
  if (abortController) abortController.abort()
  const ctrl = new AbortController()
  abortController = ctrl
  if (!silent) loading.value = true
  try {
    const res = await listChannelMonitorViews({ signal: ctrl.signal })
    if (ctrl.signal.aborted || abortController !== ctrl) return
    items.value = res.items || []
  } catch (err: unknown) {
    const e = err as { name?: string; code?: string }
    if (e?.name === 'AbortError' || e?.code === 'ERR_CANCELED') return
    appStore.showError(extractApiErrorMessage(err, t('channelStatus.loadError')))
  } finally {
    if (abortController === ctrl) {
      if (!silent) loading.value = false
      countdown.value = DEFAULT_INTERVAL_SECONDS
      abortController = null
    }
  }
}

async function manualReload() {
  await reload(false)
  if (currentWindow.value !== '7d') {
    await Promise.all(items.value.map(it => loadDetail(it.id, true)))
  }
}

async function loadDetail(id: number, force = false) {
  if (!force && detailCache[id]) return
  try {
    detailCache[id] = await fetchChannelMonitorDetail(id)
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('channelStatus.detailLoadError')))
  }
}

async function ensureDetailsForWindow() {
  if (currentWindow.value === '7d') return
  await Promise.all(items.value.map(it => loadDetail(it.id)))
}

async function handleWindowChange(value: MonitorWindow) {
  currentWindow.value = value
  await ensureDetailsForWindow()
}

function openDetail(row: UserMonitorView) {
  detailTarget.value = row
  showDetail.value = true
}

function closeDetail() {
  showDetail.value = false
  detailTarget.value = null
}

watch(items, () => {
  void ensureDetailsForWindow()
})

watch(
  () => appStore.cachedPublicSettings?.channel_monitor_enabled,
  (enabled) => {
    if (enabled === false) autoRefresh.stop()
    else if (autoRefresh.enabled.value) autoRefresh.start()
  },
)

onMounted(() => {
  void reload(false)
  if (appStore.cachedPublicSettings?.channel_monitor_enabled !== false) {
    autoRefresh.setEnabled(true)
  }
})

onBeforeUnmount(() => {
  if (abortController) abortController.abort()
})
</script>
