package probewrite

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// This opt-in regression compiles the actual archived remove callback, not a
// duplicate of it. Only its generic block-layer dependency is modeled.
func TestArchivedKernelRemovalOwnership(t *testing.T) {
	source := os.Getenv("REINVOKE_KERNEL_SOURCE")
	if source == "" {
		t.Skip("set REINVOKE_KERNEL_SOURCE for the archived-source ownership regression")
	}
	repo := os.Getenv("REINVOKE_REPO")
	if repo == "" {
		t.Fatal("REINVOKE_REPO must identify the patch source")
	}
	original, err := os.ReadFile(filepath.Join(source, "drivers/mtd/mtdblock_ro.c"))
	if err != nil {
		t.Fatal(err)
	}
	scratch := t.TempDir()
	target := filepath.Join(scratch, "drivers/mtd")
	if err := os.MkdirAll(target, 0700); err != nil {
		t.Fatal(err)
	}
	patchedPath := filepath.Join(target, "mtdblock_ro.c")
	if err := os.WriteFile(patchedPath, original, 0600); err != nil {
		t.Fatal(err)
	}
	patchPath := filepath.Join(repo, "patches/invoke-kernel/0005-fix-mtdblock-removal-lifetime.patch")
	command := exec.Command("patch", "--batch", "--fuzz=0", "-p1", "-d", scratch, "-i", patchPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("apply patch: %v\n%s", err, output)
	}
	patched, err := os.ReadFile(patchedPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		source  []byte
		succeed bool
	}{{"original-fails", original, false}, {"patched-passes", patched, true}} {
		t.Run(test.name, func(t *testing.T) {
			start := bytes.Index(test.source, []byte("static void mtdblock_remove_dev("))
			if start < 0 {
				t.Fatal("remove callback not found")
			}
			end := bytes.Index(test.source[start:], []byte("\n}\n"))
			if end < 0 {
				t.Fatal("remove callback terminator not found")
			}
			callback := test.source[start : start+end+3]
			input := filepath.Join(scratch, test.name+".c")
			binary := filepath.Join(scratch, test.name)
			body := ownershipHarnessPrefix + string(callback) + ownershipHarnessSuffix
			if err := os.WriteFile(input, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			if output, err := exec.Command("cc", "-std=c11", "-Wall", "-Wextra", "-Werror", input, "-o", binary).CombinedOutput(); err != nil {
				t.Fatalf("compile callback regression: %v\n%s", err, output)
			}
			output, err := exec.Command(binary).CombinedOutput()
			if test.succeed {
				if err != nil || !strings.Contains(string(output), "ownership checks passed") {
					t.Fatalf("fixed callback failed: %v\n%s", err, output)
				}
			} else if err == nil || !strings.Contains(string(output), "device ownership violation") {
				t.Fatalf("negative control did not fail as expected: %v\n%s", err, output)
			}
		})
	}
}

const ownershipHarnessPrefix = `
#include <stdio.h>
#include <stddef.h>
#include <stdlib.h>
typedef unsigned short u16;
struct mtd_blktrans_dev { int placeholder; };
struct mtdblock_priv { struct mtd_blktrans_dev dev; unsigned int sectors_per_block; u16 *bb_map; };
static struct mtdblock_priv object;
static u16 map[4];
static int in_block_layer, deferred, device_frees, map_frees, stopped, bad;
#define dev2priv(dev) ((struct mtdblock_priv *)(dev))
static void kfree(void *p) {
    if (p == &object) {
        if (!in_block_layer || ++device_frees != 1) {
            fprintf(stderr, "device ownership violation\n");
            bad = 1;
        }
    } else if (p == map) {
        if (!stopped || ++map_frees != 1) bad = 1;
    } else if (p != NULL) bad = 1;
}
static void del_mtd_blktrans_dev(struct mtd_blktrans_dev *dev) {
    stopped = 1;
    if (!deferred) {
        in_block_layer = 1;
        kfree(dev);
        in_block_layer = 0;
    }
}
`

const ownershipHarnessSuffix = `
int main(void) {
    for (deferred = 0; deferred < 2; ++deferred) {
        object.bb_map = map;
        device_frees = map_frees = stopped = bad = 0;
        mtdblock_remove_dev(&object.dev);
        if (deferred) {
            in_block_layer = 1;
            kfree(&object.dev);
            in_block_layer = 0;
        }
        if (bad || device_frees != 1 || map_frees != 1) return 1;
    }
    puts("ownership checks passed");
    return 0;
}
`
