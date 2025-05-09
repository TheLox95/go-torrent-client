package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"maps"
	mathRand "math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"time"

	bencodetorrent "github.com/TheLox95/go-torrent-client/pkg/bencodeTorrent"
	clientidentifier "github.com/TheLox95/go-torrent-client/pkg/clientIdentifier"
	"github.com/TheLox95/go-torrent-client/pkg/peer"
	peermanager "github.com/TheLox95/go-torrent-client/pkg/peerManager"
	peermanager2 "github.com/TheLox95/go-torrent-client/pkg/peerManager2"

	bencode "github.com/jackpal/bencode-go"
)

type TorrentFile struct {
	Announce    string
	InfoHash    [20]byte
	PieceHashes [][20]byte
	PieceLength int
	Length      int
	Name        string
}

type bencodeTrackerResp struct {
	Interval int    `bencode:"interval"`
	Peers    string `bencode:"peers"`
}

var peerID [20]byte
var _, err = rand.Read(peerID[:])

func getPeerList(bto *bencodetorrent.BencodeTorrent) (peers []peer.Peer, infoHash [20]byte) {
	base, err := url.Parse(bto.Announce)
	if err != nil {
		fmt.Println("Could not parse Announce")
		os.Exit(1)
	}

	var buf bytes.Buffer
	info := &bto.Info
	err = bencode.Marshal(&buf, *info)
	if err != nil {
		fmt.Println("Could not Marshal encodeInfo")
		os.Exit(1)
	}

	infoHash = sha1.Sum(buf.Bytes())

	Port := 6881

	params := url.Values{
		"info_hash":  []string{string(infoHash[:])},
		"peer_id":    []string{string(peerID[:])},
		"port":       []string{strconv.Itoa(Port)},
		"uploaded":   []string{"0"},
		"downloaded": []string{"0"},
		"compact":    []string{"1"},
		"left":       []string{strconv.Itoa(bto.Info.Length)},
	}
	base.RawQuery = params.Encode()

	url := base.String()
	c := &http.Client{Timeout: 15 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		fmt.Println("Request to get peer list failed", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	trackerResp := bencodeTrackerResp{}
	err = bencode.Unmarshal(resp.Body, &trackerResp)
	if err != nil {
		fmt.Println("Could not parse response")
		os.Exit(1)
	}
	peersBin := []byte(trackerResp.Peers)
	const peerSize = 6 // 4 for IP, 2 for port
	totalOfPeers := len(peersBin) / peerSize
	if len(peersBin)%peerSize != 0 {
		fmt.Println("Could not parse response")
		os.Exit(1)
	}
	fmt.Println("Peer amount: ", totalOfPeers)
	peersSlice := make([]peer.Peer, totalOfPeers)
	for i := 0; i < totalOfPeers; i++ {
		offset := i * peerSize
		peersSlice[i].IP = net.IP(peersBin[offset : offset+4])
		peersSlice[i].Port = binary.BigEndian.Uint16(peersBin[offset+4 : offset+6])
	}
	return peersSlice, infoHash
}

var transactionID = mathRand.Uint32()

func main() {
	bto := bencodetorrent.BencodeTorrent{}
	var buf bytes.Buffer
	info := &bto.Info

	//torrentPath := "./nasa2.torrent"
	torrentPath := "./debian.torrent"

	file, err := os.Open(torrentPath)
	if err != nil {
		fmt.Println("Could not read torrent file")
		os.Exit(1)
	}
	defer file.Close()

	err = bencode.Unmarshal(file, &bto)
	if err != nil {
		fmt.Println("Could not parse torrent file", err)
		os.Exit(1)
	}

	err = bencode.Marshal(&buf, *info)
	if err != nil {
		fmt.Println("Could not Marshal encodeInfo")
		os.Exit(1)
	}
	infoHash := sha1.Sum(buf.Bytes())

	announceSlice := make([]string, len(bto.AnnounceList)+1)
	for idx, item := range bto.AnnounceList {
		announceSlice[idx] = item[0]
	}
	announceSlice[len(announceSlice)-1] = bto.Announce
	slices.Sort(announceSlice)
	announceSlice = slices.Compact(announceSlice)

	peerManager2 := peermanager2.PeerManager2{
		Urls:  announceSlice,
		Peers: make(map[string]*peer.Peer),
	}

	params := peermanager2.GetPeersFromUDPParams{
		TransactionID: transactionID,
		InfoHash:      infoHash,
		PeerID:        peerID,
		TorrentLen:    bto.Info.Length,
	}
	peerManager2.PoolTrackers(&params)

	time.Sleep(time.Second * 15)

	os.Exit(1)
	peers := slices.Collect(maps.Values(peerManager2.Peers))
	//peers, _ := getPeerList(&bto)

	peerManager := peermanager.PeerManager{
		Client: &clientidentifier.ClientIdentifier{
			PeerID:   peerID,
			InfoHash: infoHash,
		},
	}
	for p := 0; p < len(peers); p++ {
		peer := peers[p]
		peerManager.Add(peer)
	}

	hashes, err := bto.Info.SplitPieceHashes()
	if err != nil {
		fmt.Println("could not parce pieces hashes", err)
		os.Exit(1)
	}

	fileBuffer := peerManager.Download(bto.Info.PieceLength, bto.Info.Length, hashes)

	outFile, err := os.Create("./debian.iso")
	if err != nil {
		fmt.Println("could not create file", err)
		os.Exit(1)
	}
	defer outFile.Close()
	_, err = outFile.Write(fileBuffer)
	if err != nil {
		fmt.Println("could not write file", err)
		os.Exit(1)
	}

	/*hashLen := 20 // Length of SHA-1 hash
	piecesBuf := []byte(bto.Info.Pieces)
	if len(piecesBuf)%hashLen != 0 {
		fmt.Errorf("Received malformed pieces of length %d", len(piecesBuf))
		os.Exit(1)
	}
	numHashes := len(piecesBuf) / hashLen
	hashes := make([][20]byte, numHashes)

	for i := 0; i < numHashes; i++ {
		copy(hashes[i][:], piecesBuf[i*hashLen:(i+1)*hashLen])
	}
	fmt.Println("hashes amount: ", len(hashes))

	fileBuffer := make([]byte, bto.Info.Length)

	for p := 0; p < len(peers); p++ {
		peer := peers[p]
		downloadManager.ConnectToPeer(peer, infoHash, peerID, fileBuffer)
	}

	permissions := os.FileMode(0644) // or whatever you need
	err = os.WriteFile("file.txt", fileBuffer, permissions)
	if err != nil {
		// handle error
	}

	outFile, err := os.Create("./file.txt")
	if err != nil {
		fmt.Errorf("Could not create end file")
		os.Exit(1)
	}
	defer outFile.Close()
	_, err = outFile.Write(fileBuffer)
	if err != nil {
		fmt.Errorf("Could not write file")
		os.Exit(1)
	}*/
}
