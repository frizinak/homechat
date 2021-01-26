package music

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/frizinak/homechat/client"
	musicdata "github.com/frizinak/homechat/server/channel/music/data"
	"github.com/frizinak/libym/collection"
)

type DownloadHandler struct {
	client.Handler
	log          client.Logger
	col          *collection.Collection
	cl           *client.Client
	can          chan struct{}
	lastResponse time.Time
}

func NewDownloadHandler(
	handler client.Handler,
	log client.Logger,
	col *collection.Collection,
	cl *client.Client,
) *DownloadHandler {
	d := &DownloadHandler{
		Handler: handler,
		log:     log,
		col:     col,
		cl:      cl,
		can:     make(chan struct{}),
	}

	go d.loop()
	return d
}

func (h *DownloadHandler) loop() {
	for {
		if time.Since(h.lastResponse) > time.Second {
			h.can <- struct{}{}
		}
		time.Sleep(time.Millisecond * 200)
	}
}

func (h *DownloadHandler) newdl(to time.Duration) bool {
	select {
	case <-h.can:
		h.lastResponse = time.Now()
		return true
	case <-time.After(to):
		return false
	}
}

func (h *DownloadHandler) DownloadSong(ns, id string, to time.Duration) error {
	if h.newdl(to) {
		return h.cl.MusicSongDownload(ns, id)
	}
	return nil
}

func (h *DownloadHandler) DownloadPlaylist(playlist string, to time.Duration) error {
	if h.newdl(to) {
		fmt.Printf("send %s\n", playlist)
		return h.cl.MusicPlaylistDownload(playlist)
	}
	return nil
}

func (h *DownloadHandler) Wait() {
	<-h.can
	select {
	case h.can <- struct{}{}:
	default:
	}
}

func (h *DownloadHandler) HandleMusicNodeMessage(m musicdata.SongDataMessage) error {
	h.lastResponse = time.Now()
	if h.Handler != nil {
		if err := h.Handler.HandleMusicNodeMessage(m); err != nil {
			return err
		}
	}

	if !m.Available {
		h.log.Flash("Download not available on server", time.Second*5)
		return nil
	}

	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case <-time.After(time.Millisecond * 200):
				h.lastResponse = time.Now()
			}
		}
	}()
	defer func() { done <- struct{}{} }()

	path := h.col.SongPath(m.Song)
	tmp := collection.TempFile(path)
	err := func() error {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
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

	h.log.Flash(fmt.Sprintf("Downloaded: %s", collection.GlobalID(m.Song)), time.Second*5)
	return os.Rename(tmp, path)
}
