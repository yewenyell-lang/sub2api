import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import HarvestFlowView from '../HarvestFlowView.vue'

const { getFlow } = vi.hoisted(() => ({ getFlow: vi.fn() }))

vi.mock('@/api/admin/accounts', () => ({
  getCodexHarvestFlow: getFlow,
  updateCodexSkipHarvest: vi.fn(),
  updateCodexHarvestConfig: vi.fn()
}))
// vue-i18n 被整体替换，所以模块里用到的导出都必须提供。
// 组件现在会经 @/api/client → @/i18n 间接使用 createI18n，
// 只 mock useI18n 会让 createI18n 变成 undefined，模块初始化即报错。
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
  createI18n: () => ({ global: { t: (key: string) => key, locale: { value: 'zh' } } })
}))
vi.mock('@/stores', () => ({
  useAppStore: () => ({
    showSuccess: vi.fn(),
    showError: vi.fn(),
    showInfo: vi.fn(),
    showWarning: vi.fn()
  })
}))
vi.mock('@/components/layout/AppLayout.vue', () => ({
  default: { template: '<main><slot /></main>' }
}))

function response() {
  return {
    harvest: { enabled: false, fail_closed: false, strategy: 'standby', models: [] },
    sidecar: { reachable: false },
    stages: [],
    accounts: [{
      id: 1,
      name: 'Test account',
      status: 'active',
      schedulable: true,
      ready_count: 0,
      tickets: null
    }],
    counts: {
      tickets_ready: 0, tickets_blocked: 0, probe_hit: 0, probe_miss: 0,
      ticket_accept: 0, ticket_reject: 0, select_ok: 0, select_skip: 0, select_fail: 0
    },
    events: []
  }
}

describe('HarvestFlowView nullable API lists', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    getFlow.mockReset()
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it.each([
    {
      name: 'disabled harvesting returns null tickets',
      payload: response(),
      text: '0/0'
    },
    {
      name: 'no eligible accounts returns null accounts',
      payload: { ...response(), accounts: null },
      text: 'admin.harvestFlow.noAccounts'
    },
    {
      name: 'null events still render the empty state',
      payload: { ...response(), accounts: [], events: null },
      text: 'admin.harvestFlow.noEvents'
    },
    {
      name: 'normal ticket lists still render',
      payload: {
        ...response(),
        accounts: [{
          ...response().accounts[0],
          tickets: [{ model: 'gpt-5.6-sol', ready: false, blocked: false, remaining_seconds: 0 }]
        }]
      },
      text: 'gpt-5.6-sol'
    }
  ])('$name', async ({ payload, text }) => {
    getFlow.mockResolvedValue(payload)
    const errors: unknown[] = []
    const wrapper = mount(HarvestFlowView, {
      global: {
        stubs: { Icon: true, LoadingSpinner: true },
        config: { errorHandler: (error) => { errors.push(error) } }
      }
    })
    try {
      await flushPromises()
      expect(errors).toEqual([])
      expect(wrapper.text()).toContain('admin.harvestFlow.title')
      expect(wrapper.text()).toContain(text)
    } finally {
      wrapper.unmount()
    }
  })
})

describe('HarvestFlowView manual tasks and node names', () => {
  it('shows external proxy mode without a sidecar error or node pool', async () => {
    getFlow.mockResolvedValue({ ...response(), sidecar: { mode: 'external', reachable: false }, stages: [{ id: 'node', status: 'idle', detail: 'external_proxy' }] })
    const wrapper = mount(HarvestFlowView, { global: { stubs: { Icon: true, LoadingSpinner: true } } })
    try {
      await flushPromises()
      expect(wrapper.text()).toContain('admin.harvestFlow.externalProxy')
      expect(wrapper.text()).toContain('admin.harvestFlow.externalProxyHint')
      expect(wrapper.text()).not.toContain('admin.harvestFlow.sidecarOffline')
      expect(wrapper.text()).not.toContain('admin.harvestFlow.waitingSidecar')
      expect(wrapper.text()).not.toContain('CODEX-ROTATE')
    } finally { wrapper.unmount() }
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it.each(['fetch rejection', 'late stream frame'])('keeps the new task active after an old %s', async (mode) => {
    getFlow.mockResolvedValue(response())
    const tasks: { signal: AbortSignal; resolve: (value: Response) => void; reject: (reason: Error) => void }[] = []
    vi.stubGlobal('fetch', vi.fn((_url, options: RequestInit) => new Promise<Response>((resolve, reject) => {
      tasks.push({ signal: options.signal as AbortSignal, resolve, reject })
    })))
    const wrapper = mount(HarvestFlowView, { global: { stubs: { Icon: true, LoadingSpinner: true, Teleport: true } } })
    const button = (label: string) => wrapper.findAll('button').find(b => b.text().includes(label))!
    let resolveFrame!: (frame: { done: boolean; value: Uint8Array }) => void
    try {
      await flushPromises()
      await wrapper.find('input[type="text"]').trigger('focus')
      await button('Test account').trigger('click')
      await button('开始打票').trigger('click')
      expect(tasks).toHaveLength(1)
      if (mode === 'late stream frame') {
        tasks[0].resolve({ ok: true, status: 200, body: { getReader: () => ({ read: () => new Promise(resolve => { resolveFrame = resolve }) }) } } as unknown as Response)
        await flushPromises()
      }
      await button('停止打票').trigger('click')
      expect(tasks[0].signal.aborted).toBe(true)
      await button('开始打票').trigger('click')
      expect(tasks).toHaveLength(2)
      if (mode === 'fetch rejection') {
        tasks[0].reject(new DOMException('Aborted', 'AbortError'))
      } else {
        resolveFrame({ done: false, value: new TextEncoder().encode('data: {"done":true,"message":"old task completed","tickets_stored":1}\n\n') })
      }
      await flushPromises()
      expect(button('停止打票')).toBeDefined()
      expect(button('开始打票')).toBeUndefined()
      expect(wrapper.text()).not.toContain('old task completed')
      expect(tasks[1].signal.aborted).toBe(false)
    } finally {
      wrapper.unmount()
      expect(tasks[tasks.length - 1].signal.aborted).toBe(true)
      tasks[tasks.length - 1].reject(new DOMException('Aborted', 'AbortError'))
      await flushPromises()
    }
  })

  it('renders subscription labels while retaining ID fallback for older responses', async () => {
    getFlow.mockResolvedValue({ ...response(),
      sidecar: { reachable: true, now: 'node-a760', now_name: '🇯🇵 日本 东京' },
      events: [
        { id: '1', at: '2026-09-21T00:00:00Z', stage: 'node', kind: 'rotate', node: 'node-a760', node_name: '🇯🇵 日本 东京', result: 'LoadBalance' },
        { id: '2', at: '2026-09-21T00:00:00Z', stage: 'node', kind: 'rotate', node: 'legacy-node', result: 'LoadBalance' },
      ],
    })
    const wrapper = mount(HarvestFlowView, { global: { stubs: { Icon: true, LoadingSpinner: true } } })
    try {
      await flushPromises()
      expect(wrapper.text()).toContain('🇯🇵 日本 东京')
      expect(wrapper.text()).not.toContain('node-a760')
      expect(wrapper.text()).toContain('legacy-node')
      expect(wrapper.text()).toContain('admin.harvestFlow.nodePolicyHint')
      expect(wrapper.find('option[value="never"]').exists()).toBe(false)
    } finally { wrapper.unmount() }
  })
})
