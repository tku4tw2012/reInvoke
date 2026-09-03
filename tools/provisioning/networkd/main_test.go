// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLifecycleTransitions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		current lifecycleState
		next    observation
		running bool
		failed  bool
		action  lifecycleAction
	}{
		{
			name:   "connected starts DHCP",
			next:   observation{completed: true, identity: "network-a"},
			action: actionStart,
		},
		{
			name: "stable connection keeps DHCP",
			current: lifecycleState{
				connected: true,
				identity:  "network-a",
				dhcp:      true,
			},
			next:    observation{completed: true, identity: "network-a"},
			running: true,
			action:  actionNone,
		},
		{
			name: "disconnect stops DHCP",
			current: lifecycleState{
				connected: true,
				identity:  "network-a",
				dhcp:      true,
			},
			running: true,
			action:  actionStop,
		},
		{
			name: "credential replacement restarts DHCP",
			current: lifecycleState{
				connected: true,
				identity:  "network-a",
				dhcp:      true,
			},
			next:    observation{completed: true, identity: "network-b"},
			running: true,
			action:  actionRestart,
		},
		{
			name: "exited DHCP restarts while connected",
			current: lifecycleState{
				connected: true,
				identity:  "network-a",
			},
			next:   observation{completed: true, identity: "network-a"},
			action: actionStart,
		},
		{
			name: "hook failure recovers DHCP",
			current: lifecycleState{
				connected: true,
				identity:  "network-a",
				dhcp:      true,
			},
			next:    observation{completed: true, identity: "network-a"},
			running: true,
			failed:  true,
			action:  actionRecover,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, action := transition(
				test.current,
				test.next,
				test.running,
				test.failed,
			)
			if action != test.action {
				t.Fatalf("action = %d, want %d", action, test.action)
			}
		})
	}
}

func TestHookFailureRecoveryRestartsDHCP(t *testing.T) {
	t.Parallel()
	current := lifecycleState{
		connected: true,
		identity:  "network-a",
		dhcp:      true,
	}
	observed := observation{completed: true, identity: "network-a"}
	recovering, action := transition(current, observed, true, true)
	if action != actionRecover {
		t.Fatalf("failure action = %d, want recovery", action)
	}
	_, action = transition(recovering, observed, false, false)
	if action != actionStart {
		t.Fatalf("post-cleanup action = %d, want DHCP restart", action)
	}
}

func TestRootControlSocketMetadata(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		mode os.FileMode
		uid  uint32
		gid  uint32
		want bool
	}{
		{
			name: "root group writable socket",
			mode: os.ModeSocket | 0770,
			want: true,
		},
		{
			name: "non-root group",
			mode: os.ModeSocket | 0770,
			gid:  1000,
		},
		{
			name: "non-root owner",
			mode: os.ModeSocket | 0770,
			uid:  1000,
		},
		{
			name: "world writable",
			mode: os.ModeSocket | 0772,
		},
		{
			name: "symlink",
			mode: os.ModeSocket | os.ModeSymlink | 0770,
		},
		{
			name: "regular file",
			mode: 0770,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			actual := validRootControlSocketMetadata(
				test.mode,
				test.uid,
				test.gid,
			)
			if actual != test.want {
				t.Fatalf("valid = %t, want %t", actual, test.want)
			}
		})
	}
}

func TestParseLeaseEnvironmentAndRenderResolver(t *testing.T) {
	t.Parallel()
	environment := []string{
		"interface=mlan0",
		"ip=192.0.2.8",
		"subnet=255.255.255.0",
		"router=192.0.2.1 192.0.2.2",
		"dns=192.0.2.53 198.51.100.53",
		"lease=3600",
	}
	current, err := parseLeaseEnvironment(environment, "mlan0")
	if err != nil {
		t.Fatalf("parse valid lease: %v", err)
	}
	if current.Prefix != 24 || current.Router != "192.0.2.1" {
		t.Fatalf("unexpected lease: %#v", current)
	}
	const expected = "nameserver 192.0.2.53\nnameserver 198.51.100.53\n"
	if actual := string(renderResolver(current)); actual != expected {
		t.Fatalf("resolver = %q, want %q", actual, expected)
	}
}

func TestRejectInvalidLeaseEnvironment(t *testing.T) {
	t.Parallel()
	valid := []string{
		"interface=mlan0",
		"ip=192.0.2.8",
		"subnet=255.255.255.0",
		"router=192.0.2.1",
		"dns=192.0.2.53",
		"lease=3600",
	}
	tests := []struct {
		name        string
		replacement string
	}{
		{name: "wrong interface", replacement: "interface=eth0"},
		{name: "injected address", replacement: "ip=192.0.2.8;reboot"},
		{name: "noncontiguous subnet", replacement: "subnet=255.0.255.0"},
		{name: "missing router", replacement: "router="},
		{name: "injected DNS", replacement: "dns=192.0.2.53\nsearch bad"},
		{name: "zero lease", replacement: "lease=0"},
	}
	for index, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			environment := append([]string(nil), valid...)
			environment[index] = test.replacement
			if _, err := parseLeaseEnvironment(environment, "mlan0"); err == nil {
				t.Fatal("invalid lease was accepted")
			}
		})
	}
}

type fakeProcessAccess struct {
	snapshot processSnapshot
	stale    bool
	reads    int
}

func (f *fakeProcessAccess) Snapshot(
	_ int,
	_ string,
) (processSnapshot, error) {
	f.reads++
	if f.stale {
		return processSnapshot{}, os.ErrNotExist
	}
	return f.snapshot, nil
}

func TestPriorActiveProcessRefusesTakeover(t *testing.T) {
	t.Parallel()
	record := ownerRecord{
		PID:       42,
		StartTime: 123,
		Token:     "0123456789abcdef0123456789abcdef",
	}
	access := &fakeProcessAccess{snapshot: processSnapshot{
		uid:            0,
		startTime:      123,
		arguments:      []string{busyboxPath, "udhcpc"},
		executableSame: true,
		token:          record.Token,
	}}
	if err := ensurePriorProcessGone(
		access,
		record,
		[]string{busyboxPath, "udhcpc"},
	); err == nil {
		t.Fatal("active prior process allowed takeover")
	}
	if access.reads != 1 {
		t.Fatalf("process reads = %d, want 1", access.reads)
	}
}

func TestPriorStaleProcessAllowsTakeover(t *testing.T) {
	t.Parallel()
	access := &fakeProcessAccess{stale: true}
	err := ensurePriorProcessGone(access, ownerRecord{PID: 42}, nil)
	if err != nil {
		t.Fatalf("stale PID: %v", err)
	}
}

func TestPriorReusedPIDAllowsTakeover(t *testing.T) {
	t.Parallel()
	record := ownerRecord{
		PID:          42,
		StartTime:    123,
		Token:        "0123456789abcdef0123456789abcdef",
		ResolverLink: "/etc/resolv.conf",
	}
	arguments := []string{busyboxPath, "udhcpc"}
	access := &fakeProcessAccess{snapshot: processSnapshot{
		uid:            0,
		startTime:      456,
		arguments:      arguments,
		executableSame: true,
		token:          record.Token,
		resolverLink:   record.ResolverLink,
	}}
	if err := ensurePriorProcessGone(access, record, arguments); err != nil {
		t.Fatalf("reused PID blocked takeover: %v", err)
	}
}

func TestWriteAtomicReplacesFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "resolv.conf")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatalf("write old file: %v", err)
	}
	if err := writeAtomic(path, []byte("new\n"), 0644); err != nil {
		t.Fatalf("replace file: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read replacement: %v", err)
	}
	if string(content) != "new\n" {
		t.Fatalf("content = %q", content)
	}
}

func TestEnsureResolverLinkReplacesSafeRegularFile(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatalf("restrict test directory: %v", err)
	}
	linkPath := filepath.Join(directory, "resolv.conf")
	targetPath := filepath.Join(directory, "networkd-resolv.conf")
	if err := os.WriteFile(linkPath, []byte("old\n"), 0644); err != nil {
		t.Fatalf("write existing resolver: %v", err)
	}
	if err := ensureResolverLink(
		linkPath,
		targetPath,
		uint32(os.Geteuid()),
		false,
	); err != nil {
		t.Fatalf("ensure resolver link: %v", err)
	}
	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatalf("inspect resolver link: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("resolver path is not a symlink")
	}
	actualTarget, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("read resolver link: %v", err)
	}
	if actualTarget != targetPath {
		t.Fatalf("link target = %q, want %q", actualTarget, targetPath)
	}
}

func TestEnsureResolverLinkRefusesUnsafeExistingPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		setup func(string) error
	}{
		{
			name: "writable regular file",
			setup: func(path string) error {
				if err := os.WriteFile(path, []byte("unsafe\n"), 0600); err != nil {
					return err
				}
				return os.Chmod(path, 0666)
			},
		},
		{
			name: "directory",
			setup: func(path string) error {
				return os.Mkdir(path, 0700)
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			if err := os.Chmod(directory, 0700); err != nil {
				t.Fatalf("restrict test directory: %v", err)
			}
			linkPath := filepath.Join(directory, "resolv.conf")
			if err := test.setup(linkPath); err != nil {
				t.Fatalf("create unsafe resolver path: %v", err)
			}
			err := ensureResolverLink(
				linkPath,
				filepath.Join(directory, "networkd-resolv.conf"),
				uint32(os.Geteuid()),
				false,
			)
			if err == nil {
				t.Fatal("unsafe resolver path was replaced")
			}
		})
	}
}

func TestEventFailureMarkerClearedAfterSuccess(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatalf("restrict test directory: %v", err)
	}
	path := filepath.Join(directory, "event-failure")
	uid := uint32(os.Geteuid())
	eventErr := errors.New("event processing failed")
	if err := finishEvent(path, uid, eventErr); err == nil {
		t.Fatal("event failure was not surfaced")
	}
	present, err := eventFailureMarkerPresent(path, uid)
	if err != nil {
		t.Fatalf("validate failure marker: %v", err)
	}
	if !present {
		t.Fatal("event failure marker was not created")
	}
	if err := finishEvent(path, uid, nil); err != nil {
		t.Fatalf("finish successful event: %v", err)
	}
	present, err = eventFailureMarkerPresent(path, uid)
	if err != nil {
		t.Fatalf("inspect cleared marker: %v", err)
	}
	if present {
		t.Fatal("successful event did not clear failure marker")
	}
}

func TestEventFailureMarkerRefusesUnsafeMode(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := filepath.Join(directory, "event-failure")
	if err := os.WriteFile(path, []byte(eventFailureContent), 0600); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatalf("make marker unsafe: %v", err)
	}
	if _, err := eventFailureMarkerPresent(
		path,
		uint32(os.Geteuid()),
	); err == nil {
		t.Fatal("unsafe marker mode was accepted")
	}
	if err := setEventFailureMarker(
		path,
		uint32(os.Geteuid()),
	); err == nil {
		t.Fatal("unsafe marker was replaced")
	}
}

type failingCleanupRunner struct {
	commands int
}

func (r *failingCleanupRunner) Run(
	_ context.Context,
	_ string,
	_ ...string,
) ([]byte, error) {
	r.commands++
	return nil, errors.New("command failed")
}

func TestRemoveLeaseNetworkAttemptsAllCleanup(t *testing.T) {
	t.Parallel()
	runner := &failingCleanupRunner{}
	err := removeLeaseNetwork(runner, lease{
		Interface: "mlan0",
		Address:   "192.0.2.8",
		Prefix:    24,
		Router:    "192.0.2.1",
	})
	if err == nil {
		t.Fatal("cleanup failures were suppressed")
	}
	if runner.commands != 4 {
		t.Fatalf("cleanup commands = %d, want 4", runner.commands)
	}
}

type cleanupRunner struct {
	commands          [][]string
	failRouteAdd      bool
	failRouteDelete   bool
	failAddressDelete bool
	failAddressAdd    bool
	routeOutput       []byte
	addressOutput     []byte
}

func (r *cleanupRunner) Run(
	_ context.Context,
	_ string,
	arguments ...string,
) ([]byte, error) {
	r.commands = append(r.commands, append([]string(nil), arguments...))
	command := strings.Join(arguments, " ")
	switch {
	case strings.Contains(command, "route add"):
		if r.failRouteAdd {
			return nil, errors.New("route add failed")
		}
	case strings.Contains(command, "route del"):
		if r.failRouteDelete {
			return nil, errors.New("route delete failed")
		}
	case strings.Contains(command, "addr del"):
		if r.failAddressDelete {
			return nil, errors.New("address delete failed")
		}
	case strings.Contains(command, "addr add"):
		if r.failAddressAdd {
			return nil, errors.New("address add failed")
		}
	case strings.Contains(command, "route show"):
		return r.routeOutput, nil
	case strings.Contains(command, "addr show"):
		return r.addressOutput, nil
	}
	return nil, nil
}

func testLease() lease {
	return lease{
		Interface: "mlan0",
		Address:   "192.0.2.8",
		Prefix:    24,
		Router:    "192.0.2.1",
	}
}

func TestEnsureLeaseAddressFirstAdd(t *testing.T) {
	t.Parallel()
	runner := &cleanupRunner{}
	if err := ensureLeaseAddress(runner, testLease()); err != nil {
		t.Fatalf("first address add: %v", err)
	}
	if err := ensureLeaseRoute(runner, testLease()); err != nil {
		t.Fatalf("first route add: %v", err)
	}
	if len(runner.commands) != 2 {
		t.Fatalf("network commands = %d, want 2", len(runner.commands))
	}
	command := strings.Join(runner.commands[0], " ")
	if !strings.Contains(command, "ip -4 addr add 192.0.2.8/24 dev mlan0") {
		t.Fatalf("unexpected address command: %q", command)
	}
	command = strings.Join(runner.commands[1], " ")
	if !strings.Contains(command, "ip -4 route add default via 192.0.2.1 dev mlan0") {
		t.Fatalf("unexpected route command: %q", command)
	}
}

func TestEnsureLeaseAddressAcceptsExistingRenewal(t *testing.T) {
	t.Parallel()
	runner := &cleanupRunner{
		failAddressAdd: true,
		failRouteAdd:   true,
		addressOutput: []byte(
			"3: mlan0: <UP>\n    inet 192.0.2.8/24 scope global mlan0\n",
		),
		routeOutput: []byte(
			"default via 192.0.2.1 dev mlan0\n",
		),
	}
	if err := ensureLeaseAddress(runner, testLease()); err != nil {
		t.Fatalf("existing renewal address: %v", err)
	}
	if err := ensureLeaseRoute(runner, testLease()); err != nil {
		t.Fatalf("existing renewal route: %v", err)
	}
	if len(runner.commands) != 4 {
		t.Fatalf("network commands = %d, want 4", len(runner.commands))
	}
}

func TestRemoveLeaseNetworkAcceptsOneAlreadyAbsent(t *testing.T) {
	t.Parallel()
	runner := &cleanupRunner{failRouteDelete: true}
	if err := removeLeaseNetwork(runner, testLease()); err != nil {
		t.Fatalf("partial idempotent cleanup: %v", err)
	}
	if len(runner.commands) != 3 {
		t.Fatalf("cleanup commands = %d, want 3", len(runner.commands))
	}
}

func TestRemoveLeaseNetworkIsRepeatableWhenAlreadyAbsent(t *testing.T) {
	t.Parallel()
	runner := &cleanupRunner{
		failRouteDelete:   true,
		failAddressDelete: true,
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := removeLeaseNetwork(runner, testLease()); err != nil {
			t.Fatalf("cleanup attempt %d: %v", attempt+1, err)
		}
	}
	if len(runner.commands) != 8 {
		t.Fatalf("cleanup commands = %d, want 8", len(runner.commands))
	}
}

func TestClearUntrackedInterfaceNetworkState(t *testing.T) {
	t.Parallel()
	runner := &cleanupRunner{
		routeOutput: []byte(
			"default via 198.51.100.1 dev eth0\n" +
				"default via 192.0.2.1 dev mlan0\n",
		),
		addressOutput: []byte(
			"3: mlan0: <UP>\n" +
				"    inet 192.0.2.8/24 scope global mlan0\n",
		),
	}
	if err := clearInterfaceNetworkState(runner, "mlan0"); err != nil {
		t.Fatalf("clear untracked interface state: %v", err)
	}
	if len(runner.commands) != 4 {
		t.Fatalf("commands = %d, want 4", len(runner.commands))
	}
	joined := make([]string, 0, len(runner.commands))
	for _, command := range runner.commands {
		joined = append(joined, strings.Join(command, " "))
	}
	all := strings.Join(joined, "\n")
	if !strings.Contains(
		all,
		"route del default via 192.0.2.1 dev mlan0",
	) {
		t.Fatalf("owned default route was not removed:\n%s", all)
	}
	if !strings.Contains(all, "addr del 192.0.2.8/24 dev mlan0") {
		t.Fatalf("owned IPv4 address was not removed:\n%s", all)
	}
	if strings.Contains(all, "route del default via 198.51.100.1") {
		t.Fatalf("unrelated interface route was removed:\n%s", all)
	}
}

func TestSupervisorLockExcludesSecondHolder(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatalf("restrict test directory: %v", err)
	}
	path := filepath.Join(directory, "supervisor.lock")
	first, err := acquireSupervisorLock(path, uint32(os.Geteuid()))
	if err != nil {
		t.Fatalf("acquire first lock: %v", err)
	}
	if second, err := acquireSupervisorLock(
		path,
		uint32(os.Geteuid()),
	); err == nil {
		_ = releaseSupervisorLock(second)
		t.Fatal("second supervisor lock was acquired")
	}
	if err := releaseSupervisorLock(first); err != nil {
		t.Fatalf("release first lock: %v", err)
	}
	third, err := acquireSupervisorLock(path, uint32(os.Geteuid()))
	if err != nil {
		t.Fatalf("reacquire released lock: %v", err)
	}
	if err := releaseSupervisorLock(third); err != nil {
		t.Fatalf("release reacquired lock: %v", err)
	}
}

func TestValidateOwnedSnapshotRejectsTokenMismatch(t *testing.T) {
	t.Parallel()
	err := validateOwnedSnapshot(
		ownerRecord{StartTime: 1, Token: "owner"},
		processSnapshot{
			uid:            0,
			startTime:      1,
			arguments:      []string{"a"},
			executableSame: true,
			token:          "other",
		},
		[]string{"a"},
	)
	if err == nil {
		t.Fatal("token mismatch was accepted")
	}
}

func TestValidateEventOwnerRejectsMissingRecord(t *testing.T) {
	t.Parallel()
	err := validateEventOwner(
		filepath.Join(t.TempDir(), "missing-owner"),
		"0123456789abcdef0123456789abcdef",
		"mlan0",
	)
	if !errors.Is(err, errEventOwnership) {
		t.Fatalf("missing owner error = %v", err)
	}
}

func TestValidateEventOwnerRecord(t *testing.T) {
	t.Parallel()
	record := ownerRecord{
		Token:     "0123456789abcdef0123456789abcdef",
		Interface: "mlan0",
	}
	if err := validateEventOwnerRecord(
		record,
		record.Token,
		record.Interface,
	); err != nil {
		t.Fatalf("validate event owner: %v", err)
	}
	if err := validateEventOwnerRecord(
		record,
		record.Token,
		"other0",
	); !errors.Is(err, errEventOwnership) {
		t.Fatalf("wrong interface error = %v", err)
	}
	if err := validateEventOwnerRecord(
		record,
		"00000000000000000000000000000000",
		record.Interface,
	); !errors.Is(err, errEventOwnership) {
		t.Fatalf("wrong token error = %v", err)
	}
}
