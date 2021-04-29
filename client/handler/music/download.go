package music

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/frizinak/homechat/client"
	musicdata "github.com/frizinak/homechat/server/channel/music/data"
	"github.com/frizinak/libym/collection"
	"github.com/frizinak/libym/youtube"
)

type DownloadHandler struct {
	client.Handler
	wg  sync.WaitGroup
	log client.Logger
	col *collection.Collection
	cl  *client.Client
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
	}

	return d
}

func (h *DownloadHandler) Wait() {
	h.wg.Wait()
}

func (h *DownloadHandler) DownloadSong(ns, id string, to time.Duration) error {
	h.wg.Add(1)
	return h.cl.MusicSongDownload(ns, id)
}

func (h *DownloadHandler) DownloadPlaylist(playlist string, to time.Duration) error {
	h.wg.Add(1)
	return h.cl.MusicPlaylistSongs(playlist)
}

func (h *DownloadHandler) HandleMusicPlaylistSongsMessage(m musicdata.ServerPlaylistSongsMessage) error {
	defer h.wg.Done()
	for _, s := range m.List {
		switch s.NS() {
		case collection.NSYoutube:
			rs := h.col.FromYoutube(youtube.NewResult(s.ID(), s.Title()))
			if rs.Local() {
				h.log.Flash(fmt.Sprintf("We already have: %s", collection.GlobalID(s)), time.Second*5)
				continue
			}
		default:
			return fmt.Errorf("song with unknown namespace: '%s'", s.NS())
		}
		if err := h.DownloadSong(s.NS(), s.ID(), 0); err != nil {
			return err
		}
	}
	return nil
}

func (h *DownloadHandler) HandleMusicNodeMessage(m musicdata.SongDataMessage) error {
	defer h.wg.Done()

	if h.Handler != nil {
		if err := h.Handler.HandleMusicNodeMessage(m); err != nil {
			return err
		}
	}

	if !m.Available {
		h.log.Flash("Download not available on server", time.Second*5)
		return nil
	}

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
