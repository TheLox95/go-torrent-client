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
	PeerManager           PeerManager
	PiecePool             chan *piece.Piece
	piecesCompletedAmount int
	piecesCompleted       []*piece.Piece
	Client                *(clientidentifier.ClientIdentifier)
	FileManager           *(filemanager.FileManager)
	totalPieces           int
	MaxParallelDownload   int
	activeDownloads       int
}

func (m *DownloadManager) Download(pieceLength int, fileLength int, hashes [][20]byte) []byte {
	m.FileManager.LoadMetadata()
	m.totalPieces = len(hashes)
	for i, hash := range hashes {
		if m.FileManager.PieceAlreadyDownloaded(&i) == false {
			pieceLen := pieceLength
			if i == m.totalPieces-1 {
				pieceLen = fileLength - (pieceLength * (m.totalPieces - 1))
			}
			if pieceLen < pieceLength {
				fmt.Println(i, m.totalPieces, pieceLen)
			}
			p := piece.Piece{Idx: i, Hash: hash, Length: pieceLen, Buf: nil}
			m.PiecePool <- &p
		} else {
			m.piecesCompletedAmount++
		}
	}

	for pw := range m.PiecePool {
		//if m.activeDownloads >= m.MaxParallelDownload {
		//	m.PiecePool <- pw
		//}
		if m.piecesCompletedAmount == m.totalPieces {
			break
		}
		p := m.PeerManager.GetPeer()
		//FIX: sometimes Bitfield is empty, should not be like that never
		for p == nil || p.Bitfield.Len() == 0 {
			p = m.PeerManager.GetPeer()
			continue
		}
		if p.Bitfield.HasPiece(pw.Idx) == false {
			m.PeerManager.AddPeer(p)
			m.PiecePool <- pw
			continue
		}

		fmt.Println("@@@@@@@@@@@@@@@@@@@@@@ COMPLETED SO FAR", m.piecesCompletedAmount, " out of ", m.totalPieces, " with ", m.PeerManager.AvailablePeers(), " peers available")
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
		go m.askPiece(unit, fileLength, pieceLength)
	}

	close(m.PiecePool)
	buf := make([]byte, fileLength)
	for i := 0; i < len(m.piecesCompleted); i++ {
		piece := m.piecesCompleted[i]

		begin, _ := piece.CalculateBounds(fileLength, pieceLength)
		//copy(buf[piece.Idx*pieceLength:], piece.Buf)
		copy(buf[begin:], piece.Buf)
	}
	return buf
}

func (m *DownloadManager) askPiece(unit *downloadunit.DownloadUnit, fileLength, pieceLength int) error {
	pieceSize := unit.Piece.CalculateSize(fileLength, pieceLength)

	fmt.Printf("asking piece %d to peer %s of size %d\n", unit.Piece.Idx, unit.Peer.GetID(), pieceSize)
	m.activeDownloads++
	err := unit.Peer.RequestPiece(unit.Piece)
	m.activeDownloads--
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
		return errors.New("piece is corrupted")
	}
	unit.Peer.OnPieceRequestSucceed(unit.Piece.Idx)
	unit.Status = downloadunit.Success
	unit.Peer.PiecesAsked = 0
	unit.Peer.PiecesDownloaded++
	m.piecesCompletedAmount++
	m.FileManager.AddToFile(unit.Piece)
	m.piecesCompleted = append(m.piecesCompleted, unit.Piece)
	if m.piecesCompletedAmount == m.totalPieces {
		m.PiecePool <- unit.Piece
	}
	return nil
}
