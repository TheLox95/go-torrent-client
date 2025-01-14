package piece

import (
	"bytes"
	"crypto/sha1"
	"errors"
)

// MaxBlockSize is the largest number of bytes a request can ask for
const MaxBlockSize = 16384

type Piece struct {
	Idx    int
	Hash   [20]byte
	Buf    []byte
	Length int
}

func (p *Piece) CalculateBounds(torrentLength int) (begin int, end int) {
	begin = p.Idx * p.Length
	end = begin + p.Length
	if end > torrentLength {
		end = torrentLength
	}
	return begin, end
}

func (p *Piece) CalculateSize(torrentLength int) (size int) {
	begin, end := p.CalculateBounds(torrentLength)
	return end - begin
}

func (p *Piece) CalculateBlockSize(totalDownloaded int) (size int) {
	blockSize := MaxBlockSize
	// Last block might be shorter than the typical block
	if p.Length-totalDownloaded < blockSize {
		blockSize = p.Length - totalDownloaded
	}
	return blockSize
}

func (p *Piece) CheckIntegrity(buf []byte) error {
	sha1 := sha1.Sum(buf)
	if !bytes.Equal(sha1[:], p.Hash[:]) {
		return errors.New("failed integrity check")
	}
	return nil
}
