package peermanager

import (
	"errors"
	"fmt"
	"net"
	"runtime"
	"strconv"
	"strings"
	"sync"

	clientidentifier "github.com/TheLox95/go-torrent-client/pkg/ClientIdentifier"
	downloadunit "github.com/TheLox95/go-torrent-client/pkg/downloadUnit"
	"github.com/TheLox95/go-torrent-client/pkg/peer"
	"github.com/TheLox95/go-torrent-client/pkg/peerMessage"
	"github.com/TheLox95/go-torrent-client/pkg/piece"
)

var Red = "\033[31m"
var Reset = "\033[0m"

type PeerManager struct {
	peers  map[peer.PeerID]*peer.Peer
	Client *(clientidentifier.ClientIdentifier)

	connectedPool   *sync.Pool
	piecePool       *sync.Pool
	piecesCompleted int
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
		}
	}
}

func (m *PeerManager) askPiece(unit *downloadunit.DownloadUnit, fileLength int) error {
	pieceSize := unit.Piece.CalculateSize(fileLength)

	fmt.Printf("asking piece %d to peer %s of size %d\n", unit.Piece.Idx, unit.Peer.GetID(), pieceSize)
	err := unit.Peer.RequestPiece(unit.Piece)
	if err != nil {
		fmt.Printf(Red+"PIECE_ID [%d] RequestPiece failed for IP %s with: %v\n"+Reset, unit.Piece.Idx, unit.Peer.IP.String(), err)
		m.piecePool.Put(unit.Piece)
		if err.Error() == peerMessage.KEEP_ALIVE_MESSAGE {
			unit.Peer.PiecesAsked = 0
			m.connectedPool.Put(unit.Peer)
		} else if err.Error() == peerMessage.NON_EXPECTED_MSG_ID {
			unit.Peer.PiecesAsked = 0
			m.connectedPool.Put(unit.Peer)
		} else if strings.Contains(err.Error(), "ected index") {
			unit.Peer.PiecesAsked = 0
			m.connectedPool.Put(unit.Peer)
		} else {
			m.SetStatus(peer.PeerID(unit.Peer.GetID()), peer.Disconnected)
			unit.Peer.CloseConnection()
			go m.connectPeer(unit.Peer)
		}
		return errors.New("call to piece failed")
	} else if unit.Piece.Buf == nil {
		fmt.Println("piece is null", unit.Piece.Buf)
		m.piecePool.Put(unit.Piece)
		m.connectedPool.Put(unit.Peer)
		return errors.New("piece is null")
	}
	err = unit.Piece.CheckIntegrity()
	if err != nil {
		unit.Peer.PiecesAsked = 0
		m.piecePool.Put(unit.Piece)
		m.connectedPool.Put(unit.Peer)
		fmt.Println(unit.Piece.Idx, " ::piece is corrupted")
		return errors.New("piece is corrupted")
	}
	unit.Peer.OnPieceRequestSucceed(unit.Piece.Idx)
	unit.Status = downloadunit.Success
	unit.Peer.PiecesAsked = 0
	m.connectedPool.Put(unit.Peer)
	m.piecesCompleted++
	return nil
}

func (m *PeerManager) Download(pieceLength, fileLength int, hashes [][20]byte) {
	hashesLen := len(hashes)
	if m.piecePool == nil {
		m.piecePool = &sync.Pool{}
	}
	for i := 0; i < hashesLen; i++ {
		p := piece.Piece{Idx: i, Hash: hashes[i], Length: pieceLength, Buf: nil}
		m.piecePool.Put(&p)
	}

	for _, peer := range m.peers {
		go m.connectPeer(peer)
	}

	for m.piecesCompleted < hashesLen {
		if m.connectedPool == nil {
			continue
		}
		aaa := m.connectedPool.Get()
		if aaa == nil {
			continue
		}

		thisPeer, _ := aaa.(*peer.Peer)

		item := m.piecePool.Get()
		if item == nil {
			m.connectedPool.Put(thisPeer)
			continue
		}
		piece := item.(*piece.Piece)

		unit := &downloadunit.DownloadUnit{Peer: thisPeer, Piece: piece, Status: downloadunit.Failed}

		fmt.Println("####AVAILABLE_IP: ", unit.Peer.IP, " pieces asked: ", unit.Peer.PiecesAsked, " for piece: ", unit.Piece.Idx, " completed so far: ", m.piecesCompleted, " workers:", runtime.NumGoroutine()-1)
		if unit.Peer.PiecesAsked < peer.MAX_REQUEST_PER_PEER {
			go m.askPiece(unit, fileLength)
			unit.Peer.PiecesAsked++
		}

	}
}

func (m *PeerManager) Add(p *peer.Peer) {
	if m.peers == nil {
		m.peers = make(map[peer.PeerID]*peer.Peer)
	}
	p.Status = peer.Disconnected
	peerUrl := net.JoinHostPort(p.IP.String(), strconv.Itoa(int(p.Port)))
	m.peers[peer.PeerID(peerUrl)] = p
}

func (m *PeerManager) SetStatus(id peer.PeerID, status peer.PeerStatus) (err error) {
	if m.peers == nil {
		m.peers = make(map[peer.PeerID]*peer.Peer)
	}
	peer, ok := m.peers[id]
	if !ok {
		return errors.New("peer not found")
	}

	peer.Status = status
	return nil
}
