package downloadmanager

import (
	"errors"
	"fmt"

	clientidentifier "github.com/TheLox95/go-torrent-client/pkg/clientIdentifier"
	downloadunit "github.com/TheLox95/go-torrent-client/pkg/downloadUnit"
	"github.com/TheLox95/go-torrent-client/pkg/peer"
	"github.com/TheLox95/go-torrent-client/pkg/piece"
)

var Cyan = "\033[36m"
var Green = "\033[32m"
var Red = "\033[31m"
var Reset = "\033[0m"

type PeerManager interface {
	GetPeer() *peer.Peer
	AddPeer(p *peer.Peer)
	AvailablePeers() int
}

type DownloadManager struct {
	PeerManager     PeerManager
	PiecePool       chan *piece.Piece
	piecesCompleted []*piece.Piece
	Client          *(clientidentifier.ClientIdentifier)
	totalPieces int
}

func (m *DownloadManager) Download(pieceLength int, fileLength int, hashes [][20]byte) []byte {
	m.totalPieces = len(hashes)
	for i, hash := range hashes {
		p := piece.Piece{Idx: i, Hash: hash, Length: pieceLength, Buf: nil}
		m.PiecePool <- &p
	}

	for pw := range m.PiecePool {
		if len(m.piecesCompleted) == m.totalPieces {
			break
		}
		p := m.PeerManager.GetPeer()
		for p == nil {
			//m.PiecePool <- pw
			p = m.PeerManager.GetPeer()
			continue
		}
		if p.Bitfield.HasPiece(pw.Idx) == false {
				m.PeerManager.AddPeer(p)
				m.PiecePool <- pw
				continue
		}

		fmt.Println("@@@@@@@@@@@@@@@@@@@@@@ COMPLETED SO FAR", len(m.piecesCompleted), " out of ", m.totalPieces, " with ", m.PeerManager.AvailablePeers(), " peers available")
		if p.IsConnected() == false {
			err := p.Connect(m.Client)
			if err != nil {
				fmt.Println("failed to connect peer: ", p.IP)
				m.PeerManager.AddPeer(p)
				m.PiecePool <- pw
				continue
			}
		}
		unit := &downloadunit.DownloadUnit{Peer: p, Piece: pw, Status: downloadunit.Failed}
		go m.askPiece(unit, fileLength)
	}

	close(m.PiecePool)
	buf := make([]byte, fileLength)
	for i := 0; i < len(m.piecesCompleted); i++ {
		piece := m.piecesCompleted[i]

		begin, end := piece.CalculateBounds(fileLength)
		copy(buf[begin:end], piece.Buf)
	}
	return buf
}

func (m *DownloadManager) askPiece(unit *downloadunit.DownloadUnit, fileLength int) error {
	pieceSize := unit.Piece.CalculateSize(fileLength)

	fmt.Printf("asking piece %d to peer %s of size %d\n", unit.Piece.Idx, unit.Peer.GetID(), pieceSize)
	err := unit.Peer.RequestPiece(unit.Piece)
	m.PeerManager.AddPeer(unit.Peer)
	if err != nil {
		fmt.Printf(Red+"PIECE_ID [%d] RequestPiece failed for IP %s with: %v\n"+Reset, unit.Piece.Idx, unit.Peer.IP.String(), err)
		m.PiecePool <- unit.Piece
		return errors.New("call to piece failed")
	} else if unit.Piece.Buf == nil {
		fmt.Printf(Cyan+"piece is null putting pice [%d] back\n"+Reset, unit.Piece.Idx)
		m.PiecePool <- unit.Piece
		unit.Peer.PiecesAsked = 0
		return errors.New("piece is null")
	}
	err = unit.Piece.CheckIntegrity()
	if err != nil {
		unit.Peer.PiecesAsked = 0
		fmt.Printf(Green+"putting pice [%d] back\n"+Reset, unit.Piece.Idx)
		m.PiecePool <- unit.Piece
		fmt.Println(unit.Piece.Idx, " ::piece is corrupted")
		return errors.New("piece is corrupted")
	}
	unit.Peer.OnPieceRequestSucceed(unit.Piece.Idx)
	unit.Status = downloadunit.Success
	unit.Peer.PiecesAsked = 0
	m.piecesCompleted = append(m.piecesCompleted, unit.Piece)
	if (len(m.piecesCompleted) == m.totalPieces) {
		m.PiecePool <- unit.Piece
	}
				
	return nil
}
