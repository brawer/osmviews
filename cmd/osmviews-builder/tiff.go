// SPDX-FileCopyrightText: 2025 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
)

// TiffReader can read TIFF images produced by our own pipeline.
// It is not a general reader for arbitrary image files from other programs.
type TiffReader struct {
	r                                              io.ReaderAt
	order                                          binary.ByteOrder
	imageWidth, imageHeight, tileWidth, tileHeight uint32
	tileOffsets, tileByteCounts                    []uint32
	maxValue                                       float32
}

func NewTiffReader(r io.ReaderAt) (*TiffReader, error) {
	tr := &TiffReader{r: r}
	if err := tr.readFirstIFD(); err != nil {
		return nil, err
	}
	return tr, nil
}

// ReadFirstIFD reads the first Image File Desriptor (IFD) in the TIFF file.
func (t *TiffReader) readFirstIFD() error {
	var header [8]byte
	if _, err := t.r.ReadAt(header[:], 0); err != nil {
		return err
	}

	if bytes.Equal(header[:4], []byte{'I', 'I', 42, 0}) {
		t.order = binary.LittleEndian
	} else if bytes.Equal(header[:4], []byte{'M', 'M', 0, 42}) {
		t.order = binary.BigEndian
	} else {
		return fmt.Errorf("unsupported format")
	}

	ifdOffset := int64(t.order.Uint32(header[4:8]))
	numDirEntries, err := t.readUint16(ifdOffset)
	if err != nil {
		return err
	}

	var ifd bytes.Buffer
	ifdSize := int64(numDirEntries) * 12
	ifdReader := io.NewSectionReader(t.r, ifdOffset+2, ifdSize)
	if _, err := io.CopyN(&ifd, ifdReader, ifdSize); err != nil {
		return err
	}

	for i := uint16(0); i < numDirEntries; i++ {
		var tag, typ uint16
		var count, value uint32
		var floatValue float32
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

		case 11: // FLOAT
			if err := binary.Read(&ifd, t.order, &floatValue); err != nil {
				return err
			}

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

		case 341: // sMaxSampleValue
			t.maxValue = floatValue
		}
	}

	return nil
}

// ReadUInt16 reads an unsigned 16-bit integer from the TIFF file,
// starting at pos.
func (t *TiffReader) readUint16(pos int64) (uint16, error) {
	var buf [2]byte
	n, err := t.r.ReadAt(buf[:], pos)
	if err != nil {
		return 0, err
	}
	if n != 2 {
		return 0, io.ErrUnexpectedEOF
	}

	num := t.order.Uint16(buf[:])
	return num, nil
}

// ReadIntArray reads an array of uint32 into memory. This is used
// internally for reading TileOffsets and TileByteCounts.
func (t *TiffReader) readIntArray(typ uint16, count, value uint32) ([]uint32, error) {
	if typ != 4 {
		return nil, fmt.Errorf("got type=%d, want 4", typ)
	}

	result := make([]uint32, count)
	reader := io.NewSectionReader(t.r, int64(value), int64(count)*4)
	if err := binary.Read(reader, t.order, &result); err != nil {
		return nil, err
	}

	return result, nil
}

// ReadTile reads a single image tile into memory.
// Clients can execute parallel ReadTile calls on the same TiffReader.
func (t *TiffReader) ReadTile(tileIndex int, data any) error {
	tileOffset := int64(t.tileOffsets[tileIndex])
	tileSize := int64(t.tileByteCounts[tileIndex])
	tileReader := io.NewSectionReader(t.r, tileOffset, tileSize)
	zlibReader, err := zlib.NewReader(tileReader)
	if err != nil {
		return err
	}

	if err := binary.Read(zlibReader, t.order, data); err != nil {
		return err
	}

	return nil
}
