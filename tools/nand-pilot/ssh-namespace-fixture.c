// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT
// Rootless user namespaces deny setgroups even for their mapped root. Permit
// only an emulated reset to the already mapped primary group (0); never change
// account lookup, authentication, files, execve, or the actual host groups.
#define _GNU_SOURCE
#include <errno.h>
#include <linux/audit.h>
#include <signal.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/prctl.h>
#include <sys/ptrace.h>
#include <linux/ptrace.h>
#include <sys/syscall.h>
#include <sys/types.h>
#include <sys/user.h>
#include <sys/wait.h>
#include <unistd.h>

struct tracee {
  pid_t pid;
  int reset_groups;
};

int main(int argc, char **argv) {
  struct tracee children[128] = {0};
  if (argc < 2 || getuid() != 0 || getgid() != 0) return 125;
  pid_t child = fork();
  if (child < 0) return 125;
  if (!child) {
    if (prctl(PR_SET_PDEATHSIG, SIGKILL) ||
        ptrace(PTRACE_TRACEME, 0, 0, 0)) _exit(125);
    raise(SIGSTOP);
    execv(argv[1], argv + 1);
    _exit(125);
  }
  int status;
  if (waitpid(child, &status, 0) != child || !WIFSTOPPED(status)) return 125;
  long options = PTRACE_O_TRACESYSGOOD | PTRACE_O_TRACEFORK |
    PTRACE_O_TRACEVFORK | PTRACE_O_TRACECLONE | PTRACE_O_TRACEEXEC |
    PTRACE_O_EXITKILL;
  if (ptrace(PTRACE_SETOPTIONS, child, 0, options) ||
      ptrace(PTRACE_SYSCALL, child, 0, 0)) return 125;
  for (;;) {
    pid_t pid = waitpid(-1, &status, __WALL);
    if (pid < 0) {
      if (errno == EINTR) continue;
      return 125;
    }
    size_t slot = 0;
    while (slot < 128 && children[slot].pid != pid) slot++;
    if (slot == 128) {
      slot = 0;
      while (slot < 128 && children[slot].pid) slot++;
    }
    if (slot == 128) return 125;
    children[slot].pid = pid;
    if (WIFEXITED(status) || WIFSIGNALED(status)) {
      children[slot] = (struct tracee){0};
      if (pid == child)
        return WIFEXITED(status) ? WEXITSTATUS(status) : 128 + WTERMSIG(status);
      continue;
    }
    if (!WIFSTOPPED(status)) return 125;
    int sig = WSTOPSIG(status);
    if (sig == (SIGTRAP | 0x80)) {
      struct ptrace_syscall_info info = {0};
      if (ptrace(PTRACE_GET_SYSCALL_INFO, pid, sizeof(info), &info) < 0) return 125;
      if (info.op == PTRACE_SYSCALL_INFO_ENTRY) {
        children[slot].reset_groups = 0;
        if (info.arch == AUDIT_ARCH_X86_64 && info.entry.nr == SYS_execve) {
          errno = 0;
          long filename = ptrace(PTRACE_PEEKDATA, pid, info.entry.args[0], 0);
          if (!errno && !memcmp(&filename, "/bin/sh", 8))
            fputs("fixture: execve(/bin/sh) observed\n", stderr);
        }
        if (info.arch == AUDIT_ARCH_X86_64 && info.entry.nr == SYS_setgroups &&
            info.entry.args[0] == 1) {
          errno = 0;
          long groups = ptrace(PTRACE_PEEKDATA, pid, info.entry.args[1], 0);
          if (!errno && (uint32_t)groups == 0) children[slot].reset_groups = 1;
        }
      } else if (info.op == PTRACE_SYSCALL_INFO_EXIT && children[slot].reset_groups) {
        if (info.exit.rval == -EPERM) {
          struct user_regs_struct regs;
          if (ptrace(PTRACE_GETREGS, pid, 0, &regs)) return 125;
          regs.rax = 0;
          if (ptrace(PTRACE_SETREGS, pid, 0, &regs)) return 125;
          fputs("fixture: namespace-only setgroups(1, [0]) permitted\n", stderr);
        }
        children[slot].reset_groups = 0;
      }
      sig = 0;
    } else if (sig == SIGSTOP || (sig == SIGTRAP && (status >> 16))) {
      sig = 0;
    }
    if (ptrace(PTRACE_SYSCALL, pid, 0, sig)) {
      if (errno != ESRCH) return 125;
    }
  }
}
