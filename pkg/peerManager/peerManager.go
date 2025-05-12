package peermanager

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"

	clientidentifier "github.com/TheLox95/go-torrent-client/pkg/clientIdentifier"
	downloadunit "github.com/TheLox95/go-torrent-client/pkg/downloadUnit"
	"github.com/TheLox95/go-torrent-client/pkg/peer"
	"github.com/TheLox95/go-torrent-client/pkg/piece"
)

var Cyan = "\033[36m"
var Green = "\033[32m"
var Red = "\033[31m"
var Reset = "\033[0m"

type PeerManager struct {
	peers  map[peer.PeerID]*peer.Peer
	Client *(clientidentifier.ClientIdentifier)

	connectedPool         *sync.Pool
	piecePool             chan *piece.Piece
	piecesCompletedAmount int
	activeDownloads       int
	piecesCompleted       []*piece.Piece
}

func (m *PeerManager) TotalAvailable() int {
	if m.peers == nil {
		return 0
	}
	return len(m.peers)
}

func (m *PeerManager) TotalAvailableConnected() int {
	if m.peers == nil {
		return 0
	}
	total := 0
	for _, p := range m.peers {
		if p.Status == peer.Connected {
			total++
		}
	}
	return total
}

func (m *PeerManager) connectPeer(p *peer.Peer) {
	if !p.IsConnected() {
		err := p.Connect(m.Client)
		if err == nil {
			if m.connectedPool == nil {
				m.connectedPool = &sync.Pool{}
			} else {
				p.PiecesAsked = 0
				m.connectedPool.Put(p)
			}
		} else {
			if p.ConnectionAttemps <= peer.MAX_CONNECTION_ATTEMPS {
				p.ConnectionAttemps++
				//time.AfterFunc(5*time.Second, func() { m.connectPeer(p) })
			}
		}
	}
}

func (m *PeerManager) askPiece(unit *downloadunit.DownloadUnit, fileLength int) error {
	pieceSize := unit.Piece.CalculateSize(fileLength)

	fmt.Printf("asking piece %d to peer %s of size %d\n", unit.Piece.Idx, unit.Peer.GetID(), pieceSize)
	err := unit.Peer.RequestPiece(unit.Piece)
	if err != nil {
		fmt.Printf(Red+"PIECE_ID [%d] RequestPiece failed for IP %s with: %v\n"+Reset, unit.Piece.Idx, unit.Peer.IP.String(), err)
		m.piecePool <- unit.Piece
		//if err.Error() == peerMessage.KEEP_ALIVE_MESSAGE {
		//	unit.Peer.PiecesAsked = 0
		//	m.connectedPool.Put(unit.Peer)
		//} else if err.Error() == peerMessage.NON_EXPECTED_MSG_ID {
		//	unit.Peer.PiecesAsked = 0
		//	m.connectedPool.Put(unit.Peer)
		//} else if strings.Contains(err.Error(), "ected index") {
		//	unit.Peer.PiecesAsked = 0
		//	m.connectedPool.Put(unit.Peer)
		//} else {
		//	unit.Peer.PiecesAsked = 0
		//	//m.SetStatus(peer.PeerID(unit.Peer.GetID()), peer.Disconnected)
		//	//unit.Peer.CloseConnection()
		//	//go m.connectPeer(unit.Peer)
		//}
		return errors.New("call to piece failed")
	} else if unit.Piece.Buf == nil {
		fmt.Println("piece is null", unit.Piece.Buf)
		fmt.Printf(Cyan+"putting pice [%d] back\n"+Reset, unit.Piece.Idx)
		m.piecePool <- unit.Piece
		unit.Peer.PiecesAsked = 0
		m.connectedPool.Put(unit.Peer)
		return errors.New("piece is null")
	}
	err = unit.Piece.CheckIntegrity()
	if err != nil {
		unit.Peer.PiecesAsked = 0
		fmt.Printf(Green+"putting pice [%d] back\n"+Reset, unit.Piece.Idx)
		m.piecePool <- unit.Piece
		m.connectedPool.Put(unit.Peer)
		fmt.Println(unit.Piece.Idx, " ::piece is corrupted")
		return errors.New("piece is corrupted")
	}
	unit.Peer.OnPieceRequestSucceed(unit.Piece.Idx)
	unit.Status = downloadunit.Success
	unit.Peer.PiecesAsked = 0
	m.connectedPool.Put(unit.Peer)
	m.piecesCompletedAmount++
	m.piecesCompleted = append(m.piecesCompleted, unit.Piece)
	return nil
}

func (m *PeerManager) startExchange(p *peer.Peer, fileLength int) {
	m.connectPeer(p)
	for pw := range m.piecePool {
		if p.PiecesAsked >= peer.MAX_REQUEST_PER_PEER || p.IsConnected() == false {
			m.piecePool <- pw
			continue
		}
		p.PiecesAsked++
		unit := &downloadunit.DownloadUnit{Peer: p, Piece: pw, Status: downloadunit.Failed}
		m.activeDownloads++

		fmt.Println("####AVAILABLE_IP: ", unit.Peer.IP, " pieces asked: ", unit.Peer.PiecesAsked, " for piece: ", unit.Piece.Idx, " completed so far: ", m.piecesCompletedAmount, " workers:", m.activeDownloads)
		m.askPiece(unit, fileLength)
		m.activeDownloads--
	}
}

func (m *PeerManager) Download(pieceLength, fileLength int, hashes [][20]byte) []byte {
	hashesLen := len(hashes)
	if m.piecePool == nil {
		m.piecePool = make(chan *piece.Piece, hashesLen)
	}
	for i := 0; i < hashesLen; i++ {
		p := piece.Piece{Idx: i, Hash: hashes[i], Length: pieceLength, Buf: nil}
		m.piecePool <- &p
	}

	for idx, peer := range m.peers {
		fmt.Println("conecting ", idx)
		go m.startExchange(peer, fileLength)
	}
	for m.piecesCompletedAmount < hashesLen {

	}
	close(m.piecePool)
	buf := make([]byte, fileLength)
	for i := 0; i < len(m.piecesCompleted); i++ {
		piece := m.piecesCompleted[i]

		begin, end := piece.CalculateBounds(fileLength)
		copy(buf[begin:end], piece.Buf)
	}
	return buf
}

func (m *PeerManager) Add(p *peer.Peer) {
	if m.peers == nil {
		m.peers = make(map[peer.PeerID]*peer.Peer)
	}
	p.Status = peer.Disconnected
	peerUrl := net.JoinHostPort(p.IP.String(), strconv.Itoa(int(p.Port)))
	m.peers[peer.PeerID(peerUrl)] = p
}
