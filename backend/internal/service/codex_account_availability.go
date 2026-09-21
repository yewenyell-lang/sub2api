package service

import "time"

// 账号可用性口径 —— 与调度选号 SQL 保持一致。
// 权威来源：internal/repository/group_repo.go 的 groupAccountAvailableSQL。
//
//	schedulable 只是其中一个条件，不代表"有额度/没被限流"：
//	真正可调度 = status=active + schedulable + 未过期 +
//	             rate_limit/overload/temp_unschedulable 三个时间窗全部过期。
const (
	CodexAccountAvailabilityAvailable         = "available"
	CodexAccountAvailabilityRateLimited       = "rate_limited"
	CodexAccountAvailabilityOverload          = "overload"
	CodexAccountAvailabilityTempUnschedulable = "temp_unschedulable"
	CodexAccountAvailabilityError             = "error"
	CodexAccountAvailabilityDisabled          = "disabled"
	CodexAccountAvailabilityExpired           = "expired"
)

// ResolveCodexAccountAvailability 计算账号当前能否被调度，以及被时间窗挡住时何时恢复。
//
// 判定顺序与调度 SQL 对齐：先判硬性条件（凭证状态 / 调度开关 / 是否过期），
// 再看三个时间窗。三个窗口必须【全部】过期才算可用，因此绑定约束是最晚结束的
// 那个窗口 —— 状态标签与恢复时间都取它，避免出现"显示 5 分钟后恢复，其实还要
// 等 2 小时限流"这类误导。
func ResolveCodexAccountAvailability(account *Account, now time.Time) (string, *time.Time) {
	if account == nil {
		return CodexAccountAvailabilityError, nil
	}
	if account.Status != StatusActive {
		return CodexAccountAvailabilityError, nil
	}
	if !account.Schedulable {
		return CodexAccountAvailabilityDisabled, nil
	}
	if account.ExpiresAt != nil && !account.ExpiresAt.After(now) && account.AutoPauseOnExpired {
		return CodexAccountAvailabilityExpired, nil
	}

	kind := ""
	var recoverAt *time.Time
	consider := func(window *time.Time, candidate string) {
		if window == nil || !window.After(now) {
			return
		}
		if recoverAt == nil || window.After(*recoverAt) {
			recoverAt = window
			kind = candidate
		}
	}
	consider(account.RateLimitResetAt, CodexAccountAvailabilityRateLimited)
	consider(account.OverloadUntil, CodexAccountAvailabilityOverload)
	consider(account.TempUnschedulableUntil, CodexAccountAvailabilityTempUnschedulable)

	if recoverAt == nil {
		return CodexAccountAvailabilityAvailable, nil
	}
	return kind, recoverAt
}
