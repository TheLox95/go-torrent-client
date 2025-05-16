package downloadmanager

import (
	"errors"
	"fmt"

	clientidentifier "github.com/TheLox95/go-torrent-client/pkg/clientIdentifier"
	downloadunit "github.com/TheLox95/go-torrent-client/pkg/downloadUnit"
	filemanager "github.com/TheLox95/go-torrent-client/pkg/fileManager"
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
	piecesCompleted int
	Client          *(clientidentifier.ClientIdentifier)
	FileManager     *(filemanager.FileManager)
	totalPieces     int
}

func (m *DownloadManager) Download(pieceLength int, fileLength int, hashes [][20]byte) []byte {
	m.FileManager.LoadMetadata()
	m.totalPieces = len(hashes)
	for i, hash := range hashes {
		if m.FileManager.PieceAlreadyDownloaded(&i) == false {
			pieceLen := pieceLength
			if i == m.totalPieces-1 {
				pieceLen = fileLength - ( pieceLength*(m.totalPieces-1) )
			}
			p := piece.Piece{Idx: i, Hash: hash, Length: pieceLen, Buf: nil}
			m.PiecePool <- &p
		} else {
			m.piecesCompleted++
		}
	}

	for pw := range m.PiecePool {
		if m.piecesCompleted > 310 {
			break
		}
		if m.piecesCompleted == m.totalPieces {
			break
		}
		p := m.PeerManager.GetPeer()
		if p == nil {
			m.PiecePool <- pw
			continue
		}
		if p.Bitfield.HasPiece(pw.Idx) == false {
			m.PeerManager.AddPeer(p)
			m.PiecePool <- pw
			continue
		}

		fmt.Println("@@@@@@@@@@@@@@@@@@@@@@ COMPLETED SO FAR", m.piecesCompleted, " out of ", m.totalPieces, " with ", m.PeerManager.AvailablePeers(), " peers available")
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

	//close(m.PiecePool)
	buf := make([]byte, fileLength)
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
	m.piecesCompleted++
	m.FileManager.AddToFile(unit.Piece)
	if m.piecesCompleted == m.totalPieces {
		m.PiecePool <- unit.Piece
	}
	return nil
}
