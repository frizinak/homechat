package musicnode

import (
	"fmt"
	"io"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/frizinak/homechat/client"
	musicdata "github.com/frizinak/homechat/server/channel/music/data"
	"github.com/frizinak/libym/collection"
	"github.com/frizinak/libym/player"
	"github.com/frizinak/libym/youtube"
)

const bigdiff = time.Second * 3

type Handler struct {
	client.Handler
	log client.Logger

	sem         sync.Mutex
	downloading bool

	cl *client.Client

	col *collection.Collection
	q   *collection.Queue
	p   *player.Player

	maxDelay time.Duration

	seek           chan struct{}
	lastS          collection.Song
	lastStateStamp time.Time
	lastState      client.MusicState
	volume         float64

	paused    bool
	latencies latencies
}

type latencies struct {
	max int
	l   []time.Duration

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
	handler client.Handler,
	log client.Logger,
	maxDelay time.Duration,
	col *collection.Collection,
	q *collection.Queue,
	p *player.Player,
) *Handler {
	h := &Handler{
		Handler:   handler,
		log:       log,
		cl:        cl,
		col:       col,
		q:         q,
		p:         p,
		maxDelay:  maxDelay,
		seek:      make(chan struct{}, 1024),
		latencies: latencies{max: 10, l: make([]time.Duration, 0, 30)},
	}

	h.volume = p.Volume()
	go h.ContinuousSeek()
	return h
}

func (h *Handler) IncreaseVolume(v float64) {
	h.p.IncreaseVolume(v)
	h.volume = h.p.Volume()
	h.lastState.Volume = h.volume
	if err := h.Handler.HandleMusicStateMessage(h.lastState); err != nil {
		h.log.Err(err.Error())
	}
}

func (h *Handler) ContinuousSeek() {
	var lastPos time.Duration
	var lastSong string
	var lastStamp time.Time

	const defaultIV = 2
	iv := defaultIV
	n := iv

	for range h.seek {
		state, now := h.lastState, h.lastStateStamp
		if now == lastStamp || state.Position == lastPos {
			continue
		}

		lastStamp = now
		lastPos = state.Position

		pos := h.p.Position()
		actual := state.Position + h.latencies.latency/2 //+ time.Since(now)
		d := actual - pos
		since := time.Since(now)
		if d+since > bigdiff || d+since < -bigdiff {
			h.p.Seek(actual+since, io.SeekStart)
			h.log.Flash(fmt.Sprintf("Out of sync by %s", d.Round(time.Millisecond)), time.Second)
		}

		n--
		name := h.lastState.NS + h.lastState.ID
		if n != 0 && lastSong == name {
			continue
		}
		lastSong = name
		n = iv

		h.log.Flash(
			fmt.Sprintf(
				"d:%s b:%s",
				d.Round(time.Millisecond),
				(d+since).Round(time.Millisecond),
			),
			time.Second,
		)

		if d+since > h.maxDelay || d+since < -h.maxDelay {
			iv = defaultIV
			add := time.Second
			if d > h.maxDelay {
				add = time.Second
			}
			actualS := (actual / time.Second) * time.Second
			for time.Second-(actual-actualS+time.Since(now)) > time.Millisecond {
				time.Sleep(time.Millisecond)
			}
			h.p.Seek(actualS+add, io.SeekStart)
			h.log.Flash(fmt.Sprintf("Out of sync by %s", d.Round(time.Millisecond)), time.Second)
			continue
		}

		if iv < 60 {
			iv += defaultIV
		}
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

func (h *Handler) HandleMusicStateMessage(state client.MusicState) error {
	h.lastState, h.lastStateStamp = state, time.Now()
	state.Volume = h.volume
	if err := h.Handler.HandleMusicStateMessage(state); err != nil {
		return err
	}

	if state.NS == "" && state.ID == "" && state.Title == "" {
		return nil
	}

	s, inQueue, err := h.song(state)
	if err != nil {
		return err
	}

	if !s.Local() {
		h.log.Flash(fmt.Sprintf("Song not downloaded yet: %s", s.GlobalID()), time.Second*5)
		h.p.Pause()
		h.paused = true
		h.sem.Lock()
		defer h.sem.Unlock()
		if !h.downloading {
			h.downloading = true
			h.log.Flash(fmt.Sprintf("Downloading: %s", s.GlobalID()), time.Second*5)
			return h.cl.MusicDownload(state.NS, state.ID)
		}

		return nil
	}

	h.seek <- struct{}{}

	if state.Paused && h.paused {
		return nil
	}

	if !inQueue {
		h.q.Reset()
		h.col.QueueSong(s)
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
	if err := h.Handler.HandleMusicNodeMessage(m); err != nil {
		return err
	}

	defer func() {
		h.sem.Lock()
		h.downloading = false
		h.sem.Unlock()
	}()

	if !m.Available {
		h.log.Flash("Download not available on server", time.Second*5)
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

	h.log.Flash(fmt.Sprintf("Downloaded: %s", m.Song().GlobalID()), time.Second*5)
	return os.Rename(tmp, path)
}

func (h *Handler) HandleLatency(d time.Duration) {
	h.Handler.HandleLatency(d)
	h.latencies.Add(d)
	h.latencies.Median()
}
