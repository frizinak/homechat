package music

import (
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/frizinak/homechat/server/channel"
	"github.com/frizinak/homechat/server/channel/music/data"
	"github.com/frizinak/homechat/server/channel/status"
	"github.com/frizinak/libym/acoustid"
	"github.com/frizinak/libym/collection"
	"github.com/frizinak/libym/di"
	"github.com/frizinak/libym/player"
	"github.com/frizinak/libym/ui"
)

type mode int

const (
	modeNone mode = iota
	modeSongs
	modeText
)

type YMChannel struct {
	sem sync.Mutex

	ym           ui.UI
	col          *collection.Collection
	problematics *collection.Problematics
	p            *player.Player

	state struct {
		mode  mode
		view  ui.View
		title string
		text  string
		songs []ui.Song
	}

	log *log.Logger

	channel string
	sender  channel.Sender

	stateCh         *StateChannel
	songCh          *SongChannel
	playlistCh      *PlaylistChannel
	playlistSongsCh *PlaylistSongsChannel
	musicNode       *MusicNodeChannel
	statusCh        *status.StatusChannel

	channel.NoSave
	channel.Limit
	channel.NoRun
}

func NewYM(
	log *log.Logger,
	status *status.StatusChannel,
	ymPath string,
	acoust acoustid.Config,
) *YMChannel {
	ym := &YMChannel{log: log, statusCh: status, Limit: channel.Limiter(1024 * 1024 * 5)}
	c := di.Config{
		StorePath: ymPath,
		AutoSave:  true,

		CustomOutput: ym,
		CustomError:  ym,

		AcoustID: acoust,
	}

	di := di.New(c)
	ym.ym = di.BaseUI()
	ym.col = di.Collection()
	ym.problematics = ym.col.Problematics()
	ym.p = di.Player()
	ym.stateCh = NewState(log, ym.p)
	ym.songCh = NewSong(log, di.Queue())
	ym.playlistCh = NewPlaylist(log, ym.col)
	ym.playlistSongsCh = NewPlaylistSongs(log, ym.col)
	ym.musicNode = NewMusicNode(log, ym.col)

	ym.LoadPlayerPosition()

	return ym
}

func (c *YMChannel) Close() error {
	return c.p.Close()
}

func (c *YMChannel) StateChannel() *StateChannel                 { return c.stateCh }
func (c *YMChannel) SongChannel() *SongChannel                   { return c.songCh }
func (c *YMChannel) PlaylistChannel() *PlaylistChannel           { return c.playlistCh }
func (c *YMChannel) PlaylistSongsChannel() *PlaylistSongsChannel { return c.playlistSongsCh }
func (c *YMChannel) NodeChannel() *MusicNodeChannel              { return c.musicNode }

func (c *YMChannel) SaveCollection() error { return c.col.Save() }

func (c *YMChannel) LoadPlayerPosition() error { return c.p.LoadPosition() }
func (c *YMChannel) SavePlayerPosition() error { return c.p.SavePosition() }

func (c *YMChannel) Register(chnl string, s channel.Sender) error {
	c.channel = chnl
	c.sender = s
	return nil
}

func (c *YMChannel) SendInterval(iv time.Duration) {
	c.ym.Input("q")
	for {
		time.Sleep(iv)
		c.ym.Refresh()
	}
}

func (c *YMChannel) StateSendInterval(iv time.Duration) {
	for {
		c.flushState()
		time.Sleep(iv)
	}
}

func (c *YMChannel) PlaylistSendInterval(iv time.Duration) {
	for {
		c.flushPlaylists()
		time.Sleep(iv)
	}
}

func (c *YMChannel) HandleBIN(cl channel.Client, r channel.BinaryReader) error {
	m, err := data.BinaryMessage(r)
	if err != nil {
		return err
	}
	return c.handle(cl, m)
}

func (c *YMChannel) HandleJSON(cl channel.Client, r io.Reader) (io.Reader, error) {
	m, nr, err := data.JSONMessage(r)
	if err != nil {
		return nr, err
	}
	return nr, c.handle(cl, m)
}

func (c *YMChannel) handle(cl channel.Client, m data.Message) error {
	if strings.TrimSpace(m.Command) == "" {
		c.Flush()
		return nil
	}
	c.ym.Input(m.Command)
	return nil
}

func (c *YMChannel) SetView(view ui.View)  { c.state.view = view }
func (c *YMChannel) SetTitle(title string) { c.state.title = title }

func (c *YMChannel) SetSongs(songs []ui.Song) {
	c.state.mode = modeSongs
	c.state.songs = songs
}

func (c *YMChannel) SetText(text string) {
	c.state.mode = modeText
	c.state.text = text
}

func (c *YMChannel) AtomicFlush(cb func(ui.AtomicOutput)) {
	c.sem.Lock()
	if cb != nil {
		cb(c)
	}
	c.flush()
	c.sem.Unlock()
}

func (c *YMChannel) Flush() {
	c.AtomicFlush(nil)
}

func (c *YMChannel) Err(err error) {
	if err := c.statusCh.Err(channel.ClientFilter{}, err); err != nil {
		c.log.Println(err)
	}
}

func (c *YMChannel) flushState() {
	c.songCh.Send()
	c.stateCh.Send()
}

func (c *YMChannel) flushPlaylists() {
	c.playlistCh.Send()
}

func (c *YMChannel) flush() {
	c.flushPlaylists()
	if c.state.mode == modeNone {
		return
	}

	s := data.ServerMessage{View: byte(c.state.view), Title: c.state.title}

	switch c.state.mode {
	case modeSongs:
		s.Songs = make([]data.Song, len(c.state.songs))
		for i, song := range c.state.songs {
			title := strings.TrimSpace(song.Title())
			extra := song.Extra()
			if title == "" {
				title = fmt.Sprintf("- no title - [%s %s]", song.NS(), song.ID())
			}
			if extra != "" {
				title += extra
			}
			s.Songs[i] = data.Song{
				song.NS(),
				song.ID(),
				title,
				song.Active(),
				c.problematics.Reason(song),
			}
		}

	case modeText:
		s.Text = c.state.text
	}

	c.send(s)
}

func (c *YMChannel) send(m data.ServerMessage) {
	f := channel.ClientFilter{Channel: c.channel}
	if err := c.sender.Broadcast(f, m); err != nil {
		c.log.Println(err)
	}
}
