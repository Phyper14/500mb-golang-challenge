// Package rediskv implements storage.Store on top of Redis, using one
// sorted set per device (score = timestamp millis) as the primary index.
// Points are encoded as a compact fixed-size binary blob to avoid JSON
// marshal/unmarshal overhead on the hot path.
package rediskv

import (
	"encoding/binary"
	"errors"
	"math"
	"math/rand/v2"

	"github.com/Phyper14/500mb-golang-challenge/internal/domain"
)

// pointEncodedLen is the fixed size, in bytes, of an encoded point:
//
//	8  ts (int64 BE)
//	4  nonce (uint32 BE) - disambiguates members with identical ts+fields
//	8  lat (float64 bits BE)
//	8  lon (float64 bits BE)
//	1  battery present flag
//	8  battery (float64 bits BE, ignored when flag == 0)
//	8  ax (float64 bits BE)
//	8  ay (float64 bits BE)
//	8  az (float64 bits BE)
const pointEncodedLen = 8 + 4 + 8 + 8 + 1 + 8 + 8 + 8 + 8

// errShortBuffer is returned by DecodePoint when the input is smaller
// than the expected fixed encoding length.
var errShortBuffer = errors.New("rediskv: buffer too short to decode point")

// EncodePoint serializes p into a fixed-size binary blob suitable for use
// as a Redis sorted-set member. A random nonce is embedded so that two
// points with byte-identical fields (including ts) never collide as the
// same ZSET member, which would otherwise silently drop one of them.
func EncodePoint(p domain.Point) []byte {
	buf := make([]byte, pointEncodedLen)
	off := 0

	binary.BigEndian.PutUint64(buf[off:], uint64(p.TS))
	off += 8

	binary.BigEndian.PutUint32(buf[off:], rand.Uint32())
	off += 4

	binary.BigEndian.PutUint64(buf[off:], math.Float64bits(p.Lat))
	off += 8

	binary.BigEndian.PutUint64(buf[off:], math.Float64bits(p.Lon))
	off += 8

	if p.HasBattery {
		buf[off] = 1
	}
	off++

	var batteryVal float64
	if p.HasBattery {
		batteryVal = p.Battery
	}
	binary.BigEndian.PutUint64(buf[off:], math.Float64bits(batteryVal))
	off += 8

	binary.BigEndian.PutUint64(buf[off:], math.Float64bits(p.AX))
	off += 8

	binary.BigEndian.PutUint64(buf[off:], math.Float64bits(p.AY))
	off += 8

	binary.BigEndian.PutUint64(buf[off:], math.Float64bits(p.AZ))
	off += 8

	return buf
}

// DecodePoint reverses EncodePoint. It returns errShortBuffer if buf is
// smaller than the fixed encoding length; extra trailing bytes are
// ignored so the format could grow in the future without breaking old
// members already stored in Redis.
func DecodePoint(buf []byte) (domain.Point, error) {
	if len(buf) < pointEncodedLen {
		return domain.Point{}, errShortBuffer
	}

	off := 0
	var p domain.Point

	p.TS = int64(binary.BigEndian.Uint64(buf[off:]))
	off += 8

	off += 4 // skip nonce, not part of the domain model

	p.Lat = math.Float64frombits(binary.BigEndian.Uint64(buf[off:]))
	off += 8

	p.Lon = math.Float64frombits(binary.BigEndian.Uint64(buf[off:]))
	off += 8

	p.HasBattery = buf[off] == 1
	off++

	p.Battery = math.Float64frombits(binary.BigEndian.Uint64(buf[off:]))
	off += 8

	p.AX = math.Float64frombits(binary.BigEndian.Uint64(buf[off:]))
	off += 8

	p.AY = math.Float64frombits(binary.BigEndian.Uint64(buf[off:]))
	off += 8

	p.AZ = math.Float64frombits(binary.BigEndian.Uint64(buf[off:]))
	off += 8

	return p, nil
}
