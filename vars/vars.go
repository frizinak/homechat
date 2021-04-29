package vars

var GitVersion string

const (
	Version         = "custom"
	ProtocolVersion = "1023"

	UpdateChannel = "update" // rw

	ChatChannel    = "c"  // rw
	HistoryChannel = "h"  // rw
	UploadChannel  = "up" // w

	PingChannel = "p" // rw

	TypingChannel = "t" // rw

	MusicChannel              = "m"  // rw
	MusicStateChannel         = ">"  // r
	MusicSongChannel          = "<"  // r
	MusicPlaylistChannel      = "-"  // r
	MusicPlaylistSongsChannel = "-*" // rw
	MusicErrorChannel         = "mr" // r
	MusicNodeChannel          = "mn" // rw

	EOFChannel = "eof"

	UserChannel = "u" // r
)
