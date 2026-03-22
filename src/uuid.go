package main

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// uuid7 generates a new UUIDv7 with current timestamp and random bytes.
func uuid7() string {
	ts := time.Now().UnixMilli()

	randBytes := make([]byte, 10)
	if _, err := rand.Read(randBytes); err != nil {
		nanos := time.Now().UnixNano()
		for i := range randBytes {
			randBytes[i] = byte(nanos >> (i * 8))
		}
	}

	tsHex := fmt.Sprintf("%012x", ts)
	randHex := hex.EncodeToString(randBytes)
	varByte := (randBytes[2] & 0x3F) | 0x80
	varHex := fmt.Sprintf("%02x", varByte)

	return fmt.Sprintf("%s-%s-7%s-%s%s-%s",
		tsHex[0:8],
		tsHex[8:12],
		randHex[0:3],
		varHex,
		randHex[5:7],
		randHex[7:19])
}

// toV7 converts any UUID string to a deterministic UUIDv7 via MD5.
// This enables idempotent span creation from transcript entry UUIDs.
func toV7(uuid string) string {
	hash := md5.Sum([]byte(uuid))
	h := hex.EncodeToString(hash[:])

	b6 := (hash[6] & 0x0F) | 0x70
	b6Hex := fmt.Sprintf("%02x", b6)
	b8 := (hash[8] & 0x3F) | 0x80
	b8Hex := fmt.Sprintf("%02x", b8)

	return fmt.Sprintf("%s-%s-%s%s-%s%s-%s",
		h[0:8],
		h[8:12],
		b6Hex,
		h[14:16],
		b8Hex,
		h[18:20],
		h[20:32])
}

// traceIDFromUUID converts a UUID string to a 32-hex-char OTLP trace ID.
func traceIDFromUUID(uuid string) [16]byte {
	hash := md5.Sum([]byte(uuid))
	return hash
}

// spanIDFromUUID converts a UUID string to a 16-hex-char OTLP span ID.
func spanIDFromUUID(uuid string) [8]byte {
	hash := md5.Sum([]byte(uuid))
	var spanID [8]byte
	copy(spanID[:], hash[:8])
	return spanID
}

// newSpanID generates a random 8-byte span ID.
func newSpanID() [8]byte {
	var id [8]byte
	if _, err := rand.Read(id[:]); err != nil {
		nanos := time.Now().UnixNano()
		for i := range id {
			id[i] = byte(nanos >> (i * 8))
		}
	}
	return id
}

// newTraceID generates a random 16-byte trace ID.
func newTraceID() [16]byte {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		nanos := time.Now().UnixNano()
		for i := range id {
			id[i] = byte(nanos >> (i * 8))
		}
	}
	return id
}
