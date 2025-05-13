// SPDX-FileCopyrightText: 2025 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// TiffReader can read TIFF images produced by our own pipeline.
// It is not a general reader for arbitrary image files from other programs.
type TiffReader struct {
	r                                              io.ReadSeeker
	order                                          binary.ByteOrder
	imageWidth, imageHeight, tileWidth, tileHeight uint32
	tileOffsets, tileByteCounts                    []uint32
}

func NewTiffReader(r io.ReadSeeker) (*TiffReader, error) {
	tr := &TiffReader{r: r}
	if err := tr.readFirstIFD(); err != nil {
		return nil, err
	}
	return tr, nil
}

// ReadFirstIFD reads the first Image File Desriptor (IFD) in the TIFF file.
func (t *TiffReader) readFirstIFD() error {
	var header [4]byte
	if _, err := t.r.Read(header[:]); err != nil {
		return err
	}

	if bytes.Equal(header[:], []byte{'I', 'I', 42, 0}) {
		t.order = binary.LittleEndian
	} else if bytes.Equal(header[:], []byte{'M', 'M', 0, 42}) {
		t.order = binary.BigEndian
	} else {
		return fmt.Errorf("unsupported format")
	}

	var ifdOffset uint32
	if err := binary.Read(t.r, t.order, &ifdOffset); err != nil {
		return err
	}
	if _, err := t.r.Seek(int64(ifdOffset), os.SEEK_SET); err != nil {
		return err
	}

	var numDirEntries uint16
	if err := binary.Read(t.r, t.order, &numDirEntries); err != nil {
		return err
	}

	var ifd bytes.Buffer
	if _, err := io.CopyN(&ifd, t.r, int64(numDirEntries)*12); err != nil {
		return err
	}

	for i := uint16(0); i < numDirEntries; i++ {
		var tag, typ uint16
		var count, value uint32
		if err := binary.Read(&ifd, t.order, &tag); err != nil {
			return err
		}
		if err := binary.Read(&ifd, t.order, &typ); err != nil {
			return err
		}
		if err := binary.Read(&ifd, t.order, &count); err != nil {
			return err
		}
		switch typ {
		case 3: // SHORT
			var sval1, sval2 uint16
			if err := binary.Read(&ifd, t.order, &sval1); err != nil {
				return err
			}
			binary.Read(&ifd, t.order, &sval2)
			value = uint32(sval1)

		default: // LONG
			if err := binary.Read(&ifd, t.order, &value); err != nil {
				return err
			}
		}

		switch tag {
		case 256: // ImageWidth
			t.imageWidth = value

		case 257: // ImageLength
			t.imageHeight = value

		case 322: // TileWidth
			t.tileWidth = value

		case 323: // TileLength
			t.tileHeight = value

		case 324: // TileOffsets
			if a, err := t.readIntArray(typ, count, value); err == nil {
				t.tileOffsets = a
			} else {
				return err
			}

		case 325: // TileByteCounts
			if a, err := t.readIntArray(typ, count, value); err == nil {
				t.tileByteCounts = a
			} else {
				return err
			}
		}
	}

	return nil
}

// ReadIntArray reads an array of uint32 into memory. This is used
// internally for reading TileOffsets and TileByteCounts.
func (t *TiffReader) readIntArray(typ uint16, count, value uint32) ([]uint32, error) {
	if typ != 4 {
		return nil, fmt.Errorf("got type=%d, want 4", typ)
	}

	if _, err := t.r.Seek(int64(value), os.SEEK_SET); err != nil {
		return nil, err
	}

	result := make([]uint32, count)
	if err := binary.Read(t.r, t.order, &result); err != nil {
		return nil, err
	}

	return result, nil
}

// ReadTile reads a single image tile into memory.
func (t *TiffReader) ReadTile(tileIndex int, data any) error {
	if _, err := t.r.Seek(int64(t.tileOffsets[tileIndex]), os.SEEK_SET); err != nil {
		return err
	}

	n := int64(t.tileByteCounts[tileIndex])
	var buf bytes.Buffer
	if _, err := io.CopyN(&buf, t.r, n); err != nil {
		return err
	}

	reader, err := zlib.NewReader(&buf)
	if err != nil {
		return err
	}

	if err := binary.Read(reader, t.order, data); err != nil {
		return err
	}

	return nil
}
