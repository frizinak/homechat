package vars

const (
	Version         = "0.1.7"
	ProtocolVersion = "1014"

	ChatChannel    = "c"  // rw
	HistoryChannel = "h"  // rw
	UploadChannel  = "up" // w

	PingChannel = "p" // w

	MusicChannel         = "m"  // rw
	MusicStateChannel    = ">"  // r
	MusicSongChannel     = "<"  // r
	MusicPlaylistChannel = "-"  // r
	MusicErrorChannel    = "mr" // r

	UserChannel = "u" // r
)
