package musicnode

import (
	"fmt"
	"io"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/frizinak/homechat/client"
	chatdata "github.com/frizinak/homechat/server/channel/chat/data"
	musicdata "github.com/frizinak/homechat/server/channel/music/data"
	usersdata "github.com/frizinak/homechat/server/channel/users/data"
	"github.com/frizinak/libym/collection"
	"github.com/frizinak/libym/player"
	"github.com/frizinak/libym/youtube"
)

type Handler struct {
	sem         sync.Mutex
	downloading bool

	cl *client.Client

	col *collection.Collection
	q   *collection.Queue
	p   *player.Player

	maxDelay time.Duration

	lastS     collection.Song
	lastPos   time.Duration
	paused    bool
	latencies latencies
}

type latencies struct {
	max int
	l   []time.Duration

	other   time.Duration
	latency time.Duration
}

func (l latencies) Add(lat time.Duration) {
	if len(l.l) == l.max {
		l.l = l.l[1:]
	}
	l.l = append(l.l, lat)
}

func (l latencies) Median() {
	s := make(slatencies, len(l.l))
	copy(s, l.l)
	l.latency = s.Median()
}

type slatencies []time.Duration

func (l slatencies) Len() int           { return len(l) }
func (l slatencies) Swap(i, j int)      { l[i], l[j] = l[j], l[i] }
func (l slatencies) Less(i, j int) bool { return l[i] < l[j] }
func (l slatencies) Median() time.Duration {
	sort.Sort(l)
	if len(l) > 0 {
		return l[len(l)/2]
	}
	return 0
}

func New(
	cl *client.Client,
	maxDelay time.Duration,
	col *collection.Collection,
	q *collection.Queue,
	p *player.Player,
) *Handler {
	return &Handler{
		cl:        cl,
		col:       col,
		q:         q,
		p:         p,
		maxDelay:  maxDelay,
		latencies: latencies{max: 300, l: make([]time.Duration, 0, 30)},
	}
}

func (h *Handler) song(state client.MusicState) (collection.Song, bool, error) {
	var s collection.Song
	c := h.q.Current()
	if c != nil && c.Song != nil {
		s = c.Song
	}
	if s != nil && s.NS() == state.NS && s.ID() == state.ID {
		return s, true, nil
	}

	switch state.NS {
	case collection.NSYoutube:
		r := youtube.NewResult(state.ID, state.Title)
		s = h.col.FromYoutube(r)
		return s, false, nil
	default:
		return s, false, fmt.Errorf("unsupported song ns %s", state.NS)
	}
}

const bigdiff = time.Second * 2

func (h *Handler) HandleMusicStateMessage(state client.MusicState) error {
	s, inQueue, err := h.song(state)
	if err != nil {
		return err
	}

	if !s.Local() {
		h.p.Pause()
		h.paused = true
		h.sem.Lock()
		defer h.sem.Unlock()
		if !h.downloading {
			h.downloading = true
			return h.cl.MusicDownload(state.NS, state.ID)
		}

		return nil
	}

	if !inQueue {
		h.q.Reset()
		h.col.QueueSong(s)
	}

	if state.Paused && h.paused {
		return nil
	}

	if state.Position != h.lastPos {
		h.lastPos = state.Position
		pos := h.p.Position()
		actual := state.Position + h.latencies.latency/2 - h.latencies.other
		d := actual - pos

		if d > bigdiff {
			h.p.Seek(actual, io.SeekStart)
		} else if d > h.maxDelay {
			go func() {
				actualS := (actual / time.Second) * time.Second
				<-time.After(actual - actualS)
				h.p.Seek(actualS, io.SeekStart)
			}()
		} else if d < -bigdiff {
			h.p.Seek(actual, io.SeekStart)
		}
	}

	if state.Paused && !h.paused {
		h.p.Pause()
		h.paused = true
		return nil
	} else if !state.Paused && h.paused {
		h.p.Play()
		h.paused = false
	}

	if s == h.lastS {
		return nil
	}

	h.p.ForcePlay()
	h.lastS = s
	return nil
}

func (h *Handler) HandleMusicNodeMessage(m musicdata.SongDataMessage) error {
	defer func() {
		h.sem.Lock()
		h.downloading = false
		h.sem.Unlock()
	}()

	if !m.Available {
		return nil
	}

	path := h.col.SongPath(m.Song())
	tmp := collection.TempFile(path)
	err := func() error {
		f, err := os.Create(tmp)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(f, m.Upload())
		return err
	}()
	if err != nil {
		return err
	}

	fmt.Println("Downloaded", path)
	return os.Rename(tmp, path)
}

func (h *Handler) HandleLatency(d time.Duration) {
	h.latencies.Add(d)
	h.latencies.Median()
}

func (h *Handler) HandleName(name string)                                         {}
func (h *Handler) HandleHistory()                                                 {}
func (h *Handler) HandleChatMessage(chatdata.ServerMessage) error                 { return nil }
func (h *Handler) HandleMusicMessage(musicdata.ServerMessage) error               { return nil }
func (h *Handler) HandleUsersMessage(usersdata.ServerMessage, client.Users) error { return nil }
