// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

// Loading and preparation of the DSP boot image.
//
// The donor reads up to 614,400 bytes of dsp-img.ldr, reverses the bit order
// of every byte, and streams the result in four byte SPI transfers. The held
// image is 160,484 bytes, which is exactly 40,121 transfers, and a recorded
// ioctl trace of the donor contains exactly that many four byte transfers
// whose concatenated transmit bytes hash to the bit reversed image.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

const (
	// maxImageBytes is the donor's staging buffer size and its read limit.
	maxImageBytes = 614400

	// imageChunkBytes is the transfer size of the download stage.
	imageChunkBytes = 4

	// speedRestoreOffset is the byte offset at which the donor restores the
	// saved bus speed and pauses before continuing.
	speedRestoreOffset = 1536

	// recordedImageBytes and recordedImageTransfers describe the held image.
	recordedImageBytes     = 160484
	recordedImageTransfers = 40121

	// recordedImageSHA256 is the digest shared by all six preserved copies of
	// usr/share/dsp/dsp-img.ldr.
	recordedImageSHA256 = "e76f6ce7c53bb5b508507354fb08523089c136b3731d5ad4f4488a50526a44c8"

	// recordedStreamSHA256 is the digest of the bit reversed byte stream, and
	// of the concatenated transmit bytes of the 40,121 recorded four byte
	// transfers.
	recordedStreamSHA256 = "9e3d85f37ac62e191616f558359e7b4ec46ce6499167da991994ea0b944f34f2"
)

type bootImage struct {
	// Stream is the bit reversed image, exactly as it goes on the wire.
	Stream []byte

	// SourceSHA256 and StreamSHA256 are hex digests of the file as read and of
	// Stream.
	SourceSHA256 string
	StreamSHA256 string
}

// Transfers is the number of four byte SPI transfers the download needs.
func (image bootImage) Transfers() int {
	return len(image.Stream) / imageChunkBytes
}

// Matches reports whether the image is the recovered donor image, byte for
// byte.
func (image bootImage) Matches() bool {
	return image.SourceSHA256 == recordedImageSHA256 &&
		image.StreamSHA256 == recordedStreamSHA256 &&
		len(image.Stream) == recordedImageBytes &&
		image.Transfers() == recordedImageTransfers
}

// reverseBits reverses the bit order of one byte, most significant bit to
// least significant.
func reverseBits(value byte) byte {
	value = (value&0xf0)>>4 | (value&0x0f)<<4
	value = (value&0xcc)>>2 | (value&0x33)<<2
	return (value&0xaa)>>1 | (value&0x55)<<1
}

// loadBootImage reads the image at path, reverses every byte, and checks that
// the result can be streamed as whole four byte transfers. It never writes.
func loadBootImage(path string) (bootImage, error) {
	file, err := os.Open(path)
	if err != nil {
		return bootImage{}, fmt.Errorf("open DSP image: %w", err)
	}
	defer file.Close()

	source, err := io.ReadAll(io.LimitReader(file, maxImageBytes+1))
	if err != nil {
		return bootImage{}, fmt.Errorf("read DSP image: %w", err)
	}
	if len(source) > maxImageBytes {
		return bootImage{}, fmt.Errorf(
			"DSP image is larger than the %d byte staging buffer",
			maxImageBytes,
		)
	}
	if len(source) == 0 {
		return bootImage{}, fmt.Errorf("DSP image %s is empty", path)
	}
	if len(source)%imageChunkBytes != 0 {
		return bootImage{}, fmt.Errorf(
			"DSP image of %d bytes is not a whole number of %d byte transfers",
			len(source),
			imageChunkBytes,
		)
	}

	stream := make([]byte, len(source))
	for index, value := range source {
		stream[index] = reverseBits(value)
	}
	sourceDigest := sha256.Sum256(source)
	streamDigest := sha256.Sum256(stream)
	return bootImage{
		Stream:       stream,
		SourceSHA256: hex.EncodeToString(sourceDigest[:]),
		StreamSHA256: hex.EncodeToString(streamDigest[:]),
	}, nil
}

// verifyBootImage refuses an image that is not the recovered one. The caller
// can accept any well formed image instead, which is what a future DSP build
// would need.
func verifyBootImage(image bootImage) error {
	if image.Matches() {
		return nil
	}
	return fmt.Errorf(
		"DSP image is not the recovered image: %d bytes, %d transfers, "+
			"sha256 %s, bit reversed sha256 %s",
		len(image.Stream),
		image.Transfers(),
		image.SourceSHA256,
		image.StreamSHA256,
	)
}
