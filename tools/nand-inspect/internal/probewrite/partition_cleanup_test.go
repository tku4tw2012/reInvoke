//go:build !nandpilot

package probewrite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type fakePartitionKernel struct {
	label       string
	partitions  map[int]string
	addCalls    int
	listCalls   int
	removeCalls []int
	nodeCalls   []int
	addError    error
	listErrors  map[int]error
	removeError error
	nodesError  error
	hideOwned   bool
	afterRemove func()
}

func (k *fakePartitionKernel) add() error {
	k.addCalls++
	if k.addError != nil {
		return k.addError
	}
	k.partitions[0] = k.label
	return nil
}

func (k *fakePartitionKernel) entries() (map[int]string, error) {
	k.listCalls++
	if err := k.listErrors[k.listCalls]; err != nil {
		return nil, err
	}
	result := make(map[int]string)
	for index, label := range k.partitions {
		if !k.hideOwned || label != k.label {
			result[index] = label
		}
	}
	return result, nil
}

func (k *fakePartitionKernel) remove(index int) error {
	k.removeCalls = append(k.removeCalls, index)
	if k.removeError != nil {
		return k.removeError
	}
	delete(k.partitions, index)
	if k.afterRemove != nil {
		k.afterRemove()
	}
	return nil
}

func (k *fakePartitionKernel) removeNodes(index int) error {
	k.nodeCalls = append(k.nodeCalls, index)
	return k.nodesError
}

func partitionCleanupFixture(t *testing.T) (*MTD, *fakePartitionKernel, error) {
	t.Helper()
	dir := t.TempDir()
	master, err := os.Create(filepath.Join(dir, "master-placeholder"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { master.Close() })
	m := &MTD{master: master, label: "reinvoke-probe-owned-test", createdIndex: -1}
	discoveryError := errors.New("injected discovery failure after ADD")
	kernel := &fakePartitionKernel{
		label: m.label, partitions: map[int]string{masterIndex: "mv_nand"},
		listErrors: map[int]error{1: discoveryError},
	}
	err = m.addAndDiscoverPartition(kernel.add, func() (map[int]string, error) {
		if !m.partitionAdded || m.createdIndex != -1 {
			t.Fatal("successful ADD was not tracked before discovery")
		}
		return kernel.entries()
	}, func() { t.Fatal("discovery error must not be retried") })
	if !errors.Is(err, discoveryError) || !m.partitionAdded || m.createdIndex != -1 ||
		kernel.addCalls != 1 || kernel.partitions[0] != m.label {
		t.Fatalf("failed to inject actual post-ADD discovery failure: state=%+v err=%v", m, err)
	}
	return m, kernel, discoveryError
}

func TestSuccessfulADDDiscoveryFailureCanResolveDuringClose(t *testing.T) {
	m, kernel, operationError := partitionCleanupFixture(t)
	master := m.master
	if err := m.closeWithPartitionOps(kernel.entries, kernel.remove, kernel.removeNodes); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(kernel.removeCalls, []int{0}) || !reflect.DeepEqual(kernel.nodeCalls, []int{0}) ||
		len(kernel.partitions) != 1 || m.partitionAdded || m.createdIndex != -1 || m.master != nil {
		t.Fatalf("owned partition was not resolved and removed exactly once: state=%+v kernel=%+v", m, kernel)
	}
	if _, err := master.Stat(); err == nil || master.Fd() != ^uintptr(0) {
		t.Fatalf("master descriptor not closed: %v", err)
	}
	if operationError == nil || kernel.addCalls != 1 || m.writeEnabled ||
		m.EnableWrites(context.Background(), nil, nil, nil) == nil {
		t.Fatal("operation error lost, ADD retried or write enablement changed")
	}
}

func TestSuccessfulADDUnresolvedCleanupNeverReportsSuccess(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*MTD, *fakePartitionKernel)
	}{
		{"discovery keeps failing", func(_ *MTD, k *fakePartitionKernel) { k.listErrors[2] = errors.New("still unavailable") }},
		{"no match", func(_ *MTD, k *fakePartitionKernel) { k.hideOwned = true }},
		{"ambiguous", func(m *MTD, k *fakePartitionKernel) { k.partitions[2] = m.label }},
		{"foreign replacement", func(_ *MTD, k *fakePartitionKernel) { k.partitions[0] = "foreign" }},
		{"master changed", func(_ *MTD, k *fakePartitionKernel) { k.partitions[masterIndex] = "foreign master" }},
		{"master owns label", func(m *MTD, k *fakePartitionKernel) { k.partitions[masterIndex] = m.label }},
		{"unsupported index", func(m *MTD, k *fakePartitionKernel) { delete(k.partitions, 0); k.partitions[128] = m.label }},
		{"known index moved", func(m *MTD, k *fakePartitionKernel) {
			m.createdIndex = 0
			k.partitions[0] = "foreign"
			k.partitions[2] = m.label
		}},
		{"label lost", func(m *MTD, _ *fakePartitionKernel) { m.label = "" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			m, kernel, _ := partitionCleanupFixture(t)
			master := m.master
			test.mutate(m, kernel)
			err := m.closeWithPartitionOps(kernel.entries, kernel.remove, kernel.removeNodes)
			if err == nil || !strings.Contains(err.Error(), "cleanup incomplete") ||
				len(kernel.removeCalls) != 0 || len(kernel.nodeCalls) != 0 || !m.partitionAdded {
				t.Fatalf("unresolved ownership reported success or touched a partition: err=%v kernel=%+v", err, kernel)
			}
			if m.master != nil {
				t.Fatal("cleanup failure skipped closing the descriptor")
			}
			if _, err := master.Stat(); err == nil || master.Fd() != ^uintptr(0) {
				t.Fatalf("master descriptor not closed: %v", err)
			}
			if err := m.Close(); err == nil || !strings.Contains(err.Error(), "cleanup incomplete") {
				t.Fatalf("repeat Close forgot an unresolved successful ADD: %v", err)
			}
		})
	}
}

func TestSuccessfulADDCleanupRequiresConfirmedRemoval(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*MTD, *fakePartitionKernel)
	}{
		{"delete failure", func(_ *MTD, k *fakePartitionKernel) { k.removeError = errors.New("injected DEL failure") }},
		{"verification error", func(_ *MTD, k *fakePartitionKernel) { k.listErrors[3] = errors.New("verification unavailable") }},
		{"owned index remains", func(m *MTD, k *fakePartitionKernel) {
			k.afterRemove = func() { k.partitions[0] = m.label }
		}},
		{"foreign index reused", func(_ *MTD, k *fakePartitionKernel) {
			k.afterRemove = func() { k.partitions[0] = "foreign replacement" }
		}},
		{"owned label remains elsewhere", func(m *MTD, k *fakePartitionKernel) {
			k.afterRemove = func() { k.partitions[2] = m.label }
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			m, kernel, _ := partitionCleanupFixture(t)
			test.mutate(m, kernel)
			err := m.closeWithPartitionOps(kernel.entries, kernel.remove, kernel.removeNodes)
			if err == nil || !strings.Contains(err.Error(), "cleanup incomplete") ||
				!reflect.DeepEqual(kernel.removeCalls, []int{0}) || len(kernel.nodeCalls) != 0 || !m.partitionAdded {
				t.Fatalf("unconfirmed removal reported success or unlinked nodes: err=%v kernel=%+v", err, kernel)
			}
			if err := m.Close(); err == nil || len(kernel.removeCalls) != 1 {
				t.Fatalf("unconfirmed removal retried or forgotten: %v", err)
			}
		})
	}
}

func TestDiscoveryTimeoutRetainsSuccessfulADDForCleanup(t *testing.T) {
	m := &MTD{label: "reinvoke-probe-owned-test", createdIndex: -1}
	kernel := &fakePartitionKernel{
		label: m.label, partitions: map[int]string{masterIndex: "mv_nand"}, hideOwned: true,
	}
	pauses := 0
	err := m.addAndDiscoverPartition(kernel.add, kernel.entries, func() { pauses++ })
	if err == nil || !m.partitionAdded || m.createdIndex != -1 || kernel.addCalls != 1 ||
		kernel.listCalls != 20 || pauses != 20 || kernel.partitions[0] != m.label {
		t.Fatalf("discovery timeout lost successful ADD: state=%+v kernel=%+v err=%v", m, kernel, err)
	}
	if err := m.Close(); err == nil {
		t.Fatal("successful ADD without a usable master handle reported cleanup success")
	}
}

func TestFailedADDDoesNotInventCleanupOwnership(t *testing.T) {
	m := &MTD{label: "reinvoke-probe-owned-test", createdIndex: -1}
	injected := errors.New("ADD failed")
	err := m.addAndDiscoverPartition(func() error { return injected }, func() (map[int]string, error) {
		t.Fatal("discovery after failed ADD")
		return nil, nil
	}, func() { t.Fatal("pause after failed ADD") })
	if !errors.Is(err, injected) || m.partitionAdded || m.createdIndex != -1 {
		t.Fatalf("failed ADD invented ownership: state=%+v err=%v", m, err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("nothing was added, but Close invented an obligation: %v", err)
	}
}

func TestConfirmedRemovalNodeFailureIsNotSuccess(t *testing.T) {
	m, kernel, _ := partitionCleanupFixture(t)
	kernel.nodesError = errors.New("node cleanup failed")
	err := m.closeWithPartitionOps(kernel.entries, kernel.remove, kernel.removeNodes)
	if err == nil || !strings.Contains(err.Error(), kernel.nodesError.Error()) ||
		m.partitionAdded || m.createdIndex != -1 || !reflect.DeepEqual(kernel.nodeCalls, []int{0}) {
		t.Fatalf("node cleanup failure suppressed: state=%+v err=%v", m, err)
	}
}
