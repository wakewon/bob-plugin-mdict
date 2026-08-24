// Package testmdx writes tiny, from-scratch MDX fixtures for integration tests.
// It is test support only and contains no third-party dictionary content.
package testmdx

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"hash/adler32"
	"os"
	"path/filepath"
	"sort"
)

// Entry is one synthetic headword and its raw HTML record.
type Entry struct {
	Key  string
	HTML string
}

// Write creates a minimal MDict v2 UTF-8 file at path.
func Write(path string, source []Entry) error {
	if len(source) == 0 {
		return fmt.Errorf("test MDX requires at least one entry")
	}
	entries := append([]Entry(nil), source...)
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })

	var definitions, keyBlock []byte
	for _, entry := range entries {
		start := len(definitions)
		definitions = append(definitions, entry.HTML...)
		keyBlock = binary.BigEndian.AppendUint64(keyBlock, uint64(start))
		keyBlock = append(keyBlock, entry.Key...)
		keyBlock = append(keyBlock, 0)
	}

	keyBlockData, err := compressedBlock(keyBlock)
	if err != nil {
		return err
	}
	first, last := []byte(entries[0].Key), []byte(entries[len(entries)-1].Key)
	keyInfo := binary.BigEndian.AppendUint64(nil, uint64(len(entries)))
	keyInfo = binary.BigEndian.AppendUint16(keyInfo, uint16(len(first)))
	keyInfo = append(keyInfo, first...)
	keyInfo = append(keyInfo, 0)
	keyInfo = binary.BigEndian.AppendUint16(keyInfo, uint16(len(last)))
	keyInfo = append(keyInfo, last...)
	keyInfo = append(keyInfo, 0)
	keyInfo = binary.BigEndian.AppendUint64(keyInfo, uint64(len(keyBlockData)))
	keyInfo = binary.BigEndian.AppendUint64(keyInfo, uint64(len(keyBlock)))
	keyInfoBlock, err := compressedBlock(keyInfo)
	if err != nil {
		return err
	}

	header := []byte(`<Dictionary GeneratedByEngineVersion="2.0" Encoding="UTF-8" Title="Synthetic case dictionary" Description="From-scratch test fixture" />`)
	headerUTF16 := make([]byte, 0, (len(header)+1)*2)
	for _, value := range header {
		headerUTF16 = append(headerUTF16, value, 0)
	}
	headerUTF16 = append(headerUTF16, 0, 0)

	var file bytes.Buffer
	write := func(order binary.ByteOrder, value any) error {
		return binary.Write(&file, order, value)
	}
	if err := write(binary.BigEndian, uint32(len(headerUTF16))); err != nil {
		return err
	}
	file.Write(headerUTF16)
	if err := write(binary.LittleEndian, adler32.Checksum(headerUTF16)); err != nil {
		return err
	}
	for _, value := range []uint64{1, uint64(len(entries)), uint64(len(keyInfo)), uint64(len(keyInfoBlock)), uint64(len(keyBlockData))} {
		if err := write(binary.BigEndian, value); err != nil {
			return err
		}
	}
	if err := write(binary.BigEndian, uint32(0)); err != nil {
		return err
	}
	file.Write(keyInfoBlock)
	file.Write(keyBlockData)

	recordBlock := []byte{0, 0, 0, 0}
	recordBlock = binary.BigEndian.AppendUint32(recordBlock, adler32.Checksum(definitions))
	recordBlock = append(recordBlock, definitions...)
	for _, value := range []uint64{1, uint64(len(entries)), 16, uint64(len(recordBlock)), uint64(len(recordBlock)), uint64(len(definitions))} {
		if err := write(binary.BigEndian, value); err != nil {
			return err
		}
	}
	file.Write(recordBlock)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, file.Bytes(), 0o600)
}

func compressedBlock(data []byte) ([]byte, error) {
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	if _, err := writer.Write(data); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	block := []byte{2, 0, 0, 0}
	block = binary.BigEndian.AppendUint32(block, adler32.Checksum(data))
	return append(block, compressed.Bytes()...), nil
}
