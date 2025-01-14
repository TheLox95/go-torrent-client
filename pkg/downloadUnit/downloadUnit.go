package downloadunit

import (
	"github.com/TheLox95/go-torrent-client/pkg/peer"
	"github.com/TheLox95/go-torrent-client/pkg/piece"
)

type UnitStatus int

const (
	Success UnitStatus = 1
	Failed  UnitStatus = 2
)

type DownloadUnit struct {
	Piece  *piece.Piece
	Peer   *peer.Peer
	Status UnitStatus
}
