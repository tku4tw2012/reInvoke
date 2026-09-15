//go:build !nandbsl && !nandbslcompact

package probewrite

import "testing"

func TestLegacyTablePinAndRestoreSupportRemain(t *testing.T) {
	if !RestoreSupported || TableHash != "53fab213e1be985fae65abaaf1ad9de6fbb9f5f7c66cb2316bd65f2ef88d992f" {
		t.Fatal("legacy version-table pin or separately approved restoration changed")
	}
}
