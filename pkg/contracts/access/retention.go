package access

import "errors"

type Retention struct {
	AllowedDays int `json:"allowedDays"`
	BlockedDays int `json:"blockedDays"`
	HourlyDays  int `json:"hourlyDays"`
}

var DefaultRetention = Retention{AllowedDays: 30, BlockedDays: 90, HourlyDays: 90}

func (r Retention) Validate() error {
	if r.AllowedDays < 1 || r.AllowedDays > 3650 || r.BlockedDays < 1 || r.BlockedDays > 3650 || r.HourlyDays < 1 || r.HourlyDays > 3650 {
		return errors.New("retention days must be between 1 and 3650")
	}
	return nil
}
