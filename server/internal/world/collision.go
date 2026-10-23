package world

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
)

const (
	collisionHeaderLen = 22
	collisionVersion   = 1
)

// Grid is the static collision grid shared with the client (shared/map,
// collision.bin). World units are metres: X east, Y south (the map's z).
// It is immutable after loading and safe for concurrent reads.
type Grid struct {
	cellSize float64 // metres
	width    int
	height   int
	originX  float64 // metres, west edge
	originY  float64 // metres, north edge
	bits     []byte
}

// ParseGrid decodes the collision.bin format documented in shared/map/README.md.
func ParseGrid(data []byte) (*Grid, error) {
	if len(data) < collisionHeaderLen {
		return nil, fmt.Errorf("collision grid: %d bytes, need at least %d", len(data), collisionHeaderLen)
	}
	if string(data[0:4]) != "LCOL" {
		return nil, fmt.Errorf("collision grid: bad magic %q", data[0:4])
	}
	if v := data[4]; v != collisionVersion {
		return nil, fmt.Errorf("collision grid: unsupported version %d", v)
	}
	cell := binary.LittleEndian.Uint16(data[6:])
	w := int(binary.LittleEndian.Uint16(data[10:]))
	h := int(binary.LittleEndian.Uint16(data[12:]))
	ox := int32(binary.LittleEndian.Uint32(data[14:]))
	oz := int32(binary.LittleEndian.Uint32(data[18:]))
	if cell == 0 || w == 0 || h == 0 {
		return nil, fmt.Errorf("collision grid: zero cell size or dimensions (%d, %dx%d)", cell, w, h)
	}
	n := (w*h + 7) / 8
	if len(data)-collisionHeaderLen != n {
		return nil, fmt.Errorf("collision grid: %d data bytes, want %d for %dx%d", len(data)-collisionHeaderLen, n, w, h)
	}
	return &Grid{
		cellSize: float64(cell) / 10,
		width:    w,
		height:   h,
		originX:  float64(ox) / 10,
		originY:  float64(oz) / 10,
		bits:     data[collisionHeaderLen:],
	}, nil
}

// LoadGrid reads and decodes a collision grid file.
func LoadGrid(path string) (*Grid, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("load collision grid: %w", err)
	}
	g, err := ParseGrid(data)
	if err != nil {
		return nil, fmt.Errorf("load collision grid %s: %w", path, err)
	}
	return g, nil
}

// Size returns the grid dimensions in cells.
func (g *Grid) Size() (w, h int) { return g.width, g.height }

// CellBlocked reports whether cell (col,row) is blocked. Cells outside the
// grid are blocked.
func (g *Grid) CellBlocked(col, row int) bool {
	if col < 0 || row < 0 || col >= g.width || row >= g.height {
		return true
	}
	idx := row*g.width + col
	return g.bits[idx>>3]>>(uint(idx)&7)&1 == 1
}

// Blocked reports whether the world position p is in a blocked cell.
// Non-finite positions are blocked.
func (g *Grid) Blocked(p Vec2) bool {
	cx := math.Floor((float64(p.X) - g.originX) / g.cellSize)
	cy := math.Floor((float64(p.Y) - g.originY) / g.cellSize)
	if math.IsNaN(cx) || math.IsNaN(cy) || math.Abs(cx) > math.MaxInt32 || math.Abs(cy) > math.MaxInt32 {
		return true
	}
	return g.CellBlocked(int(cx), int(cy))
}

// CellSize returns the cell edge length in metres.
func (g *Grid) CellSize() float64 { return g.cellSize }
