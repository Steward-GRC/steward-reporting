// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package strip_test

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Steward-GRC/steward-reporting/internal/strip"
)

// marker stands in for what a camera or an editor writes: a name, a device
// and a place. None of it may survive the strip.
const marker = "Alice Example, Example Phone 7, GPS 40.7128N 74.0060W"

func picture() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for x := range 8 {
		for y := range 8 {
			img.Set(x, y, color.RGBA{uint8(x * 30), uint8(y * 30), 120, 255})
		}
	}
	return img
}

// jpegWithMetadata adds an EXIF APP1 segment, an XMP APP1 segment and a
// comment right after the SOI marker.
func jpegWithMetadata(t *testing.T) []byte {
	t.Helper()
	var plain bytes.Buffer
	require.NoError(t, jpeg.Encode(&plain, picture(), nil))
	seg := func(m byte, payload []byte) []byte {
		out := []byte{0xFF, m, 0, 0}
		binary.BigEndian.PutUint16(out[2:], uint16(len(payload)+2))
		return append(out, payload...)
	}
	var b bytes.Buffer
	b.Write(plain.Bytes()[:2])
	b.Write(seg(0xE1, append([]byte("Exif\x00\x00"), marker...)))
	b.Write(seg(0xE1, append([]byte("http://ns.adobe.com/xap/1.0/\x00"), marker...)))
	b.Write(seg(0xFE, []byte(marker)))
	b.Write(plain.Bytes()[2:])
	return b.Bytes()
}

// pngWithMetadata adds tEXt and eXIf chunks after IHDR.
func pngWithMetadata(t *testing.T) []byte {
	t.Helper()
	var plain bytes.Buffer
	require.NoError(t, png.Encode(&plain, picture()))
	chunk := func(typ string, data []byte) []byte {
		out := make([]byte, 8, 12+len(data))
		binary.BigEndian.PutUint32(out, uint32(len(data)))
		copy(out[4:], typ)
		out = append(out, data...)
		crc := crc32.ChecksumIEEE(append([]byte(typ), data...))
		return binary.BigEndian.AppendUint32(out, crc)
	}
	raw := plain.Bytes()
	const ihdrEnd = 8 + 25
	var b bytes.Buffer
	b.Write(raw[:ihdrEnd])
	b.Write(chunk("tEXt", append([]byte("Author\x00"), marker...)))
	b.Write(chunk("eXIf", []byte(marker)))
	b.Write(raw[ihdrEnd:])
	return b.Bytes()
}

// gifWithMetadata adds a comment extension after the logical screen.
func gifWithMetadata(t *testing.T) []byte {
	t.Helper()
	pal := image.NewPaletted(image.Rect(0, 0, 8, 8), color.Palette{color.Black, color.White})
	var plain bytes.Buffer
	require.NoError(t, gif.Encode(&plain, pal, nil))
	raw := plain.Bytes()
	// header (6) + logical screen descriptor (7) + global colour table (2*3)
	const screenEnd = 6 + 7 + 6
	var b bytes.Buffer
	b.Write(raw[:screenEnd])
	b.Write([]byte{0x21, 0xFE, byte(len(marker))})
	b.WriteString(marker)
	b.WriteByte(0)
	b.Write(raw[screenEnd:])
	return b.Bytes()
}

func TestStripRemovesImageMetadata(t *testing.T) {
	cases := []struct {
		name, ctype string
		data        []byte
		decode      func([]byte) error
	}{
		{"jpeg", "image/jpeg", jpegWithMetadata(t), func(b []byte) error { _, err := jpeg.Decode(bytes.NewReader(b)); return err }},
		{"png", "image/png", pngWithMetadata(t), func(b []byte) error { _, err := png.Decode(bytes.NewReader(b)); return err }},
		{"gif", "image/gif", gifWithMetadata(t), func(b []byte) error { _, err := gif.DecodeAll(bytes.NewReader(b)); return err }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Contains(t, string(c.data), marker, "the fixture carries metadata")
			out, err := strip.Strip(c.data)
			require.NoError(t, err)
			require.Equal(t, c.ctype, out.ContentType)
			require.NotContains(t, string(out.Data), marker)
			require.NotContains(t, string(out.Data), "Exif")
			require.NotContains(t, string(out.Data), "tEXt")
			require.NotContains(t, string(out.Data), "eXIf")
			require.NotContains(t, string(out.Data), "adobe")
			require.NoError(t, c.decode(out.Data), "the stripped file is still a picture")
		})
	}
}

func TestStripKeepsPlainText(t *testing.T) {
	out, err := strip.Strip([]byte("About one page, maybe 30 names.\n"))
	require.NoError(t, err)
	require.Equal(t, "text/plain", out.ContentType)
	require.Equal(t, "About one page, maybe 30 names.\n", string(out.Data))
}

// The type is sniffed from the bytes, so a document renamed to .jpg is
// still refused.
func TestStripRefusesEverythingElse(t *testing.T) {
	for name, data := range map[string][]byte{
		"pdf":           []byte("%PDF-1.7\n1 0 obj << /Author (" + marker + ") >>"),
		"zip":           {'P', 'K', 3, 4, 0, 0, 0, 0},
		"html":          []byte("<html><body>" + marker + "</body></html>"),
		"broken jpeg":   {0xFF, 0xD8, 0xFF, 0xE0, 0, 0},
		"invalid utf-8": {'a', 0xC3, 0x28, 'b'},
		"empty":         {},
	} {
		_, err := strip.Strip(data)
		require.ErrorIs(t, err, strip.ErrUnsupported, name)
	}
}

func TestStripRefusesHugeFiles(t *testing.T) {
	_, err := strip.Strip(bytes.Repeat([]byte("a"), strip.MaxBytes+1))
	require.ErrorIs(t, err, strip.ErrUnsupported)
}

// A tiny file that declares an enormous canvas is refused before decoding.
func TestStripRefusesDecompressionBombs(t *testing.T) {
	var b bytes.Buffer
	require.NoError(t, png.Encode(&b, image.NewGray(image.Rect(0, 0, 1, 1))))
	raw := b.Bytes()
	binary.BigEndian.PutUint32(raw[16:], 100000)
	binary.BigEndian.PutUint32(raw[20:], 100000)
	crc := crc32.ChecksumIEEE(raw[12:29])
	binary.BigEndian.PutUint32(raw[29:], crc)
	_, err := strip.Strip(raw)
	require.ErrorIs(t, err, strip.ErrUnsupported)
}

func TestNeutralName(t *testing.T) {
	require.Equal(t, "attachment-1.jpg", strip.NeutralName(1, "image/jpeg"))
	require.Equal(t, "attachment-3.txt", strip.NeutralName(3, "text/plain"))
}
