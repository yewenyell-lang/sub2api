package service

import (
	"strings"
	"testing"
	"time"
)

// TestResolveCodexAccountAvailability 用线上真实账号的状态组合验证可用性判定。
//
// 判定口径必须与调度 SQL（repository/group_repo.go groupAccountAvailableSQL）一致：
// 硬性条件（status / schedulable / 过期）先判，再看三个时间窗；三个窗口要全部
// 过期才算可用，所以绑定约束是最晚结束的那个窗口。
func TestResolveCodexAccountAvailability(t *testing.T) {
	now := time.Date(2026, 9, 20, 22, 50, 0, 0, time.Local)
	at := func(d time.Duration) *time.Time {
		v := now.Add(d)
		return &v
	}
	expired := now.Add(-time.Hour)

	cases := []struct {
		name      string
		account   Account
		wantKind  string
		wantRecov time.Duration // 0 表示期望 recoverAt 为 nil
	}{
		{
			// 线上 #456 hmpro：active + schedulable，无任何限制
			name:     "可用账号",
			account:  Account{Status: StatusActive, Schedulable: true},
			wantKind: CodexAccountAvailabilityAvailable,
		},
		{
			// 线上 #461：schedulable=true，但 rate_limit_reset_at 在未来
			// —— 这正是"schedulable 不代表能用"的证据
			name:      "被限流但 schedulable=true",
			account:   Account{Status: StatusActive, Schedulable: true, RateLimitResetAt: at(2 * time.Hour)},
			wantKind:  CodexAccountAvailabilityRateLimited,
			wantRecov: 2 * time.Hour,
		},
		{
			// 限流窗口已过去 → 应恢复可用
			name:     "限流窗口已过期",
			account:  Account{Status: StatusActive, Schedulable: true, RateLimitResetAt: at(-time.Minute)},
			wantKind: CodexAccountAvailabilityAvailable,
		},
		{
			// 线上 #462 / #457：status=error（凭证失效）
			name:     "凭证失效",
			account:  Account{Status: "error", Schedulable: false},
			wantKind: CodexAccountAvailabilityError,
		},
		{
			// status=error 优先级高于 schedulable
			name:     "凭证失效优先于停用",
			account:  Account{Status: "error", Schedulable: true},
			wantKind: CodexAccountAvailabilityError,
		},
		{
			name:     "被手动停用",
			account:  Account{Status: StatusActive, Schedulable: false},
			wantKind: CodexAccountAvailabilityDisabled,
		},
		{
			name:     "已过期且到点自动暂停",
			account:  Account{Status: StatusActive, Schedulable: true, ExpiresAt: &expired, AutoPauseOnExpired: true},
			wantKind: CodexAccountAvailabilityExpired,
		},
		{
			// expires_at 过期但 auto_pause_on_expired=false → 调度 SQL 仍视其可用
			name:     "已过期但不自动暂停",
			account:  Account{Status: StatusActive, Schedulable: true, ExpiresAt: &expired, AutoPauseOnExpired: false},
			wantKind: CodexAccountAvailabilityAvailable,
		},
		{
			name:      "过载中",
			account:   Account{Status: StatusActive, Schedulable: true, OverloadUntil: at(15 * time.Minute)},
			wantKind:  CodexAccountAvailabilityOverload,
			wantRecov: 15 * time.Minute,
		},
		{
			name:      "临时停用",
			account:   Account{Status: StatusActive, Schedulable: true, TempUnschedulableUntil: at(50 * time.Minute)},
			wantKind:  CodexAccountAvailabilityTempUnschedulable,
			wantRecov: 50 * time.Minute,
		},
		{
			// 关键用例：多个窗口叠加时，绑定条件是【最晚】那个，
			// 不能让 UI 显示"50 分钟后恢复"其实还要等 2 小时限流。
			name: "多窗口叠加取最晚",
			account: Account{
				Status:                 StatusActive,
				Schedulable:            true,
				TempUnschedulableUntil: at(50 * time.Minute),
				RateLimitResetAt:       at(2 * time.Hour),
			},
			wantKind:  CodexAccountAvailabilityRateLimited,
			wantRecov: 2 * time.Hour,
		},
		{
			// 顺序相反时标签应跟着最晚窗口走
			name: "多窗口叠加取最晚（过载更晚）",
			account: Account{
				Status:           StatusActive,
				Schedulable:      true,
				RateLimitResetAt: at(10 * time.Minute),
				OverloadUntil:    at(4 * time.Hour),
			},
			wantKind:  CodexAccountAvailabilityOverload,
			wantRecov: 4 * time.Hour,
		},
		{
			name:     "nil 账号",
			account:  Account{},
			wantKind: CodexAccountAvailabilityError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var ptr *Account
			if tc.name != "nil 账号" {
				a := tc.account
				ptr = &a
			}
			kind, recoverAt := ResolveCodexAccountAvailability(ptr, now)
			if kind != tc.wantKind {
				t.Fatalf("可用性类型 = %q，期望 %q", kind, tc.wantKind)
			}
			if tc.wantRecov == 0 {
				if recoverAt != nil {
					t.Fatalf("恢复时间应为 nil，实际 %v", recoverAt)
				}
				return
			}
			if recoverAt == nil {
				t.Fatalf("恢复时间不应为 nil，期望 %v", now.Add(tc.wantRecov))
			}
			if !recoverAt.Equal(now.Add(tc.wantRecov)) {
				t.Fatalf("恢复时间 = %v，期望 %v", recoverAt, now.Add(tc.wantRecov))
			}
		})
	}
}

// TestCodexHarvestTunableInt 验证批量读取结果的解析与范围校验。
func TestCodexHarvestTunableInt(t *testing.T) {
	raw := map[string]string{
		"ok":       "180",
		"below":    "5",
		"above":    "99999",
		"garbage":  "abc",
		"empty":    "   ",
		"padded":   " 42 ",
		"negative": "-7",
	}
	cases := []struct {
		key      string
		fallback int
		want     int
	}{
		{"ok", 999, 180},
		{"padded", 999, 42},
		{"below", 999, 999},
		{"above", 999, 999},
		{"garbage", 999, 999},
		{"empty", 999, 999},
		{"negative", 999, 999},
		{"missing", 999, 999},
	}
	for _, tc := range cases {
		if got := codexHarvestTunableInt(raw, tc.key, tc.fallback, 10, 1800); got != tc.want {
			t.Errorf("codexHarvestTunableInt(%q) = %d，期望 %d", tc.key, got, tc.want)
		}
	}
	if got := codexHarvestTunableInt(nil, "ok", 777, 10, 1800); got != 777 {
		t.Errorf("nil map 应回退 fallback，实际 %d", got)
	}
}

// TestDescribeCodexProbeFailure 验证内部错误被翻译成通俗文案，且技术细节不丢。
func TestDescribeCodexProbeFailure(t *testing.T) {
	cases := []struct {
		raw        string
		status     int
		wantLevel  string
		wantSubstr string
	}{
		{"invalid probe event", 200, "WARN", "不是有效数据"},
		{"probe response failed", 200, "WARN", "上游明确返回失败"},
		{"unterminated probe event", 200, "WARN", "被中途截断"},
		{`Post "https://chatgpt.com/...": context deadline exceeded`, 0, "WARN", "响应超时"},
		{"unexpected EOF", 0, "WARN", "连接被中断"},
		{"dial tcp: lookup x: no such host", 0, "WARN", "连不上"},
		{"", 401, "ERROR", "凭证已失效"},
		{"", 403, "WARN", "上游拒绝"},
		{"完全没见过的错误", 200, "WARN", "没有拿到合规门票"},
	}
	for _, tc := range cases {
		message, level, detail := describeCodexProbeFailure(tc.raw, tc.status, "gpt-6-astra", "node-abc")
		if level != tc.wantLevel {
			t.Errorf("%q/%d 级别 = %q，期望 %q", tc.raw, tc.status, level, tc.wantLevel)
		}
		if !strings.Contains(message, tc.wantSubstr) {
			t.Errorf("%q/%d 文案 = %q，期望包含 %q", tc.raw, tc.status, message, tc.wantSubstr)
		}
		// 技术细节必须保留（排障用），且带上模型与节点
		if tc.raw != "" && !strings.Contains(detail, tc.raw) {
			t.Errorf("%q/%d 细节 = %q，应包含原始错误", tc.raw, tc.status, detail)
		}
		if !strings.Contains(detail, "model=gpt-6-astra") || !strings.Contains(detail, "node=node-abc") {
			t.Errorf("%q/%d 细节 = %q，应包含 model 与 node", tc.raw, tc.status, detail)
		}
	}
}
