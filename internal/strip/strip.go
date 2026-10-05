// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

// Package strip removes metadata from report attachments before they are
// stored. Pictures are decoded and encoded again, so nothing but the pixels
// survives: no EXIF, XMP or IPTC, no comments, no text chunks. Plain text has
// no metadata to remove. Every other type is refused, because a document
// format's metadata can't be removed reliably without a parser for it.
package strip

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

// MaxBytes is the largest attachment taken.
const MaxBytes = 10 << 20

// maxPixels bounds a picture's canvas, so a small file can't declare a
// canvas that takes gigabytes to decode.
const maxPixels = 40_000_000

const jpegQuality = 90

// ErrUnsupported means the file is not a JPEG, PNG, GIF or UTF-8 text file,
// can't be decoded, or is too large.
var ErrUnsupported = errors.New("strip: unsupported attachment")

// Result is a stripped file.
type Result struct {
	// ContentType is sniffed from the bytes, never taken from the uploader.
	ContentType string
	Data        []byte
}

// Strip returns data without its metadata.
func Strip(data []byte) (Result, error) {
	if len(data) == 0 || len(data) > MaxBytes {
		return Result{}, ErrUnsupported
	}
	sniffed, _, _ := strings.Cut(http.DetectContentType(data), ";")
	switch sniffed {
	case "image/jpeg", "image/png", "image/gif":
		return reencode(sniffed, data)
	case "text/plain":
		if !utf8.Valid(data) {
			return Result{}, ErrUnsupported
		}
		return Result{ContentType: "text/plain", Data: data}, nil
	}
	return Result{}, ErrUnsupported
}

func reencode(ctype string, data []byte) (Result, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxPixels {
		return Result{}, ErrUnsupported
	}
	var out bytes.Buffer
	switch ctype {
	case "image/jpeg":
		img, err := jpeg.Decode(bytes.NewReader(data))
		if err != nil {
			return Result{}, ErrUnsupported
		}
		err = jpeg.Encode(&out, img, &jpeg.Options{Quality: jpegQuality})
		if err != nil {
			return Result{}, fmt.Errorf("strip: encode jpeg: %w", err)
		}
	case "image/png":
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return Result{}, ErrUnsupported
		}
		if err := png.Encode(&out, img); err != nil {
			return Result{}, fmt.Errorf("strip: encode png: %w", err)
		}
	case "image/gif":
		g, err := gif.DecodeAll(bytes.NewReader(data))
		if err != nil {
			return Result{}, ErrUnsupported
		}
		if err := gif.EncodeAll(&out, g); err != nil {
			return Result{}, fmt.Errorf("strip: encode gif: %w", err)
		}
	}
	return Result{ContentType: ctype, Data: out.Bytes()}, nil
}

var extensions = map[string]string{"image/jpeg": "jpg", "image/png": "png", "image/gif": "gif", "text/plain": "txt"}

// NeutralName is the stored name of an anonymous report's n-th attachment:
// the uploaded name can carry a person's name, so it is never kept.
func NeutralName(n int, contentType string) string {
	return "attachment-" + strconv.Itoa(n) + "." + extensions[contentType]
}
