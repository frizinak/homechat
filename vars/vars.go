package vars

const (
	Version         = "0.1.3"
	ProtocolVersion = "1009"

	ChatChannel    = "c"  // rw
	HistoryChannel = "h"  // w
	UploadChannel  = "up" // w

	PingChannel = "p" // w

	MusicChannel      = "m"  // rw
	MusicStateChannel = "ms" // r
	MusicErrorChannel = "mr" // r

	UserChannel = "u" // r
)
