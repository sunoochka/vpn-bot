package domain

// PricePerDay is the cost (in the same currency units as payment amounts)
// for one day of VPN access.
const PricePerDay = 5

// DefaultTrialBalance is the starting balance for newly registered users.
const DefaultTrialBalance = 15

// DefaultDevices is the number of devices granted by default for new users.
const DefaultDevices = 1

// UserStatus defines a string-based status value stored in the database.
const (
	StatusActive   = "active"
	StatusInactive = "inactive"
)
