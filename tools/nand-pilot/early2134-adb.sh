#!/bin/sh
# Copyright (c) 2026 tku4tw2012
# SPDX-License-Identifier: MIT

umask 022
run_is_tmpfs=
pts_is_devpts=
while read -r source target type options rest; do
    [ "$target:$type" != /run:tmpfs ] || run_is_tmpfs=yes
    [ "$target:$type" != /dev/pts:devpts ] || pts_is_devpts=yes
done < /proc/mounts
# Never fall back to persistent storage if the vendor's /run mount failed.
[ "$run_is_tmpfs" = yes ] || exit 1

log=/run/early2134-adb.log
case "$1" in
    prepare)
        printf '%s\n' 'prepare-entered (not transport proof)' > "$log"
        [ -e /dev/.coldboot_done ] &&
            printf '%s\n' 'coldboot-done-present' >> "$log"
        [ -e /dev/__properties__ ] &&
            printf '%s\n' 'property-area-present' >> "$log"
        if [ ! -e /dev/ptmx ] && [ ! -L /dev/ptmx ]; then
            /bin/mknod.coreutils -m 0666 /dev/ptmx c 5 2 ||
                printf '%s\n' 'ptmx-create-failed' >> "$log"
        fi
        if [ -c /dev/ptmx ] &&
            [ "$(/bin/stat.coreutils -Lc '%t:%T:%a' /dev/ptmx)" = 5:2:666 ]; then
            printf '%s\n' 'ptmx-char-5:2-mode-0666' >> "$log"
        else
            printf '%s\n' 'ptmx-invalid (not replaced)' >> "$log"
        fi
        printf 'devpts-mounted=%s\n' "${pts_is_devpts:-no}" >> "$log"
        if [ -c /dev/android_adb ]; then
            printf '%s\n' 'android-adb-node-present' >> "$log"
        else
            printf '%s\n' 'android-adb-node-missing' >> "$log"
        fi
        if [ -d /sys/class/android_usb/android0 ]; then
            printf '%s\n' 'android-gadget-present' >> "$log"
        else
            printf '%s\n' 'android-gadget-missing; USB configuration cannot succeed' >> "$log"
        fi
        ;;
    observe)
        printf '%s\n' 'init-issued-gadget-and-adbd-start (not transport proof)' >> "$log"
        for name in idProduct iProduct functions enable state; do
            value=unreadable
            if [ -r "/sys/class/android_usb/android0/$name" ]; then
                IFS= read -r value < "/sys/class/android_usb/android0/$name"
            fi
            printf '%s=%s\n' "$name" "$value" >> "$log"
        done
        ;;
    *) exit 2 ;;
esac
