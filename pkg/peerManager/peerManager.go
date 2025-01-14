package peermanager

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"

	clientidentifier "github.com/TheLox95/go-torrent-client/pkg/ClientIdentifier"
	downloadunit "github.com/TheLox95/go-torrent-client/pkg/downloadUnit"
	"github.com/TheLox95/go-torrent-client/pkg/peer"
	"github.com/TheLox95/go-torrent-client/pkg/peerMessage"
	"github.com/TheLox95/go-torrent-client/pkg/piece"
)

var Red = "\033[31m"
var Reset = "\033[0m"

type PeerStatus int
type PeerID string

const (
	Connected    PeerStatus = 1
	Disconnected PeerStatus = 2
	Busy         PeerStatus = 3
)

type TrackedPeer struct {
	peer.Peer
	status PeerStatus
}

type PeerManager struct {
	peers          map[PeerID]TrackedPeer
	availablePeers []*TrackedPeer
	Client         *(clientidentifier.ClientIdentifier)

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
		if p.status == Connected {
			total++
		}
	}
	return total
}

func (m *PeerManager) connectPeer(p *TrackedPeer) {
	if m.availablePeers == nil {
		m.availablePeers = make([]*TrackedPeer, 0)
	}

	if !p.IsConnected() {
		err := p.Connect(m.Client)
		if err == nil {
			m.availablePeers = append(m.availablePeers, p)
			if m.connectedPool == nil {
				m.connectedPool = &sync.Pool{}
			} else {
				p.PiecesAsked = 0
				m.connectedPool.Put(p)
			}
		}
	}
}

func (m *PeerManager) askPiece(unit *downloadunit.DownloadUnit, fileLength int, unitChan, completedChan chan *downloadunit.DownloadUnit) error {
	pieceSize := unit.Piece.CalculateSize(fileLength)

	fmt.Printf("asking piece %d to peer %s of size %d\n", unit.Piece.Idx, unit.Peer.GetID(), pieceSize)
	err := unit.Peer.RequestPiece(unit.Piece)
	if err != nil {
		m.SetStatus(PeerID(unit.Peer.GetID()), Disconnected)
		unit.Peer.CloseConnection()
		unitChan <- unit
		return errors.New("call to piece failed")
	} else if unit.Piece.Buf == nil {
		fmt.Println("piece is null", unit.Piece.Buf)
		unitChan <- unit
		return errors.New("piece is null")
	} else {
		err = unit.Piece.CheckIntegrity(unit.Piece.Buf)
		if err != nil {
			unitChan <- unit
			return errors.New("piece is corrupted")
		}
		unit.Peer.OnPieceRequestSucceed()
		unit.Status = downloadunit.Success
		completedChan <- unit
	}
	return nil
}
func (m *PeerManager) askPiece2(unit *downloadunit.DownloadUnit, fileLength int) error {
	pieceSize := unit.Piece.CalculateSize(fileLength)

	fmt.Printf("asking piece %d to peer %s of size %d\n", unit.Piece.Idx, unit.Peer.GetID(), pieceSize)
	err := unit.Peer.RequestPiece(unit.Piece)
	if err != nil {
		fmt.Printf(Red+"PIECE_ID [%d] RequestPiece failed with: %v\n"+Reset, unit.Piece.Idx, err)
		m.piecePool.Put(unit.Piece)
		if err.Error() == peerMessage.KEEP_ALIVE_MESSAGE {
			unit.Peer.PiecesAsked = 0
			m.connectedPool.Put(&TrackedPeer{Peer: *unit.Peer, status: Connected})
		} else if err.Error() == peerMessage.NON_EXPECTED_MSG_ID {
			unit.Peer.PiecesAsked = 0
			m.connectedPool.Put(&TrackedPeer{Peer: *unit.Peer, status: Connected})
		} else {
			m.SetStatus(PeerID(unit.Peer.GetID()), Disconnected)
			unit.Peer.CloseConnection()
			go m.connectPeer(&TrackedPeer{Peer: *unit.Peer, status: Connected})
		}
		return errors.New("call to piece failed")
	} else if unit.Piece.Buf == nil {
		fmt.Println("piece is null", unit.Piece.Buf)
		m.piecePool.Put(unit.Piece)
		return errors.New("piece is null")
	} else {
		err = unit.Piece.CheckIntegrity(unit.Piece.Buf)
		if err != nil {
			m.piecePool.Put(unit.Piece)
			go m.connectPeer(&TrackedPeer{Peer: *unit.Peer, status: Connected})
			return errors.New("piece is corrupted")
		}
		unit.Peer.OnPieceRequestSucceed()
		unit.Status = downloadunit.Success
		unit.Peer.PiecesAsked = 0
		m.connectedPool.Put(&TrackedPeer{Peer: *unit.Peer, status: Connected})
		m.piecesCompleted++
	}
	return nil
}

func (m *PeerManager) Download(pieceLength, fileLength int, hashes [][20]byte) {
	hashesLen := len(hashes)
	//unitChan := make(chan *downloadunit.DownloadUnit, hashesLen)
	//completedUnitChan := make(chan *downloadunit.DownloadUnit, hashesLen)
	pieces := make([]piece.Piece, hashesLen)
	for i := 0; i < hashesLen; i++ {
		p := piece.Piece{Idx: i, Hash: hashes[i], Length: pieceLength, Buf: nil}
		pieces[i] = p
		if m.piecePool == nil {
			m.piecePool = &sync.Pool{
				New: func() any {
					return &p
				},
			}
		} else {
			m.piecePool.Put(&p)
		}
		//unit := &downloadunit.DownloadUnit{Peer: nil, Piece: &p, Status: downloadunit.Failed}
		//unitChan <- unit
	}

	for _, peer := range m.peers {
		go m.connectPeer(&peer)
	}

	/*for downloadUnit := range unitChan {
		if piecesCompleted == hashesLen {
			break
		}

		if len(m.availablePeers) == 0 {
			time.Sleep(time.Second * 3)
			continue
		}

		peer := m.availablePeers[0]
		downloadUnit.Peer = &peer.Peer
		m.availablePeers = m.availablePeers[1:]

		fmt.Println("####AVAILABLE_IP: ", downloadUnit.Peer.IP, " for piece: ", downloadUnit.Piece.Idx)
		go m.askPiece(downloadUnit, fileLength, unitChan, completedUnitChan)
	}

	for piecesCompleted < hashesLen {
		completed := <-completedUnitChan
		if completed.Status == downloadunit.Success {
			piecesCompleted++
			fmt.Printf("----------- RECEIVED FROM: %s piece: %d, completed so far: %d\n", completed.Peer.GetID(), completed.Piece.Idx, piecesCompleted)
			m.availablePeers = append(m.availablePeers, &TrackedPeer{Peer: *completed.Peer, status: Connected})
		} else {
			pieces = append(pieces, *completed.Piece)
			if completed.Peer != nil {
				fmt.Printf("XXXXXXX RECEIVED FROM: %s piece: %d, completed so far: %d\n", completed.Peer.GetID(), completed.Piece.Idx, piecesCompleted)
			}
			unitChan <- completed
		}
	}*/
	for m.piecesCompleted < hashesLen {
		if m.connectedPool == nil {
			continue
		}
		aaa := m.connectedPool.Get()
		if aaa == nil {
			continue
		}

		thisPeer, _ := aaa.(*TrackedPeer)

		piece := m.piecePool.Get().(*piece.Piece)
		if piece == nil {
			continue
		}

		unit := &downloadunit.DownloadUnit{Peer: &thisPeer.Peer, Piece: piece, Status: downloadunit.Failed}

		fmt.Println("####AVAILABLE_IP: ", unit.Peer.IP, " pieces asked: ", unit.Peer.PiecesAsked, " for piece: ", unit.Piece.Idx)
		if unit.Peer.PiecesAsked < peer.MAX_REQUEST_PER_PEER {
			go m.askPiece2(unit, fileLength)
		}
		unit.Peer.PiecesAsked++
	}
}

func (m *PeerManager) Add(p *peer.Peer) {
	if m.peers == nil {
		m.peers = make(map[PeerID]TrackedPeer)
	}
	status := Disconnected
	peer := TrackedPeer{status: status, Peer: *p}
	peerUrl := net.JoinHostPort(p.IP.String(), strconv.Itoa(int(p.Port)))
	m.peers[PeerID(peerUrl)] = peer
}

func (m *PeerManager) SetStatus(id PeerID, status PeerStatus) (err error) {
	if m.peers == nil {
		m.peers = make(map[PeerID]TrackedPeer)
	}
	peer, ok := m.peers[id]
	if !ok {
		return errors.New("peer not found")
	}

	peer.status = status
	return nil
}

//TODO
// Retry to connect to disconnected peer
// call multiple pieces simultaneously
