package bencodetorrent

import (
	bencodeinfo "github.com/TheLox95/go-torrent-client/pkg/bencodeInfo"
)

type BencodeTorrent struct {
	Announce     string                  `bencode:"announce"`
	AnnounceList [][]string              `bencode:"announce-list"`
	Info         bencodeinfo.BencodeInfo `bencode:"info"`
}
