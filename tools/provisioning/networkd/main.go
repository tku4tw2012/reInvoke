// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	defaultInterface    = "mlan0"
	defaultControlDir   = "/run/reinvoke/wpa_supplicant"
	defaultResolverLink = "/etc/resolv.conf"
	runtimeDirectory    = "/run/reinvoke/networkd"
	busyboxPath         = "/bin/busybox"
	wpaCLIPath          = "/bin/wpa_cli"
	pollInterval        = time.Second
	commandTimeout      = 3 * time.Second
	childStopTimeout    = 3 * time.Second
	maxCommandFailures  = 3
	tmpfsMagic          = 0x01021994
	ramfsMagic          = 0x858458f6
	eventFailureContent = "network event failed\n"
)

var (
	interfacePattern  = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,15}$`)
	errEventOwnership = errors.New("DHCP event ownership could not be verified")
)

type paths struct {
	runtime        string
	resolver       string
	lease          string
	owner          string
	pid            string
	lock           string
	supervisorLock string
	eventFailure   string
}

func networkPaths() paths {
	return paths{
		runtime:        runtimeDirectory,
		resolver:       filepath.Join(runtimeDirectory, "resolv.conf"),
		lease:          filepath.Join(runtimeDirectory, "lease.json"),
		owner:          filepath.Join(runtimeDirectory, "udhcpc.owner"),
		pid:            filepath.Join(runtimeDirectory, "udhcpc.pid"),
		lock:           filepath.Join(runtimeDirectory, "lease.lock"),
		supervisorLock: filepath.Join(runtimeDirectory, "supervisor.lock"),
		eventFailure:   filepath.Join(runtimeDirectory, "event-failure"),
	}
}

type commandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Run(
	ctx context.Context,
	name string,
	arguments ...string,
) ([]byte, error) {
	return exec.CommandContext(ctx, name, arguments...).CombinedOutput()
}

type observation struct {
	completed bool
	identity  string
}

type lifecycleState struct {
	connected bool
	identity  string
	dhcp      bool
}

type lifecycleAction int

const (
	actionNone lifecycleAction = iota
	actionStart
	actionStop
	actionRestart
	actionRecover
)

func transition(
	current lifecycleState,
	next observation,
	childRunning bool,
	eventFailed bool,
) (lifecycleState, lifecycleAction) {
	result := current
	result.dhcp = childRunning
	if !next.completed {
		result.connected = false
		result.identity = ""
		if childRunning {
			result.dhcp = false
			return result, actionStop
		}
		return result, actionNone
	}

	changed := current.connected &&
		current.identity != "" &&
		current.identity != next.identity
	result.connected = true
	result.identity = next.identity
	if eventFailed {
		result.dhcp = childRunning
		return result, actionRecover
	}
	if changed && childRunning {
		result.dhcp = true
		return result, actionRestart
	}
	if !childRunning {
		result.dhcp = true
		return result, actionStart
	}
	return result, actionNone
}

func scheduleRecoveryRetry(now time.Time, retryDelay time.Duration) (time.Time, time.Duration) {
	return now.Add(retryDelay), retryDelay
}

type lease struct {
	Interface string   `json:"interface"`
	Address   string   `json:"address"`
	Prefix    int      `json:"prefix"`
	Router    string   `json:"router"`
	DNS       []string `json:"dns"`
	LeaseTime uint64   `json:"lease_time"`
}

func parseLeaseEnvironment(environment []string, expectedInterface string) (lease, error) {
	values := make(map[string]string)
	for _, entry := range environment {
		key, value, found := strings.Cut(entry, "=")
		if found {
			values[key] = value
		}
	}
	if values["interface"] != expectedInterface {
		return lease{}, errors.New("invalid lease interface")
	}
	address, err := validIPv4(values["ip"])
	if err != nil {
		return lease{}, errors.New("invalid lease address")
	}
	prefix, err := subnetPrefix(values["subnet"])
	if err != nil {
		return lease{}, errors.New("invalid lease subnet")
	}
	routerFields := strings.Fields(values["router"])
	if len(routerFields) == 0 || len(routerFields) > 8 {
		return lease{}, errors.New("lease has no default route")
	}
	router := ""
	for index, value := range routerFields {
		parsed, parseErr := validIPv4(value)
		if parseErr != nil {
			return lease{}, errors.New("invalid lease router")
		}
		if index == 0 {
			router = parsed
		}
	}
	dnsFields := strings.Fields(values["dns"])
	if len(dnsFields) == 0 || len(dnsFields) > 3 {
		return lease{}, errors.New("invalid lease DNS")
	}
	dns := make([]string, 0, len(dnsFields))
	for _, value := range dnsFields {
		server, parseErr := validIPv4(value)
		if parseErr != nil {
			return lease{}, errors.New("invalid lease DNS")
		}
		dns = append(dns, server)
	}
	leaseTime, err := strconv.ParseUint(values["lease"], 10, 32)
	if err != nil || leaseTime == 0 {
		return lease{}, errors.New("invalid lease lifetime")
	}
	return lease{
		Interface: expectedInterface,
		Address:   address,
		Prefix:    prefix,
		Router:    router,
		DNS:       dns,
		LeaseTime: leaseTime,
	}, nil
}

func validIPv4(value string) (string, error) {
	if value == "" || strings.TrimSpace(value) != value {
		return "", errors.New("invalid IPv4 address")
	}
	address := net.ParseIP(value)
	if address == nil || address.To4() == nil {
		return "", errors.New("invalid IPv4 address")
	}
	address = address.To4()
	if address.IsUnspecified() || address.IsMulticast() ||
		address.Equal(net.IPv4bcast) {
		return "", errors.New("unsafe IPv4 address")
	}
	return address.String(), nil
}

func subnetPrefix(value string) (int, error) {
	maskIP := net.ParseIP(value)
	if maskIP == nil || maskIP.To4() == nil {
		return 0, errors.New("invalid subnet mask")
	}
	ones, bits := net.IPMask(maskIP.To4()).Size()
	if bits != 32 || ones < 1 || ones > 32 {
		return 0, errors.New("invalid subnet mask")
	}
	return ones, nil
}

func renderResolver(current lease) []byte {
	var builder strings.Builder
	for _, server := range current.DNS {
		builder.WriteString("nameserver ")
		builder.WriteString(server)
		builder.WriteByte('\n')
	}
	return []byte(builder.String())
}

func validateStoredLease(current lease, expectedInterface string) error {
	if current.Interface != expectedInterface ||
		current.Prefix < 1 || current.Prefix > 32 ||
		current.LeaseTime == 0 ||
		len(current.DNS) == 0 || len(current.DNS) > 3 {
		return errors.New("invalid stored lease")
	}
	if address, err := validIPv4(current.Address); err != nil ||
		address != current.Address {
		return errors.New("invalid stored lease")
	}
	if router, err := validIPv4(current.Router); err != nil ||
		router != current.Router {
		return errors.New("invalid stored lease")
	}
	for _, server := range current.DNS {
		if parsed, err := validIPv4(server); err != nil || parsed != server {
			return errors.New("invalid stored lease")
		}
	}
	return nil
}

func configureLease(
	runner commandRunner,
	current lease,
	networkPaths paths,
	resolverLink string,
) error {
	if err := ensureLeaseAddress(runner, current); err != nil {
		return err
	}
	if err := ensureLeaseRoute(runner, current); err != nil {
		return err
	}
	if err := writeAtomic(networkPaths.resolver, renderResolver(current), 0644); err != nil {
		return err
	}
	content, err := json.Marshal(current)
	if err != nil {
		return errors.New("encode lease state")
	}
	if err := writeAtomic(networkPaths.lease, append(content, '\n'), 0600); err != nil {
		return err
	}
	if err := ensureResolverLink(
		resolverLink,
		networkPaths.resolver,
		0,
		true,
	); err != nil {
		return err
	}
	return nil
}

func ensureLeaseAddress(runner commandRunner, current lease) error {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	_, addErr := runner.Run(
		ctx,
		busyboxPath,
		"ip", "-4", "addr", "add",
		fmt.Sprintf("%s/%d", current.Address, current.Prefix),
		"dev", current.Interface,
	)
	cancel()
	if addErr == nil {
		return nil
	}
	ctx, cancel = context.WithTimeout(context.Background(), commandTimeout)
	output, queryErr := runner.Run(
		ctx,
		busyboxPath,
		"ip", "-4", "addr", "show", "dev", current.Interface,
	)
	cancel()
	if queryErr != nil {
		return errors.New("configure lease address and verify presence")
	}
	if !addressPresent(output, current.Address, current.Prefix) {
		return errors.New("configure lease address")
	}
	return nil
}

func ensureLeaseRoute(runner commandRunner, current lease) error {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	_, addErr := runner.Run(
		ctx,
		busyboxPath,
		"ip", "-4", "route", "add", "default",
		"via", current.Router, "dev", current.Interface,
	)
	cancel()
	if addErr == nil {
		return nil
	}
	ctx, cancel = context.WithTimeout(context.Background(), commandTimeout)
	output, queryErr := runner.Run(
		ctx,
		busyboxPath,
		"ip", "-4", "route", "show", "default",
	)
	cancel()
	if queryErr != nil {
		return errors.New("configure lease route and verify presence")
	}
	if !routePresent(output, current.Router, current.Interface) {
		return errors.New("configure lease route")
	}
	return nil
}

func removeLeaseNetwork(runner commandRunner, current lease) error {
	return combineErrors(
		removeDefaultRoute(
			runner,
			current.Interface,
			current.Router,
		),
		removeIPv4Address(
			runner,
			current.Interface,
			current.Address,
			current.Prefix,
		),
	)
}

func removeDefaultRoute(
	runner commandRunner,
	interfaceName string,
	router string,
) error {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	arguments := []string{"ip", "-4", "route", "del", "default"}
	if router != "" {
		arguments = append(arguments, "via", router)
	}
	arguments = append(arguments, "dev", interfaceName)
	_, err := runner.Run(ctx, busyboxPath, arguments...)
	cancel()
	if err == nil {
		return nil
	}
	ctx, cancel = context.WithTimeout(context.Background(), commandTimeout)
	output, queryErr := runner.Run(
		ctx,
		busyboxPath,
		"ip", "-4", "route", "show", "default",
	)
	cancel()
	if queryErr != nil {
		return errors.New("remove lease route and verify absence")
	}
	if routePresent(output, router, interfaceName) {
		return errors.New("remove lease route")
	}
	return nil
}

func removeIPv4Address(
	runner commandRunner,
	interfaceName string,
	address string,
	prefix int,
) error {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	_, err := runner.Run(
		ctx,
		busyboxPath,
		"ip", "-4", "addr", "del",
		fmt.Sprintf("%s/%d", address, prefix),
		"dev", interfaceName,
	)
	cancel()
	if err == nil {
		return nil
	}
	ctx, cancel = context.WithTimeout(context.Background(), commandTimeout)
	output, queryErr := runner.Run(
		ctx,
		busyboxPath,
		"ip", "-4", "addr", "show", "dev", interfaceName,
	)
	cancel()
	if queryErr != nil {
		return errors.New("remove lease address and verify absence")
	}
	if addressPresent(output, address, prefix) {
		return errors.New("remove lease address")
	}
	return nil
}

func routePresent(output []byte, router string, interfaceName string) bool {
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "default" {
			continue
		}
		hasRouter := router == ""
		hasInterface := false
		hasAnyRouter := false
		for index := 0; index+1 < len(fields); index++ {
			if fields[index] == "via" {
				hasAnyRouter = true
				if fields[index+1] == router {
					hasRouter = true
				}
			}
			if fields[index] == "dev" && fields[index+1] == interfaceName {
				hasInterface = true
			}
		}
		if hasRouter && hasInterface && (router != "" || !hasAnyRouter) {
			return true
		}
	}
	return false
}

func addressPresent(output []byte, address string, prefix int) bool {
	expected := fmt.Sprintf("%s/%d", address, prefix)
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		for index := 0; index+1 < len(fields); index++ {
			if fields[index] == "inet" && fields[index+1] == expected {
				return true
			}
		}
	}
	return false
}

type interfaceAddress struct {
	address string
	prefix  int
}

type interfaceRoute struct {
	router string
}

func clearInterfaceNetworkState(
	runner commandRunner,
	interfaceName string,
) error {
	var queryErr error
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	routeOutput, err := runner.Run(
		ctx,
		busyboxPath,
		"ip", "-4", "route", "show", "default",
	)
	cancel()
	var routes []interfaceRoute
	if err != nil {
		queryErr = errors.New("inspect stale default routes")
	} else {
		routes, err = parseInterfaceRoutes(routeOutput, interfaceName)
		if err != nil {
			queryErr = err
		}
	}

	ctx, cancel = context.WithTimeout(context.Background(), commandTimeout)
	addressOutput, err := runner.Run(
		ctx,
		busyboxPath,
		"ip", "-4", "addr", "show", "dev", interfaceName,
	)
	cancel()
	var addresses []interfaceAddress
	if err != nil {
		queryErr = combineErrors(
			queryErr,
			errors.New("inspect stale IPv4 addresses"),
		)
	} else {
		addresses, err = parseInterfaceAddresses(addressOutput)
		if err != nil {
			queryErr = combineErrors(queryErr, err)
		}
	}

	var cleanupErr error
	for _, route := range routes {
		cleanupErr = combineErrors(
			cleanupErr,
			removeDefaultRoute(runner, interfaceName, route.router),
		)
	}
	for _, address := range addresses {
		cleanupErr = combineErrors(
			cleanupErr,
			removeIPv4Address(
				runner,
				interfaceName,
				address.address,
				address.prefix,
			),
		)
	}
	return combineErrors(queryErr, cleanupErr)
}

func parseInterfaceRoutes(
	output []byte,
	interfaceName string,
) ([]interfaceRoute, error) {
	var routes []interfaceRoute
	seen := make(map[string]bool)
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "default" {
			continue
		}
		device := ""
		router := ""
		for index := 1; index < len(fields); index++ {
			switch fields[index] {
			case "dev", "via":
				if index+1 >= len(fields) {
					return routes, errors.New("invalid stale default route")
				}
				if fields[index] == "dev" {
					device = fields[index+1]
				} else {
					router = fields[index+1]
				}
				index++
			}
		}
		if device != interfaceName {
			continue
		}
		if router != "" {
			parsed, err := validIPv4(router)
			if err != nil || parsed != router {
				return routes, errors.New("invalid stale default route")
			}
		}
		if !seen[router] {
			routes = append(routes, interfaceRoute{router: router})
			seen[router] = true
		}
	}
	return routes, nil
}

func parseInterfaceAddresses(output []byte) ([]interfaceAddress, error) {
	var addresses []interfaceAddress
	seen := make(map[string]bool)
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		for index := 0; index+1 < len(fields); index++ {
			if fields[index] != "inet" {
				continue
			}
			address, network, err := net.ParseCIDR(fields[index+1])
			if err != nil || address.To4() == nil {
				return addresses, errors.New("invalid stale IPv4 address")
			}
			ones, bits := network.Mask.Size()
			parsed, err := validIPv4(address.String())
			if err != nil || bits != 32 || ones < 1 || ones > 32 {
				return addresses, errors.New("invalid stale IPv4 address")
			}
			key := fmt.Sprintf("%s/%d", parsed, ones)
			if !seen[key] {
				addresses = append(addresses, interfaceAddress{
					address: parsed,
					prefix:  ones,
				})
				seen[key] = true
			}
			break
		}
	}
	return addresses, nil
}

func loadStoredLease(path string, expectedInterface string) (lease, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return lease{}, err
	}
	uid, ownerErr := fileOwnerUID(info)
	if ownerErr != nil || uid != 0 || !info.Mode().IsRegular() ||
		info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0600 {
		return lease{}, errors.New("lease state is not root-controlled")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return lease{}, err
	}
	if len(content) > 4096 {
		return lease{}, errors.New("lease state is too large")
	}
	var current lease
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&current); err != nil {
		return lease{}, errors.New("invalid lease state")
	}
	var extra interface{}
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return lease{}, errors.New("invalid lease state")
	}
	if err := validateStoredLease(current, expectedInterface); err != nil {
		return lease{}, err
	}
	return current, nil
}

func clearLeaseState(
	runner commandRunner,
	networkPaths paths,
	expectedInterface string,
	resolverLink string,
) error {
	var cleanupErr, leaseFileErr error
	current, err := loadStoredLease(networkPaths.lease, expectedInterface)
	if err == nil {
		cleanupErr = removeLeaseNetwork(runner, current)
		if cleanupErr == nil {
			leaseFileErr = removeFile(networkPaths.lease)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		cleanupErr = err
	}
	return combineErrors(
		cleanupErr,
		removeManagedResolverLink(
			resolverLink,
			networkPaths.resolver,
			0,
			true,
		),
		removeFile(networkPaths.resolver),
		leaseFileErr,
	)
}

func validateResolverLinkParent(
	linkPath string,
	expectedUID uint32,
	requireRAM bool,
) error {
	if !filepath.IsAbs(linkPath) ||
		filepath.Clean(linkPath) != linkPath ||
		strings.ContainsAny(linkPath, "\x00\r\n") {
		return errors.New("resolver link path must be absolute and clean")
	}
	parent := filepath.Dir(linkPath)
	info, err := os.Lstat(parent)
	if err != nil {
		return fmt.Errorf("inspect resolver link parent: %w", err)
	}
	uid, ownerErr := fileOwnerUID(info)
	if ownerErr != nil || uid != expectedUID || !info.IsDir() ||
		info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 {
		return errors.New("resolver link parent is not root-controlled")
	}
	if requireRAM {
		var filesystem syscall.Statfs_t
		if err := syscall.Statfs(parent, &filesystem); err != nil {
			return fmt.Errorf("inspect resolver link filesystem: %w", err)
		}
		filesystemType := uint64(filesystem.Type) & 0xffffffff
		if filesystemType != tmpfsMagic && filesystemType != ramfsMagic {
			return errors.New("resolver link parent must reside on tmpfs or ramfs")
		}
	}
	return nil
}

func ensureResolverLink(
	linkPath string,
	targetPath string,
	expectedUID uint32,
	requireRAM bool,
) error {
	if !filepath.IsAbs(targetPath) ||
		filepath.Clean(targetPath) != targetPath ||
		strings.ContainsAny(targetPath, "\x00\r\n") ||
		linkPath == targetPath {
		return errors.New("resolver target path must be absolute and clean")
	}
	if err := validateResolverLinkParent(
		linkPath,
		expectedUID,
		requireRAM,
	); err != nil {
		return err
	}
	info, err := os.Lstat(linkPath)
	if err == nil {
		uid, ownerErr := fileOwnerUID(info)
		if ownerErr != nil || uid != expectedUID {
			return errors.New("existing resolver path is not root-controlled")
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			currentTarget, readErr := os.Readlink(linkPath)
			if readErr != nil {
				return fmt.Errorf("inspect resolver link: %w", readErr)
			}
			if currentTarget == targetPath {
				return nil
			}
		case info.Mode().IsRegular() && info.Mode().Perm()&0022 == 0:
		default:
			return errors.New("existing resolver path is not a safe replaceable file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect resolver path: %w", err)
	}

	parent := filepath.Dir(linkPath)
	file, err := os.CreateTemp(parent, ".networkd-resolver-link-*")
	if err != nil {
		return fmt.Errorf("reserve resolver link: %w", err)
	}
	temporaryPath := file.Name()
	if closeErr := file.Close(); closeErr != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("close resolver link reservation: %w", closeErr)
	}
	if err := os.Remove(temporaryPath); err != nil {
		return fmt.Errorf("remove resolver link reservation: %w", err)
	}
	defer os.Remove(temporaryPath)
	if err := os.Symlink(targetPath, temporaryPath); err != nil {
		return fmt.Errorf("create resolver link: %w", err)
	}
	if err := os.Rename(temporaryPath, linkPath); err != nil {
		return fmt.Errorf("activate resolver link: %w", err)
	}
	return nil
}

func removeManagedResolverLink(
	linkPath string,
	targetPath string,
	expectedUID uint32,
	requireRAM bool,
) error {
	if err := validateResolverLinkParent(
		linkPath,
		expectedUID,
		requireRAM,
	); err != nil {
		return err
	}
	info, err := os.Lstat(linkPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect resolver link: %w", err)
	}
	uid, ownerErr := fileOwnerUID(info)
	if ownerErr != nil || uid != expectedUID {
		return errors.New("resolver link is not root-controlled")
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return nil
	}
	currentTarget, err := os.Readlink(linkPath)
	if err != nil {
		return fmt.Errorf("read resolver link: %w", err)
	}
	if currentTarget != targetPath {
		return nil
	}
	if err := os.Remove(linkPath); err != nil {
		return fmt.Errorf("remove resolver link: %w", err)
	}
	return nil
}

func removeFile(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func combineErrors(values ...error) error {
	var messages []string
	for _, err := range values {
		if err != nil {
			messages = append(messages, err.Error())
		}
	}
	if len(messages) == 0 {
		return nil
	}
	return errors.New(strings.Join(messages, "; "))
}

func acquireSupervisorLock(path string, expectedUID uint32) (*os.File, error) {
	existing, err := os.Lstat(path)
	if err == nil {
		uid, ownerErr := fileOwnerUID(existing)
		if ownerErr != nil || uid != expectedUID ||
			!existing.Mode().IsRegular() ||
			existing.Mode()&os.ModeSymlink != 0 ||
			existing.Mode().Perm() != 0600 {
			return nil, errors.New("supervisor lock is not root-controlled")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect supervisor lock path: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open supervisor lock: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("inspect supervisor lock: %w", err)
	}
	uid, ownerErr := fileOwnerUID(info)
	if ownerErr != nil || uid != expectedUID || !info.Mode().IsRegular() ||
		info.Mode().Perm() != 0600 {
		file.Close()
		return nil, errors.New("supervisor lock is not root-controlled")
	}
	if err := syscall.Flock(
		int(file.Fd()),
		syscall.LOCK_EX|syscall.LOCK_NB,
	); err != nil {
		file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) ||
			errors.Is(err, syscall.EAGAIN) {
			return nil, errors.New("network lifecycle supervisor is already active")
		}
		return nil, fmt.Errorf("lock network lifecycle supervisor: %w", err)
	}
	return file, nil
}

func releaseSupervisorLock(file *os.File) error {
	if file == nil {
		return nil
	}
	unlockErr := syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	closeErr := file.Close()
	if unlockErr != nil {
		unlockErr = fmt.Errorf("unlock network lifecycle supervisor: %w", unlockErr)
	}
	if closeErr != nil {
		closeErr = fmt.Errorf("close network lifecycle supervisor lock: %w", closeErr)
	}
	return combineErrors(unlockErr, closeErr)
}

func withLeaseLock(path string, action func() error) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("open lease lock: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect lease lock: %w", err)
	}
	uid, err := fileOwnerUID(info)
	if err != nil || uid != 0 || !info.Mode().IsRegular() ||
		info.Mode().Perm() != 0600 {
		return errors.New("lease lock is not root-controlled")
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("lock lease state: %w", err)
	}
	actionErr := action()
	unlockErr := syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	if unlockErr != nil {
		unlockErr = fmt.Errorf("unlock lease state: %w", unlockErr)
	}
	return combineErrors(actionErr, unlockErr)
}

func eventFailureMarkerPresent(path string, expectedUID uint32) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect event failure marker: %w", err)
	}
	uid, ownerErr := fileOwnerUID(info)
	if ownerErr != nil || uid != expectedUID || !info.Mode().IsRegular() ||
		info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0600 {
		return false, errors.New("event failure marker is not root-controlled")
	}
	if info.Size() != int64(len(eventFailureContent)) {
		return false, errors.New("event failure marker has invalid content")
	}
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read event failure marker: %w", err)
	}
	if string(content) != eventFailureContent {
		return false, errors.New("event failure marker has invalid content")
	}
	return true, nil
}

func setEventFailureMarker(path string, expectedUID uint32) error {
	if _, err := eventFailureMarkerPresent(path, expectedUID); err != nil {
		return err
	}
	if err := writeAtomic(
		path,
		[]byte(eventFailureContent),
		0600,
	); err != nil {
		return fmt.Errorf("write event failure marker: %w", err)
	}
	return nil
}

func clearEventFailureMarker(path string, expectedUID uint32) error {
	present, err := eventFailureMarkerPresent(path, expectedUID)
	if err != nil {
		return err
	}
	if !present {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove event failure marker: %w", err)
	}
	return nil
}

func finishEvent(
	markerPath string,
	expectedUID uint32,
	eventErr error,
) error {
	if eventErr != nil {
		return combineErrors(
			eventErr,
			setEventFailureMarker(markerPath, expectedUID),
		)
	}
	return clearEventFailureMarker(markerPath, expectedUID)
}

func runEvent(
	action string,
	interfaceName string,
	resolverLink string,
	networkPaths paths,
) error {
	if os.Geteuid() != 0 {
		return errors.New("network event handler must run as root")
	}
	if err := validateRuntimeDirectory(networkPaths.runtime); err != nil {
		return err
	}
	var eventErr error
	eventOwned := true
	if err := validateRootExecutable(busyboxPath); err != nil {
		eventErr = fmt.Errorf("validate BusyBox: %w", err)
	} else if !interfacePattern.MatchString(interfaceName) {
		eventErr = errors.New("invalid interface name")
	} else if err := validateResolverLinkParent(resolverLink, 0, true); err != nil {
		eventErr = err
	} else {
		eventErr = withLeaseLock(networkPaths.lock, func() error {
			if err := validateEventOwner(
				networkPaths.owner,
				os.Getenv("NETWORKD_OWNER"),
				interfaceName,
			); err != nil {
				eventOwned = false
				return nil
			}
			switch action {
			case "deconfig":
				return clearLeaseState(
					execRunner{},
					networkPaths,
					interfaceName,
					resolverLink,
				)
			case "bound", "renew":
				current, err := parseLeaseEnvironment(os.Environ(), interfaceName)
				if err != nil {
					return err
				}
				previous, previousErr := loadStoredLease(
					networkPaths.lease,
					interfaceName,
				)
				if previousErr == nil &&
					(previous.Address != current.Address ||
						previous.Prefix != current.Prefix ||
						previous.Router != current.Router) {
					if err := removeLeaseNetwork(
						execRunner{},
						previous,
					); err != nil {
						return err
					}
				} else if previousErr != nil &&
					!errors.Is(previousErr, os.ErrNotExist) {
					return combineErrors(
						previousErr,
						clearLeaseState(
							execRunner{},
							networkPaths,
							interfaceName,
							resolverLink,
						),
					)
				}
				if err := configureLease(
					execRunner{},
					current,
					networkPaths,
					resolverLink,
				); err != nil {
					return combineErrors(
						err,
						removeLeaseNetwork(execRunner{}, current),
						removeManagedResolverLink(
							resolverLink,
							networkPaths.resolver,
							0,
							true,
						),
						removeFile(networkPaths.resolver),
						removeFile(networkPaths.lease),
					)
				}
				return nil
			default:
				return errors.New("unsupported DHCP event")
			}
		})
	}
	if !eventOwned {
		return nil
	}
	return finishEvent(networkPaths.eventFailure, 0, eventErr)
}

type ownerRecord struct {
	PID          int    `json:"pid"`
	StartTime    uint64 `json:"start_time"`
	Token        string `json:"token"`
	Interface    string `json:"interface"`
	Script       string `json:"script"`
	ResolverLink string `json:"resolver_link"`
}

type processSnapshot struct {
	uid            uint32
	startTime      uint64
	arguments      []string
	executableSame bool
	token          string
	resolverLink   string
}

func expectedDHCPArguments(
	interfaceName string,
	scriptPath string,
	networkPaths paths,
) []string {
	return []string{
		busyboxPath,
		"udhcpc",
		"-f",
		"-i", interfaceName,
		"-p", networkPaths.pid,
		"-s", scriptPath,
	}
}

func validateOwnedSnapshot(
	record ownerRecord,
	snapshot processSnapshot,
	expectedArguments []string,
) error {
	if snapshot.startTime != record.StartTime ||
		snapshot.token != record.Token ||
		validateDHCPProcessSnapshot(
			snapshot,
			expectedArguments,
			record.ResolverLink,
		) != nil {
		return errors.New("prior DHCP process ownership could not be verified")
	}
	return nil
}

func validateDHCPProcessSnapshot(
	snapshot processSnapshot,
	expectedArguments []string,
	resolverLink string,
) error {
	tokenBytes, tokenErr := hex.DecodeString(snapshot.token)
	if snapshot.uid != 0 ||
		!snapshot.executableSame ||
		tokenErr != nil ||
		len(tokenBytes) != 16 ||
		snapshot.resolverLink != resolverLink ||
		len(snapshot.arguments) != len(expectedArguments) {
		return errors.New("DHCP process identity could not be verified")
	}
	for index := range expectedArguments {
		if snapshot.arguments[index] != expectedArguments[index] {
			return errors.New("DHCP process identity could not be verified")
		}
	}
	return nil
}

type processAccess interface {
	Snapshot(int, string) (processSnapshot, error)
}

type procAccess struct{}

func (procAccess) Snapshot(pid int, executable string) (processSnapshot, error) {
	base := filepath.Join("/proc", strconv.Itoa(pid))
	status, err := os.Open(filepath.Join(base, "status"))
	if err != nil {
		return processSnapshot{}, err
	}
	defer status.Close()
	var uid uint64
	scanner := bufio.NewScanner(status)
	foundUID := false
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[0] == "Uid:" {
			uid, err = strconv.ParseUint(fields[1], 10, 32)
			foundUID = err == nil
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return processSnapshot{}, err
	}
	if !foundUID {
		return processSnapshot{}, errors.New("process UID is unavailable")
	}
	stat, err := os.ReadFile(filepath.Join(base, "stat"))
	if err != nil {
		return processSnapshot{}, err
	}
	closeParen := bytes.LastIndexByte(stat, ')')
	if closeParen < 0 {
		return processSnapshot{}, errors.New("invalid process stat")
	}
	fields := strings.Fields(string(stat[closeParen+1:]))
	if len(fields) <= 19 {
		return processSnapshot{}, errors.New("invalid process stat")
	}
	startTime, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return processSnapshot{}, errors.New("invalid process start time")
	}
	commandLine, err := os.ReadFile(filepath.Join(base, "cmdline"))
	if err != nil {
		return processSnapshot{}, err
	}
	arguments := splitNullFields(commandLine)
	processExecutable, err := os.Stat(filepath.Join(base, "exe"))
	if err != nil {
		return processSnapshot{}, err
	}
	expectedExecutable, err := os.Stat(executable)
	if err != nil {
		return processSnapshot{}, err
	}
	environment, err := os.ReadFile(filepath.Join(base, "environ"))
	if err != nil {
		return processSnapshot{}, err
	}
	token := ""
	resolverLink := ""
	for _, entry := range splitNullFields(environment) {
		if strings.HasPrefix(entry, "NETWORKD_OWNER=") {
			token = strings.TrimPrefix(entry, "NETWORKD_OWNER=")
		}
		if strings.HasPrefix(entry, "NETWORKD_RESOLV_LINK=") {
			resolverLink = strings.TrimPrefix(
				entry,
				"NETWORKD_RESOLV_LINK=",
			)
		}
	}
	return processSnapshot{
		uid:            uint32(uid),
		startTime:      startTime,
		arguments:      arguments,
		executableSame: os.SameFile(processExecutable, expectedExecutable),
		token:          token,
		resolverLink:   resolverLink,
	}, nil
}

func splitNullFields(content []byte) []string {
	content = bytes.TrimSuffix(content, []byte{0})
	if len(content) == 0 {
		return nil
	}
	parts := bytes.Split(content, []byte{0})
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		result = append(result, string(part))
	}
	return result
}

func ensurePriorProcessGone(
	access processAccess,
	record ownerRecord,
	expectedArguments []string,
) error {
	snapshot, err := access.Snapshot(record.PID, busyboxPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect prior DHCP process: %w", err)
	}
	if validateOwnedSnapshot(record, snapshot, expectedArguments) != nil {
		return nil
	}
	return errors.New("prior DHCP process is still active; refusing takeover")
}

func loadOwner(path string) (ownerRecord, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return ownerRecord{}, err
	}
	uid, ownerErr := fileOwnerUID(info)
	if ownerErr != nil || uid != 0 || !info.Mode().IsRegular() ||
		info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0600 {
		return ownerRecord{}, errors.New("DHCP owner record is not root-controlled")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return ownerRecord{}, err
	}
	if len(content) > 4096 {
		return ownerRecord{}, errors.New("DHCP owner record is too large")
	}
	var record ownerRecord
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return ownerRecord{}, errors.New("invalid DHCP owner record")
	}
	if record.PID < 2 || record.StartTime == 0 ||
		len(record.Token) != 32 ||
		!interfacePattern.MatchString(record.Interface) ||
		!filepath.IsAbs(record.Script) ||
		(record.ResolverLink != "" &&
			(!filepath.IsAbs(record.ResolverLink) ||
				filepath.Clean(record.ResolverLink) != record.ResolverLink)) {
		return ownerRecord{}, errors.New("invalid DHCP owner record")
	}
	return record, nil
}

func validateEventOwner(
	path string,
	token string,
	interfaceName string,
) error {
	record, err := loadOwner(path)
	if err != nil {
		return errEventOwnership
	}
	return validateEventOwnerRecord(record, token, interfaceName)
}

func validateEventOwnerRecord(
	record ownerRecord,
	token string,
	interfaceName string,
) error {
	if record.Token != token || record.Interface != interfaceName {
		return errEventOwnership
	}
	return nil
}

func writeOwner(path string, record ownerRecord) error {
	content, err := json.Marshal(record)
	if err != nil {
		return errors.New("encode DHCP owner record")
	}
	return writeAtomic(path, append(content, '\n'), 0600)
}

func replacePriorDHCP(
	networkPaths paths,
	interfaceName string,
	scriptPath string,
	resolverLink string,
) error {
	expectedArguments := expectedDHCPArguments(
		interfaceName,
		scriptPath,
		networkPaths,
	)
	record, err := loadOwner(networkPaths.owner)
	if errors.Is(err, os.ErrNotExist) {
		if _, pidErr := os.Lstat(networkPaths.pid); pidErr == nil {
			pid, readErr := readPIDFile(networkPaths.pid)
			if readErr != nil {
				return readErr
			}
			snapshot, processErr := (procAccess{}).Snapshot(pid, busyboxPath)
			if processErr == nil {
				if validateDHCPProcessSnapshot(
					snapshot,
					expectedArguments,
					resolverLink,
				) == nil {
					return errors.New("unowned DHCP PID is still active")
				}
			} else if !errors.Is(processErr, os.ErrNotExist) {
				return errors.New("inspect unowned DHCP PID")
			}
			if removeErr := os.Remove(networkPaths.pid); removeErr != nil {
				return fmt.Errorf("remove stale DHCP PID file: %w", removeErr)
			}
		} else if !errors.Is(pidErr, os.ErrNotExist) {
			return fmt.Errorf("inspect DHCP PID file: %w", pidErr)
		}
		return nil
	}

	if err != nil {
		return err
	}
	recordArguments := expectedDHCPArguments(
		record.Interface,
		record.Script,
		networkPaths,
	)
	if err := ensurePriorProcessGone(
		procAccess{},
		record,
		recordArguments,
	); err != nil {
		return err
	}
	return combineErrors(
		removeFile(networkPaths.owner),
		removeFile(networkPaths.pid),
	)
}

func readPIDFile(path string) (int, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return 0, err
	}
	uid, ownerErr := fileOwnerUID(info)
	if ownerErr != nil || uid != 0 || !info.Mode().IsRegular() ||
		info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 {
		return 0, errors.New("DHCP PID file is not root-controlled")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	if len(content) > 32 {
		return 0, errors.New("invalid DHCP PID file")
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
	if err != nil || pid < 2 {
		return 0, errors.New("invalid DHCP PID file")
	}
	return pid, nil
}

type dhcpChild struct {
	command *exec.Cmd
	done    chan error
	record  ownerRecord
}

func startDHCP(
	interfaceName string,
	scriptPath string,
	resolverLink string,
	networkPaths paths,
) (*dhcpChild, error) {
	var child *dhcpChild
	err := withLeaseLock(networkPaths.lock, func() error {
		var startErr error
		child, startErr = startDHCPLocked(
			interfaceName,
			scriptPath,
			resolverLink,
			networkPaths,
		)
		return startErr
	})
	if err != nil {
		if child != nil {
			_, stopErr := stopDHCP(child, networkPaths)
			err = combineErrors(err, stopErr)
		}
		return nil, err
	}
	return child, nil
}

func startDHCPLocked(
	interfaceName string,
	scriptPath string,
	resolverLink string,
	networkPaths paths,
) (*dhcpChild, error) {
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, errors.New("generate DHCP ownership token")
	}
	token := hex.EncodeToString(tokenBytes)
	arguments := expectedDHCPArguments(interfaceName, scriptPath, networkPaths)
	command := exec.Command(arguments[0], arguments[1:]...)
	command.Env = append(
		os.Environ(),
		"NETWORKD_EVENT=1",
		"NETWORKD_INTERFACE="+interfaceName,
		"NETWORKD_OWNER="+token,
		"NETWORKD_RESOLV_LINK="+resolverLink,
	)
	command.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
	if err := command.Start(); err != nil {
		return nil, errors.New("start DHCP client")
	}
	snapshot, err := (procAccess{}).Snapshot(command.Process.Pid, busyboxPath)
	if err != nil {
		return nil, combineErrors(
			errors.New("inspect started DHCP client"),
			killStartedCommand(command),
		)
	}
	record := ownerRecord{
		PID:          command.Process.Pid,
		StartTime:    snapshot.startTime,
		Token:        token,
		Interface:    interfaceName,
		Script:       scriptPath,
		ResolverLink: resolverLink,
	}
	if err := validateOwnedSnapshot(record, snapshot, arguments); err != nil {
		return nil, combineErrors(err, killStartedCommand(command))
	}
	if err := writeOwner(networkPaths.owner, record); err != nil {
		return nil, combineErrors(err, killStartedCommand(command))
	}
	child := &dhcpChild{
		command: command,
		done:    make(chan error, 1),
		record:  record,
	}
	go func() {
		child.done <- command.Wait()
	}()
	return child, nil
}

func killStartedCommand(command *exec.Cmd) error {
	if err := command.Process.Kill(); err != nil &&
		!errors.Is(err, os.ErrProcessDone) &&
		!errors.Is(err, syscall.ESRCH) {
		return errors.New("kill unowned DHCP client")
	}
	_ = command.Wait()
	return nil
}

func stopDHCP(child *dhcpChild, networkPaths paths) (bool, error) {
	if child != nil && child.command.Process != nil {
		err := child.command.Process.Signal(syscall.SIGTERM)
		if err != nil && !errors.Is(err, os.ErrProcessDone) &&
			!errors.Is(err, syscall.ESRCH) {
			return false, errors.New("signal DHCP client")
		}
		select {
		case <-child.done:
		case <-time.After(childStopTimeout):
			if err := child.command.Process.Kill(); err != nil &&
				!errors.Is(err, os.ErrProcessDone) &&
				!errors.Is(err, syscall.ESRCH) {
				return false, errors.New("kill DHCP client")
			}
			<-child.done
		}
	}
	return true, combineErrors(
		removeFile(networkPaths.owner),
		removeFile(networkPaths.pid),
	)
}

func querySupplicant(
	runner commandRunner,
	controlDirectory string,
	interfaceName string,
) (observation, error) {
	socketIdentity, err := validateControlSocket(controlDirectory, interfaceName)
	if err != nil {
		return observation{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	output, err := runner.Run(
		ctx,
		wpaCLIPath,
		"-p", controlDirectory,
		"-i", interfaceName,
		"status",
	)
	cancel()
	if err != nil {
		return observation{}, errors.New("query supplicant status")
	}
	values := make(map[string]string)
	for _, line := range strings.Split(string(output), "\n") {
		key, value, found := strings.Cut(line, "=")
		if found && (key == "wpa_state" || key == "id" || key == "ssid") {
			values[key] = value
		}
	}
	if values["wpa_state"] != "COMPLETED" {
		return observation{}, nil
	}
	sum := sha256.Sum256([]byte(
		socketIdentity + "\x00" + values["id"] + "\x00" + values["ssid"],
	))
	return observation{
		completed: true,
		identity:  hex.EncodeToString(sum[:]),
	}, nil
}

func validateControlSocket(
	controlDirectory string,
	interfaceName string,
) (string, error) {
	if !filepath.IsAbs(controlDirectory) {
		return "", errors.New("control directory must be absolute")
	}
	info, err := os.Lstat(controlDirectory)
	if err != nil {
		return "", fmt.Errorf("inspect control directory: %w", err)
	}
	uid, gid, err := fileOwnerUIDGID(info)
	if err != nil || uid != 0 || gid != 0 || !info.IsDir() ||
		info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0002 != 0 {
		return "", errors.New("supplicant control directory is not root-controlled")
	}
	socketPath := filepath.Join(controlDirectory, interfaceName)
	socketInfo, err := os.Lstat(socketPath)
	if err != nil {
		return "", fmt.Errorf("inspect supplicant control socket: %w", err)
	}
	uid, gid, err = fileOwnerUIDGID(socketInfo)
	if err != nil ||
		!validRootControlSocketMetadata(socketInfo.Mode(), uid, gid) {
		return "", errors.New("supplicant control socket is not root-controlled")
	}

	status, ok := socketInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return "", errors.New("supplicant socket identity is unavailable")
	}
	return fmt.Sprintf("%d:%d", status.Dev, status.Ino), nil
}

func validRootControlSocketMetadata(
	mode os.FileMode,
	uid uint32,
	gid uint32,
) bool {
	return uid == 0 &&
		gid == 0 &&
		mode&os.ModeSocket != 0 &&
		mode&os.ModeSymlink == 0 &&
		mode.Perm()&0002 == 0
}

func supervise(
	interfaceName string,
	controlDirectory string,
	resolverLink string,
) (resultErr error) {
	if os.Geteuid() != 0 {
		return errors.New("network lifecycle service must run as root")
	}
	if !interfacePattern.MatchString(interfaceName) {
		return errors.New("invalid interface name")
	}
	if err := ensureRuntimeDirectory(runtimeDirectory); err != nil {
		return err
	}
	if err := validateRootExecutable(busyboxPath); err != nil {
		return fmt.Errorf("validate BusyBox: %w", err)
	}
	if err := validateRootExecutable(wpaCLIPath); err != nil {
		return fmt.Errorf("validate wpa_cli: %w", err)
	}
	if err := validateResolverLinkParent(resolverLink, 0, true); err != nil {
		return err
	}
	scriptPath, err := os.Executable()
	if err != nil {
		return errors.New("locate networkd executable")
	}
	scriptPath, err = filepath.Abs(scriptPath)
	if err != nil {
		return errors.New("resolve networkd executable")
	}
	if err := validateRootExecutable(scriptPath); err != nil {
		return fmt.Errorf("validate networkd executable: %w", err)
	}
	networkPaths := networkPaths()
	supervisorLock, err := acquireSupervisorLock(
		networkPaths.supervisorLock,
		0,
	)
	if err != nil {
		return err
	}
	defer func() {
		resultErr = combineErrors(
			resultErr,
			releaseSupervisorLock(supervisorLock),
		)
	}()
	if err := replacePriorDHCP(
		networkPaths,
		interfaceName,
		scriptPath,
		resolverLink,
	); err != nil {
		return err
	}
	if err := withLeaseLock(networkPaths.lock, func() error {
		return combineErrors(
			clearLeaseState(
				execRunner{},
				networkPaths,
				interfaceName,
				resolverLink,
			),
			clearInterfaceNetworkState(
				execRunner{},
				interfaceName,
			),
		)
	}); err != nil {
		return fmt.Errorf("clear prior lease state: %w", err)
	}
	if err := clearEventFailureMarker(networkPaths.eventFailure, 0); err != nil {
		return fmt.Errorf("clear prior event failure: %w", err)
	}

	signalContext, stopSignals := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stopSignals()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	var (
		state           lifecycleState
		child           *dhcpChild
		failures        int
		retryDelay      = time.Second
		nextRetry       time.Time
		statusFailures  int
		childExitSignal <-chan error
		childStarted    time.Time
		cleanupPending  bool
		recoveringEvent bool
	)
	cleanup := func() error {
		stopped, stopErr := stopDHCP(child, networkPaths)
		if stopped {
			child = nil
			childExitSignal = nil
		} else {
			return stopErr
		}
		leaseErr := withLeaseLock(networkPaths.lock, func() error {
			return clearLeaseState(
				execRunner{},
				networkPaths,
				interfaceName,
				resolverLink,
			)
		})
		markerErr := clearEventFailureMarker(networkPaths.eventFailure, 0)
		return combineErrors(stopErr, leaseErr, markerErr)
	}

	log.Printf("network lifecycle supervision started interface=%s", interfaceName)
	for {
		observed, statusErr := querySupplicant(
			execRunner{},
			controlDirectory,
			interfaceName,
		)
		if statusErr != nil {
			if statusFailures == 0 {
				log.Printf("supplicant status query failed: %v", statusErr)
			}
			statusFailures++
			if statusFailures < maxCommandFailures {
				observed = observation{
					completed: state.connected,
					identity:  state.identity,
				}
			}
		} else {
			if statusFailures > 0 {
				log.Printf("supplicant status query recovered")
			}
			statusFailures = 0
		}
		if statusFailures >= maxCommandFailures {
			observed = observation{}
		}

		eventFailed, markerErr := eventFailureMarkerPresent(
			networkPaths.eventFailure,
			0,
		)
		if markerErr != nil {
			return combineErrors(markerErr, cleanup())
		}
		nextState, action := transition(
			state,
			observed,
			child != nil,
			eventFailed,
		)
		state = nextState
		if action == actionStop || action == actionRestart {
			log.Printf("network disconnected; removing DHCP state")
			cleanupPending = true
			failures = 0
			retryDelay = time.Second
			nextRetry = time.Time{}
		}
		if action == actionRecover {
			if !recoveringEvent {
				log.Printf("DHCP event failed; recovering client")
			}
			recoveringEvent = true
			cleanupPending = true
		}
		if cleanupPending && !time.Now().Before(nextRetry) {
			if err := cleanup(); err != nil {
				log.Printf("network cleanup failed; retrying")
				nextRetry = time.Now().Add(retryDelay)
				if retryDelay < 30*time.Second {
					retryDelay *= 2
					if retryDelay > 30*time.Second {
						retryDelay = 30 * time.Second
					}
				}
			} else {
				cleanupPending = false
				log.Printf("network state removed")
				if recoveringEvent {
					nextRetry, retryDelay = scheduleRecoveryRetry(
						time.Now(),
						retryDelay,
					)
					recoveringEvent = false
				}
			}
		}
		if (action == actionStart || action == actionRestart) &&
			!cleanupPending &&
			statusErr == nil &&
			!time.Now().Before(nextRetry) {
			started, startErr := startDHCP(
				interfaceName,
				scriptPath,
				resolverLink,
				networkPaths,
			)
			if startErr != nil {
				failures++
				log.Printf("DHCP client start failed attempt=%d", failures)
				nextRetry = time.Now().Add(retryDelay)
				if retryDelay < 30*time.Second {
					retryDelay *= 2
					if retryDelay > 30*time.Second {
						retryDelay = 30 * time.Second
					}
				}
			} else {
				child = started
				childExitSignal = child.done
				childStarted = time.Now()
				failures = 0
				nextRetry = time.Time{}
				log.Printf("DHCP client started")
			}
		}

		select {
		case <-signalContext.Done():
			log.Printf("network lifecycle supervision stopping")
			if err := cleanup(); err != nil {
				return fmt.Errorf("shutdown network cleanup: %w", err)
			}
			log.Printf("network state removed")
			return nil
		case <-ticker.C:
		case <-childExitSignal:
			log.Printf("DHCP client exited; removing DHCP state")
			child = nil
			childExitSignal = nil
			cleanupPending = true
			if err := cleanup(); err != nil {
				log.Printf("network cleanup failed; retrying")
			} else {
				cleanupPending = false
				log.Printf("network state removed")
			}
			if time.Since(childStarted) >= 30*time.Second {
				retryDelay = time.Second
			}
			nextRetry = time.Now().Add(retryDelay)
			if time.Since(childStarted) < 30*time.Second &&
				retryDelay < 30*time.Second {
				retryDelay *= 2
				if retryDelay > 30*time.Second {
					retryDelay = 30 * time.Second
				}
			}
		}
	}
}

func ensureRuntimeDirectory(path string) error {
	if err := os.Mkdir(path, 0700); err != nil &&
		!errors.Is(err, os.ErrExist) {
		return fmt.Errorf("create runtime directory: %w", err)
	}
	return validateRuntimeDirectory(path)
}

func validateRuntimeDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect runtime directory: %w", err)
	}
	uid, err := fileOwnerUID(info)
	if err != nil || uid != 0 || !info.IsDir() ||
		info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0700 {
		return errors.New("runtime directory must be a root-owned mode-0700 directory")
	}
	var filesystem syscall.Statfs_t
	if err := syscall.Statfs(path, &filesystem); err != nil {
		return fmt.Errorf("inspect runtime filesystem: %w", err)
	}
	filesystemType := uint64(filesystem.Type) & 0xffffffff
	if filesystemType != tmpfsMagic && filesystemType != ramfsMagic {
		return errors.New("runtime directory must reside on tmpfs or ramfs")
	}
	return nil
}

func validateRootExecutable(path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("executable path must be absolute")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect executable: %w", err)
	}
	uid, err := fileOwnerUID(info)
	if err != nil || uid != 0 || !info.Mode().IsRegular() ||
		info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 ||
		info.Mode().Perm()&0111 == 0 {
		return errors.New("executable is not a root-controlled executable")
	}
	return nil
}

func fileOwnerUID(info os.FileInfo) (uint32, error) {
	uid, _, err := fileOwnerUIDGID(info)
	return uid, err
}

func fileOwnerUIDGID(info os.FileInfo) (uint32, uint32, error) {
	status, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, errors.New("filesystem does not expose Unix ownership")
	}
	return status.Uid, status.Gid, nil
}

func writeAtomic(path string, content []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	file, err := os.CreateTemp(directory, ".networkd-*")
	if err != nil {
		return fmt.Errorf("create temporary state: %w", err)
	}
	temporaryPath := file.Name()
	defer os.Remove(temporaryPath)
	if err := file.Chmod(mode); err != nil {
		file.Close()
		return fmt.Errorf("restrict temporary state: %w", err)
	}
	if _, err := file.Write(content); err != nil {
		file.Close()
		return fmt.Errorf("write temporary state: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("sync temporary state: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close temporary state: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("activate state: %w", err)
	}
	return nil
}

func run() error {
	if os.Getenv("NETWORKD_EVENT") == "1" {
		if len(os.Args) != 2 {
			return errors.New("DHCP event requires exactly one action")
		}
		interfaceName := os.Getenv("NETWORKD_INTERFACE")
		resolverLink := os.Getenv("NETWORKD_RESOLV_LINK")
		if resolverLink == "" {
			resolverLink = defaultResolverLink
		}
		return runEvent(
			os.Args[1],
			interfaceName,
			resolverLink,
			networkPaths(),
		)
	}

	var interfaceName, controlDirectory, resolverLink string
	flag.StringVar(
		&interfaceName,
		"interface",
		defaultInterface,
		"station network interface",
	)
	flag.StringVar(
		&controlDirectory,
		"control-dir",
		defaultControlDir,
		"wpa_supplicant control directory",
	)
	flag.StringVar(
		&resolverLink,
		"resolv-link",
		defaultResolverLink,
		"system resolver symlink",
	)
	flag.Parse()
	if flag.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	return supervise(interfaceName, controlDirectory, resolverLink)
}

func main() {
	if err := run(); err != nil {
		log.Printf("ERROR: %v", err)
		os.Exit(1)
	}
}
