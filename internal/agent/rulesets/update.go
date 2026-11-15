package rulesets

import (
	"context"
	"errors"
	"os"
)

// Update owns one apply's downloads. Preparation leaves live files untouched.
// Activation and rollback run in the control loop with the core switch.
type Update struct{ files []replacement }
type replacement struct {
	target, temporary   string
	previous, installed bool
}

func (u *Update) Append(other *Update) {
	if other != nil {
		u.files = append(u.files, other.files...)
		other.files = nil
	}
}
func (u *Update) Changed() bool { return u != nil && len(u.files) != 0 }
func (u *Update) Activate() error {
	if u == nil {
		return nil
	}
	for i := range u.files {
		f := &u.files[i]
		if _, err := os.Stat(f.target); err == nil {
			if err := os.Rename(f.target, f.target+".prev"); err != nil {
				return err
			}
			f.previous = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.Rename(f.temporary, f.target); err != nil {
			return err
		}
		f.temporary, f.installed = "", true
	}
	return nil
}

// Rollback is idempotent: adapters restore before restarting old configurations,
// and the reconciler also unwinds early failures.
func (u *Update) Rollback() error {
	if u == nil {
		return nil
	}
	var result error
	for i := len(u.files) - 1; i >= 0; i-- {
		f := &u.files[i]
		if f.previous {
			if err := os.Rename(f.target+".prev", f.target); err != nil {
				result = errors.Join(result, err)
				continue
			}
			f.previous, f.installed = false, false
		} else if f.installed {
			if err := os.Remove(f.target); err != nil {
				result = errors.Join(result, err)
				continue
			}
			f.installed = false
		}
		if f.temporary != "" {
			if err := os.Remove(f.temporary); err != nil {
				result = errors.Join(result, err)
				continue
			}
			f.temporary = ""
		}
	}
	return result
}
func (u *Update) Commit() error {
	if u == nil {
		return nil
	}
	var result error
	for i := range u.files {
		f := &u.files[i]
		if f.previous {
			if err := os.Remove(f.target + ".prev"); err != nil {
				result = errors.Join(result, err)
				continue
			}
			f.previous = false
		}
		f.installed = false
	}
	return result
}

type updateKey struct{}

func (u *Update) Context(ctx context.Context) context.Context {
	return context.WithValue(ctx, updateKey{}, u)
}
func RestoreBeforeRestart(ctx context.Context) error {
	u, _ := ctx.Value(updateKey{}).(*Update)
	return u.Rollback()
}
