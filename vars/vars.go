package vars

var GitVersion string

const (
	Version         = "custom"
	ProtocolVersion = "1021"

	ChatChannel    = "c"  // rw
	HistoryChannel = "h"  // rw
	UploadChannel  = "up" // w

	PingChannel = "p" // rw

	MusicChannel         = "m"  // rw
	MusicStateChannel    = ">"  // r
	MusicSongChannel     = "<"  // r
	MusicPlaylistChannel = "-"  // r
	MusicErrorChannel    = "mr" // r
	MusicNodeChannel     = "mn" // rw

	EOFChannel = "eof"

	UserChannel = "u" // r
)
