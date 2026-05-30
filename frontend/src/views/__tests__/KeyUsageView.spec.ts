import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'

import KeyUsageView from '../KeyUsageView.vue'

const { showInfo, showSuccess, showError, fetchPublicSettings } = vi.hoisted(() => ({
  showInfo: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn(),
  fetchPublicSettings: vi.fn(),
}))

const messages: Record<string, string> = {
  'keyUsage.title': 'API Key Usage',
  'keyUsage.subtitle': 'Usage status',
  'keyUsage.placeholder': 'sk-test',
  'keyUsage.query': 'Query',
  'keyUsage.querying': 'Querying...',
  'keyUsage.privacyNote': 'Privacy note',
  'keyUsage.dateRange': 'Date Range:',
  'keyUsage.dateRangeToday': 'Today',
  'keyUsage.dateRange7d': '7 Days',
  'keyUsage.dateRange30d': '30 Days',
  'keyUsage.dateRange90d': '90 Days',
  'keyUsage.dateRangeCustom': 'Custom',
  'keyUsage.apply': 'Apply',
  'keyUsage.used': 'Used',
  'keyUsage.detailInfo': 'Detail Information',
  'keyUsage.tokenStats': 'Token Statistics',
  'keyUsage.groupRate': 'Group Rate',
  'keyUsage.dailyDetail': 'Daily Detail',
  'keyUsage.recentRequests': 'Recent Requests',
  'keyUsage.date': 'Date',
  'keyUsage.time': 'Time',
  'keyUsage.endpoint': 'Endpoint',
  'keyUsage.requestType': 'Request Type',
  'keyUsage.rateMultiplier': 'Rate',
  'keyUsage.duration': 'Duration',
  'keyUsage.requestId': 'Request ID',
  'keyUsage.requests': 'Requests',
  'keyUsage.inputTokens': 'Input Tokens',
  'keyUsage.outputTokens': 'Output Tokens',
  'keyUsage.cacheReadTokens': 'Cache Read',
  'keyUsage.cacheWriteTokens': 'Cache Write',
  'keyUsage.cost': 'Cost',
  'keyUsage.quotaMode': 'Key Quota Mode',
  'keyUsage.walletBalance': 'Wallet Balance',
  'keyUsage.totalQuota': 'Total Quota',
  'keyUsage.limit5h': '5-Hour Limit',
  'keyUsage.limitDaily': 'Daily Limit',
  'keyUsage.limit7d': '7-Day Limit',
  'keyUsage.limitWeekly': 'Weekly Limit',
  'keyUsage.limitMonthly': 'Monthly Limit',
  'keyUsage.remainingQuota': 'Remaining Quota',
  'keyUsage.usedQuota': 'Used Quota',
  'keyUsage.subscriptionType': 'Subscription Type',
  'keyUsage.todayRequests': 'Today Requests',
  'keyUsage.todayInputTokens': 'Today Input',
  'keyUsage.todayOutputTokens': 'Today Output',
  'keyUsage.todayTokens': 'Today Tokens',
  'keyUsage.todayCacheCreation': 'Today Cache Creation',
  'keyUsage.todayCacheRead': 'Today Cache Read',
  'keyUsage.todayCost': 'Today Cost',
  'keyUsage.rpmTpm': 'RPM / TPM',
  'keyUsage.totalRequests': 'Total Requests',
  'keyUsage.totalInputTokens': 'Total Input',
  'keyUsage.totalOutputTokens': 'Total Output',
  'keyUsage.totalTokensLabel': 'Total Tokens',
  'keyUsage.totalCacheCreation': 'Total Cache Creation',
  'keyUsage.totalCacheRead': 'Total Cache Read',
  'keyUsage.totalCost': 'Total Cost',
  'keyUsage.avgDuration': 'Avg Duration',
  'keyUsage.groupName': 'Group Name',
  'keyUsage.groupPlatform': 'Platform',
  'keyUsage.groupDefaultRate': 'Group Default Rate',
  'keyUsage.userRate': 'User Rate',
  'keyUsage.effectiveRate': 'Effective Rate',
  'keyUsage.rateSource': 'Rate Source',
  'keyUsage.rateSourceUser': 'User Override',
  'keyUsage.rateSourceGroup': 'Group Default',
  'keyUsage.imageRate': 'Image Rate',
  'keyUsage.imageRateMode': 'Image Rate Mode',
  'keyUsage.imageRateIndependent': 'Independent',
  'keyUsage.imageRateShared': 'Follows Group',
  'keyUsage.notConfigured': 'Not configured',
  'keyUsage.querySuccess': 'Query successful',
  'keyUsage.queryFailed': 'Query failed',
  'keyUsage.queryFailedRetry': 'Query failed, please try again later',
  'home.viewDocs': 'Docs',
  'home.switchToLight': 'Light',
  'home.switchToDark': 'Dark',
  'home.footer.allRightsReserved': 'All rights reserved.',
}

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key,
      locale: { value: 'en' },
    }),
  }
})

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    cachedPublicSettings: null,
    siteName: 'Sub2API',
    siteLogo: '',
    docUrl: '',
    publicSettingsLoaded: true,
    fetchPublicSettings,
    showInfo,
    showSuccess,
    showError,
  }),
}))

describe('KeyUsageView daily detail', () => {
  beforeEach(() => {
    showInfo.mockReset()
    showSuccess.mockReset()
    showError.mockReset()
    fetchPublicSettings.mockReset()
    localStorage.clear()

    Object.defineProperty(window, 'matchMedia', {
      configurable: true,
      value: vi.fn().mockReturnValue({ matches: false }),
    })
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => window.setTimeout(() => cb(0), 0))
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        mode: 'quota_limited',
        isValid: true,
        status: 'active',
        quota: {
          limit: 10,
          used: 1,
          remaining: 9,
          unit: 'USD',
        },
        usage: {
          today: {
            requests: 1,
            input_tokens: 10,
            output_tokens: 20,
            cache_creation_tokens: 0,
            cache_read_tokens: 0,
            total_tokens: 30,
            actual_cost: 0.01,
          },
          total: {
            requests: 12,
            input_tokens: 100,
            output_tokens: 200,
            cache_creation_tokens: 10,
            cache_read_tokens: 30,
            total_tokens: 340,
            actual_cost: 0.12,
          },
          rpm: 0,
          tpm: 0,
        },
        daily_usage: [
          {
            date: '2026-05-19',
            requests: 12,
            input_tokens: 100,
            output_tokens: 200,
            cache_read_tokens: 30,
            cache_write_tokens: 10,
            total_tokens: 340,
            cost: 0.15,
            actual_cost: 0.12,
          },
        ],
        group: {
          id: 1,
          name: 'Default Group',
          platform: 'anthropic',
          rate_multiplier: 1.2,
          user_rate_multiplier: 0.8,
          effective_rate_multiplier: 0.8,
          rate_multiplier_source: 'user',
          image_rate_multiplier: 1,
          image_rate_independent: false,
        },
        recent_requests: [
          {
            id: 88,
            request_id: 'req_recent_1',
            model: 'claude-sonnet-4-5',
            inbound_endpoint: '/v1/messages',
            request_type: 'stream',
            input_tokens: 11,
            output_tokens: 22,
            cache_creation_tokens: 3,
            cache_read_tokens: 4,
            actual_cost: 0.02,
            rate_multiplier: 0.8,
            duration_ms: 1234,
            created_at: '2026-05-19T01:02:03Z',
          },
        ],
      }),
    }))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('renders daily usage detail rows after a successful query', async () => {
    const wrapper = mount(KeyUsageView, {
      global: {
        stubs: {
          RouterLink: { template: '<a><slot /></a>' },
          LocaleSwitcher: true,
          Icon: true,
        },
      },
    })

    await wrapper.find('input').setValue('sk-test-key')
    await wrapper.find('input').trigger('keydown.enter')
    await flushPromises()
    await nextTick()

    const fetchMock = vi.mocked(fetch)
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/v1/usage?'),
      expect.objectContaining({
        headers: { Authorization: 'Bearer sk-test-key' },
      })
    )
    expect(String(fetchMock.mock.calls[0][0])).toContain('days=30')
    expect(String(fetchMock.mock.calls[0][0])).toContain('recent_limit=10')

    const text = wrapper.text()
    expect(text).toContain('Group Rate')
    expect(text).toContain('Default Group')
    expect(text).toContain('0.80x')
    expect(text).toContain('Recent Requests')
    expect(text).toContain('claude-sonnet-4-5')
    expect(text).toContain('req_recent_1')
    expect(text).toContain('Daily Detail')
    expect(text).toContain('Date')
    expect(text).toContain('Cache Read')
    expect(text).toContain('Cache Write')
    expect(text).toContain('2026-05-19')
    expect(text).toContain('12')
    expect(text).toContain('100')
    expect(text).toContain('200')
    expect(text).toContain('30')
    expect(text).toContain('10')
    expect(text).toContain('$0.12')

    wrapper.unmount()
  })
})
