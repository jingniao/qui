package hostupdatestate

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func privateDir(t *testing.T) string {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Fatal("requires Linux root; compile as ordinary user and execute with sudo")
	}
	d := t.TempDir()
	if err := os.Chmod(d, 0700); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestReadReleasedJournalV1(t *testing.T) {
	t.Run("backup and permissions", func(t *testing.T) {
		d := privateDir(t)
		os.Mkdir(filepath.Join(d, "nested"), 0700)
		backup := filepath.Join(d, "nested", "backup")
		os.WriteFile(backup, []byte("db"), 0600)
		j := Journal{Component: "panel", Phase: "prepared", Backup: backup}
		if err := WriteJournal(d, j); err != nil {
			t.Fatal(err)
		}
		f := filepath.Join(d, "journal")
		if _, err := ReadJournal(f, d); err != nil {
			t.Fatal("nested historical backup rejected", err)
		}
		os.Chmod(f, 0644)
		if _, err := ReadJournal(f, d); err == nil {
			t.Fatal("accepted public journal")
		}
		os.Chmod(f, 0600)
		os.Remove(backup)
		os.Symlink(filepath.Join(d, "journal"), backup)
		if _, err := ReadJournal(f, d); err == nil {
			t.Fatal("accepted symlink backup")
		}
		os.Chmod(d, 0755)
		if err := WriteJournal(d, j); err == nil {
			t.Fatal("accepted public directory")
		}
	})
	for _, version := range []string{"1.7.5", "1.10.3", "1.10.4"} {
		for _, component := range []string{"panel", "agent"} {
			for _, phase := range []string{"prepared", "started", "committed", "restored"} {
				t.Run(version+component+phase, func(t *testing.T) {
					b, err := os.ReadFile("testdata/" + version + "-" + component + "-" + phase)
					if err != nil {
						t.Fatal(err)
					}
					d := privateDir(t)
					file := filepath.Join(d, "journal")
					if err = os.WriteFile(file, b, 0600); err != nil {
						t.Fatal(err)
					}
					j, err := ReadJournal(file, d)
					if err != nil {
						t.Fatal(err)
					}
					if j.Component != component || j.Phase != phase || j.Previous != "1.7.5" || j.Target != "1.10.4" {
						t.Fatalf("%+v", j)
					}
				})
			}
		}
	}
	d := privateDir(t)
	f := filepath.Join(d, "journal")
	base := "proxycontrol-update-journal-v1\ncomponent=agent\nwas_active=false\nphase=started"
	for _, tc := range []struct {
		suffix string
		bad    bool
	}{
		{"", false}, {"\ncomponent=panel\nunknown=ignored\nprevious=中文 = 空格", false},
		{"\r", true}, {"\nphase=invalid", false},
	} {
		if err := os.WriteFile(f, []byte(base+tc.suffix), 0600); err != nil {
			t.Fatal(err)
		}
		j, err := ReadJournal(f, d)
		if (err != nil) != tc.bad {
			t.Fatalf("%q: %v", tc.suffix, err)
		}
		if err == nil && (j.Component != "agent" || j.Target != "未知") {
			t.Fatalf("%+v", j)
		}
	}
	for _, bad := range []string{strings.Replace(base, "component=agent", "component=bad", 1), strings.Replace(base, "was_active=false", "was_active=", 1), strings.Replace(base, "phase=started", "phase=bad", 1), strings.Replace(base, "component=agent\n", "", 1), base + "\nbackup=/etc/passwd"} {
		os.WriteFile(f, []byte(bad), 0600)
		if _, err := ReadJournal(f, d); err == nil {
			t.Fatal("accepted invalid journal", bad)
		}
	}
	os.Remove(f)
	os.Symlink("/etc/passwd", f)
	if _, err := ReadJournal(f, d); err == nil {
		t.Fatal("accepted symlink")
	}
}

func TestJournalWriteDurability(t *testing.T) {
	t.Run("released byte layout", func(t *testing.T) {
		for _, component := range []string{"panel", "agent"} {
			for _, phase := range []string{"prepared", "started", "committed", "restored"} {
				d := privateDir(t)
				j := Journal{Component: component, Phase: phase, Previous: "1.7.5", Target: "1.10.4", WasActive: true}
				if err := WriteJournal(d, j); err != nil {
					t.Fatal(err)
				}
				got, err := os.ReadFile(filepath.Join(d, "journal"))
				if err != nil {
					t.Fatal(err)
				}
				want, err := os.ReadFile("testdata/1.10.4-" + component + "-" + phase)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, want) {
					t.Fatalf("%s %s\n%s\n%s", component, phase, got, want)
				}
			}
		}
	})
	t.Run("pinned directory", func(t *testing.T) {
		parent := privateDir(t)
		snapshot := filepath.Join(parent, "rollback.pinned")
		os.Mkdir(snapshot, 0700)
		j := Journal{Component: "panel", Phase: "prepared"}
		err := writeJournal(snapshot, j, fileOperations{pinned: func() error {
			if err := os.Rename(snapshot, snapshot+".old"); err != nil {
				return err
			}
			return os.Mkdir(snapshot, 0700)
		}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = os.Stat(snapshot + ".old/journal"); err != nil {
			t.Fatal(err)
		}
		if _, err = os.Stat(snapshot + "/journal"); !os.IsNotExist(err) {
			t.Fatal("writer reopened replaced path", err)
		}
	})
	t.Run("rename and stale temporary", func(t *testing.T) {
		d := privateDir(t)
		j := Journal{Component: "panel", Phase: "prepared"}
		if err := WriteJournal(d, j); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(d, ".journal.tmp"), []byte("killed"), 0600)
		boom := errors.New("rename failure")
		j.Phase = "started"
		err := writeJournal(d, j, fileOperations{rename: func(int, string, int, string) error { return boom }})
		if !errors.Is(err, boom) {
			t.Fatal(err)
		}
		got, err := ReadJournal(filepath.Join(d, "journal"), d)
		if err != nil || got.Phase != "prepared" {
			t.Fatal(got, err)
		}
		if _, err = os.Lstat(filepath.Join(d, ".journal.tmp")); !os.IsNotExist(err) {
			t.Fatal(err)
		}
	})
	d := privateDir(t)
	j := Journal{Component: "agent", Previous: "中文 = 版本", Target: "next", Phase: "prepared"}
	if err := WriteJournal(d, j); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(d, "journal"))
	if !strings.HasSuffix(string(b), "phase=prepared\ncore_request=\ncore_helper=\n") {
		t.Fatal(string(b))
	}
	info, _ := os.Stat(filepath.Join(d, "journal"))
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	j.Component = "panel"
	j.Phase = "committed"
	if err := WriteJournal(d, j); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(filepath.Join(d, "journal"))
	if strings.Contains(string(b), "core_") {
		t.Fatal(string(b))
	}
	boom := errors.New("sync failure")
	for _, at := range []int{1, 2} {
		calls := 0
		j.Phase = "started"
		err := writeJournal(d, j, fileOperations{sync: func(f *os.File) error {
			calls++
			if calls == at {
				return boom
			}
			return f.Sync()
		}})
		if !errors.Is(err, boom) {
			t.Fatal(err)
		}
		got, err := ReadJournal(filepath.Join(d, "journal"), d)
		if err != nil {
			t.Fatal(err)
		}
		want := "committed"
		if at == 2 {
			want = "started"
		}
		if got.Phase != want {
			t.Fatal(got)
		}
		if _, err = os.Lstat(filepath.Join(d, ".journal.tmp")); !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	j.Previous = "bad\nvalue"
	if err := WriteJournal(d, j); err == nil {
		t.Fatal("accepted multiline")
	}
}
