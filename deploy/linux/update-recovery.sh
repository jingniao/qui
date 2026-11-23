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
umask 077

# Test-only prefix for the one absolute path this script touches, so
# deploy/linux/tests/update-test.sh can exercise it without touching a real
# installation. It is empty in production and only root can set the environment of
# a root shell, so it is trusted the same way $PATH is.
registry=${QUI_UPDATE_TEST_ROOT:-}/var/lib/proxycontrol-install

# One unusable snapshot must not keep the others from being recovered, so each
# failure is recorded and the loop carries on; the exit status is the verdict.
failed=0
# First-enable transactions can outlive a SIGKILL independently of an updater
# snapshot. Their pinned helper is validated by the shared library before use.
for transaction in "$registry"/web-handover.*; do
  [ -e "$transaction" ] || [ -L "$transaction" ] || continue
  library=${QUI_UPDATE_TEST_ROOT:-}/usr/local/lib/proxycontrol/service.sh
  if [ ! -f "$library" ] || [ -L "$library" ] || [ "$(stat -c '%u' -- "$library")" != 0 ]; then
    printf '网页初始化恢复缺少可信 service.sh：%s\n' "$library" >&2
    failed=1
  elif ! /bin/bash -c '. "$1"; init_system=$(detect_init) || exit 1; qui_core_handover_recover_web "$2" boot' recovery "$library" "$registry"; then
    printf '网页初始化恢复停核未确认，保留现场。\n' >&2
    failed=1
  fi
  break
done
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
  # This receipt belongs to the original snapshot, not to a reconstructed
  # installation. The root web bridge can reconcile an interrupted reporting
  # process from an actual boot recovery outcome without replaying an upgrade.
  receipts=$registry/recovery-receipts
  if [ -L "$receipts" ]; then printf 'Unsafe recovery receipt directory: %s\n' "$receipts" >&2; failed=1; continue; fi
  mkdir -p -- "$receipts" || { failed=1; continue; }
  chmod 0700 -- "$receipts" || { failed=1; continue; }
  receipt=$receipts/${directory##*/}
  recovery_status=0
  if /bin/bash "$directory/updater" --recover "$directory" --boot > "$receipt.log.partial" 2>&1; then :; else recovery_status=$?; fi
  cat -- "$receipt.log.partial"
  mv -f -- "$receipt.log.partial" "$receipt.log" || { failed=1; continue; }
  printf '%s\n' "$recovery_status" > "$receipt.status.partial"
  sync -f -- "$receipt.log" || { failed=1; continue; }
  mv -f -- "$receipt.status.partial" "$receipt.status" || { failed=1; continue; }
  sync -f -- "$receipts" || { failed=1; continue; }
  if [ "$recovery_status" -ne 0 ]; then
    printf '恢复被中断的更新失败，快照保留在 %s\n' "$directory" >&2
    failed=1
  fi
done
exit "$failed"
