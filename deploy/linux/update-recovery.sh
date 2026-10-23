#!/bin/sh
# Finish or roll back a qui-update run that did not get to restore the previous
# version: kill -9, a hung-up SSH session that took the shell with it, or a power
# failure between the stop and the health check of an update.
#
# qui-update copies the files install.sh may replace into
# /var/lib/proxycontrol-install/rollback.XXXXXXXX, writes a progress journal into
# it last and removes the directory once the update or the rollback has succeeded.
# A journal found at boot is therefore the snapshot of an update that never
# finished: the installed program and service definitions may be a mix of the old
# and the new version, or a new panel may already have migrated the database.
# install.sh installs this file as /usr/local/lib/proxycontrol/update-recovery and
# runs it as a oneshot unit (proxycontrol-update-recovery.service) or as the
# proxycontrol-update-recovery init script, both ordered before proxy-agent,
# proxy-panel and the enforced-source units, so the services start on one
# consistent version instead of on the half-updated one.
#
# The recovery itself is the --recover entry point of the qui-update copy the
# snapshot carries, not a second implementation here: that copy is the exact
# version that wrote the journal, so it knows the phases, which files belong to
# the component and whether the panel database has to go back as well. This script
# only finds the snapshots, runs each recovery and reports what it could not do.
# --boot keeps the recovery from stopping or starting anything: at boot the init
# system starts the services afterwards, and a stop job here would cancel the
# start it has already queued.
#
# A snapshot whose updater is gone is reported and left in place instead of being
# skipped: someone has to look at it, and the exit status is what makes that
# visible in the unit's result.
#
# Plain POSIX shell, because this runs before anything else on an Alpine node
# (busybox ash) as well as on Debian and Ubuntu (dash).
set -u

# Test-only prefix for the one absolute path this script touches, so
# deploy/linux/tests/update-test.sh can exercise it without touching a real
# installation. It is empty in production and only root can set the environment of
# a root shell, so it is trusted the same way $PATH is.
registry=${QUI_UPDATE_TEST_ROOT:-}/var/lib/proxycontrol-install

# One unusable snapshot must not keep the others from being recovered, so each
# failure is recorded and the loop carries on; the exit status is the verdict.
failed=0
for directory in "$registry"/rollback.*; do
  # Without a match the glob stays literal, and that pattern is not a directory.
  # Anything that is not a plain directory with a plain journal is left alone as
  # well: only a real journal next to the files marks an update this script may
  # undo.
  [ -d "$directory" ] && [ ! -L "$directory" ] || continue
  [ -f "$directory/journal" ] && [ ! -L "$directory/journal" ] || continue
  if [ ! -f "$directory/updater" ] || [ -L "$directory/updater" ]; then
    printf '被中断的更新缺少恢复程序：%s，请手工检查\n' "$directory" >&2
    failed=1
    continue
  fi
  printf '处理被中断的更新：%s\n' "$directory"
  if ! /bin/bash "$directory/updater" --recover "$directory" --boot; then
    printf '恢复被中断的更新失败，快照保留在 %s\n' "$directory" >&2
    failed=1
  fi
done
exit "$failed"
