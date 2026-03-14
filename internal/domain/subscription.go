package domain

import "time"

// ExtendSubscription computes a new subscription expiration time based on the
// current expiration, the current time, and the payment amount in the same
// units as pricePerDay.
//
// The calculation is intentionally deterministic and ensures that existing paid
// time is never lost.
//
// baseTime = max(currentSubUntil, now)
// extensionDays = paymentAmount / pricePerDay
// newSubUntil = baseTime + (extensionDays * 24h)
func ExtendSubscription(currentSubUntil int64, now time.Time, paymentAmount int, pricePerDay int) int64 {
	if pricePerDay <= 0 || paymentAmount <= 0 {
		// No change in subscription.
		return currentSubUntil
	}

	base := now.Unix()
	if currentSubUntil > base {
		base = currentSubUntil
	}

	extensionDays := paymentAmount / pricePerDay
	if extensionDays <= 0 {
		return currentSubUntil
	}

	return time.Unix(base, 0).Add(time.Duration(extensionDays) * 24 * time.Hour).Unix()
}
