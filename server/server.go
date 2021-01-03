package server

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/frizinak/binary"
	"github.com/frizinak/gotls/simplehttp"
	"github.com/frizinak/homechat/bandwidth"
	"github.com/frizinak/homechat/server/channel"
	"golang.org/x/net/websocket"
)

var fileRE = regexp.MustCompile(`(?i)[^a-z0-9\-_.]+`)
var errProto = errors.New("unsupported protocol version")

type writeJob struct {
	ch string
	c  *client
	m  []channel.Msg
}

type Config struct {
	// Arbitrary string clients will need to be able to connect
	ProtocolVersion string

	Log *log.Logger

	// HTTP/WS listen address
	HTTPAddress string
	// TCP listen address
	TCPAddress string

	// Path to store databases
	StorePath string

	// Path to store uploads and from which the http server can serve files
	UploadsPath   string
	MaxUploadSize int64

	// HTTPS certs, leave empty to disable TLS
	Cert    []byte
	CertKey []byte

	Router simplehttp.Router

	// Interval to log bandwidth, 0 = no logging
	LogBandwidth time.Duration
}

type Server struct {
	c   Config
	tls bool
	s   *simplehttp.Server
	ws  websocket.Server
	tcp net.Listener

	saveMutex sync.Mutex

	clientsMutex sync.RWMutex
	clients      map[string]map[string][]*client

	workers  int
	outgoing chan writeJob

	channels map[string]channel.Channel

	onUserUpdate channel.UserUpdateHandler

	bw Bandwidth
}

func New(c Config) (*Server, error) {
	const workers = 8
	s := &Server{
		c:        c,
		channels: make(map[string]channel.Channel),

		workers:  workers,
		outgoing: make(chan writeJob, workers),

		clients: make(map[string]map[string][]*client),

		bw: &NoopBandwidth{},
	}

	if c.LogBandwidth != 0 {
		s.bw = bandwidth.New()
	}

	s.ws = websocket.Server{Handler: s.onWS}

	var tlsConf *tls.Config
	if c.Cert != nil {
		s.tls = true
		tlsConf = &tls.Config{}
		tlsConf.Certificates = make([]tls.Certificate, 1)
		var err error
		tlsConf.Certificates[0], err = tls.X509KeyPair(c.Cert, c.CertKey)
		if err != nil {
			return nil, err
		}
	}

	nilLogger := log.New(ioutil.Discard, "", 0)
	hs := &http.Server{TLSConfig: tlsConf, ErrorLog: nilLogger}
	s.s = simplehttp.FromHTTPServer(hs, s.route, s.c.Log)

	return s, nil
}

func (s *Server) Init() error {
	if err := s.load(); err != nil {
		return err
	}

	go func() {
		for {
			time.Sleep(time.Second * 5)
			s.saveMutex.Lock()
			if err := s.save(); err != nil {
				s.c.Log.Println("ERR saving ", err)
			}
			s.saveMutex.Unlock()
		}
	}()

	go func() {
		if s.c.LogBandwidth == 0 {
			return
		}
		for {
			time.Sleep(s.c.LogBandwidth)
			_up, _down := s.bw.Get()
			up, down := NewBytes(_up, B).Human(), NewBytes(_down, B).Human()
			s.c.Log.Printf("Bandwidth: up:%s down:%s", up, down)
		}
	}()

	for i := 0; i < s.workers; i++ {
		go func() {
			for j := range s.outgoing {
				for _, m := range j.m {
					if err := j.c.msg(j.ch, m); err != nil {
						s.c.Log.Printf("ERR send to '%s': %s", j.c.name, err)
						break
					}
				}
			}
		}()
	}

	return nil
}

func (s *Server) jobs(b channel.Batch) ([]writeJob, error) {
	f, msg := b.Filter, b.Msg
	j := make([]writeJob, 0)
	if f.Channel == "" {
		return nil, errors.New("broadcast requires a channel filter for now")
	}

	h, ok := s.clients[f.Channel]
	if !ok {
		return nil, nil
	}

	clients := make([]*client, 0)
	for n, c := range h {
		if !f.CheckName(n) {
			continue
		}

		for _, cl := range c {
			if !f.CheckChannels(cl.channels) {
				continue
			}
			if !f.CheckIdentity(cl) {
				continue
			}
			clients = append(clients, cl)
		}
	}

	if len(clients) == 0 {
		return j, nil
	}

	for _, c := range clients {
		j = append(j, writeJob{f.Channel, c, []channel.Msg{msg}})
	}

	return j, nil
}

func (s *Server) BroadcastBatch(b []channel.Batch) error {
	s.clientsMutex.RLock()
	defer s.clientsMutex.RUnlock()

	var gerr error
	jobs := make([]writeJob, 0)
	for _, bat := range b {
		j, err := s.jobs(bat)
		if err != nil {
			gerr = err
			continue
		}

		jobs = append(jobs, j...)
	}

	not := make(map[int]struct{})
	for i := range jobs {
		for j := i + 1; j < len(jobs); j++ {
			if jobs[i].ch == jobs[j].ch && jobs[i].c == jobs[j].c {
				not[j] = struct{}{}
				jobs[i].m = append(jobs[i].m, jobs[j].m...)
			}
		}
	}

	for i, j := range jobs {
		if _, ok := not[i]; ok {
			continue
		}
		s.outgoing <- j
	}

	return gerr
}

func (s *Server) Broadcast(f channel.ClientFilter, m channel.Msg) error {
	return s.BroadcastBatch([]channel.Batch{channel.Batch{f, m}})
}

func (s *Server) MustSetUserUpdateHandler(h channel.UserUpdateHandler) {
	if err := s.SetUserUpdateHandler(h); err != nil {
		panic(err)
	}
}

func (s *Server) SetUserUpdateHandler(h channel.UserUpdateHandler) error {
	if s.onUserUpdate != nil {
		return errors.New("already set")
	}
	s.onUserUpdate = h
	return nil
}

func (s *Server) MustAddChannel(name string, c channel.Channel) {
	if err := s.AddChannel(name, c); err != nil {
		panic(err)
	}
}

func (s *Server) AddChannel(name string, c channel.Channel) error {
	if _, ok := s.channels[name]; ok {
		return fmt.Errorf("already have a channel for '%s'", name)
	}
	if err := c.Register(name, s); err != nil {
		return err
	}

	s.channels[name] = c
	return nil
}

func (s *Server) GetUsers(ch string) []channel.User {
	n := make([]channel.User, 0)
	s.clientsMutex.RLock()
	defer s.clientsMutex.RUnlock()
	if _, ok := s.clients[ch]; !ok {
		return n
	}

	for name, cs := range s.clients[ch] {
		if len(cs) == 0 {
			continue
		}
		n = append(n, channel.User{Name: name, Clients: len(cs)})
	}
	return n
}

func (s *Server) save() error {
	errs := make([]string, 0)
	for i, c := range s.channels {
		if !c.NeedsSave() {
			continue
		}
		f := fmt.Sprintf("%s-%s", s.c.StorePath, i)
		tmp := f + ".tmp"
		if err := c.Save(f); err != nil {
			errs = append(errs, fmt.Sprintf("Save error: %s: %s", i, err))
			continue
		}
		_, err := os.Stat(tmp)
		if os.IsNotExist(err) {
			continue
		}
		if err := os.Rename(tmp, f); err != nil {
			errs = append(errs, fmt.Sprintf("Save error: %s: %s", i, err))
		}
	}

	if len(errs) == 0 {
		return nil
	}

	return errors.New(strings.Join(errs, "\n"))
}

func (s *Server) load() error {
	for i, c := range s.channels {
		f := fmt.Sprintf("%s-%s", s.c.StorePath, i)
		if err := c.Load(f); err != nil {
			return err
		}
	}

	return nil
}

func (s *Server) route(r *http.Request, l *log.Logger) (simplehttp.HandleFunc, int) {
	p := "/" + strings.TrimLeft(r.URL.Path, "/")
	switch p {
	case "/ws":
		return s.handleWS, 0
	case "/upload":
		return s.handleUpload, 0
	}

	if strings.HasPrefix(p, "/f/") {
		return func(w http.ResponseWriter, r *http.Request, l *log.Logger) (int, error) {
			file := fileRE.ReplaceAllString(p[3:], "-")
			file = filepath.Join(s.c.UploadsPath, file)
			http.ServeFile(w, r, file)
			return 0, nil
		}, 0
	}

	return s.c.Router(r, l)
}

func (s *Server) Upload(filename string, r io.Reader) (*url.URL, error) {
	if filename == "" {
		filename = "file"
	}
	fn := fileRE.ReplaceAllString(filepath.Base(filename), "-")
	inp := bytes.NewBuffer(nil)
	inp.WriteString(strconv.FormatInt(time.Now().UnixNano(), 10))
	inp.WriteString(fn)
	hsh := fnv.New64()
	hsh.Write(inp.Bytes())
	h := hsh.Sum(nil)
	hstr := base64.RawURLEncoding.EncodeToString(h[:])

	webfile := fmt.Sprintf("%s/%s", hstr, fn)
	fn = fmt.Sprintf("%s-%s", hstr, fn)

	dst := filepath.Join(s.c.UploadsPath, fn)
	tmp := dst + ".__temp"
	f, err := os.Create(tmp)
	if err != nil {
		return nil, fmt.Errorf("ERR upload create: %w", err)
	}

	if _, err := f.ReadFrom(r); err != nil {
		f.Close()
		return nil, fmt.Errorf("ERR upload write: %w", err)
	}

	f.Close()
	if err := os.Rename(tmp, dst); err != nil {
		return nil, fmt.Errorf("ERR upload rename; %w", err)
	}

	scheme := "http"
	if s.tls {
		scheme = "https"
	}
	u, err := url.Parse(fmt.Sprintf("%s://%s", scheme, s.c.HTTPAddress))
	if err != nil {
		return nil, err
	}

	u.Path = fmt.Sprintf("/f/%s", webfile)
	return u, nil
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request, l *log.Logger) (int, error) {
	if r.Method != "POST" {
		return http.StatusMethodNotAllowed, nil
	}
	enc := json.NewEncoder(w)
	doErr := func(err error) {
		enc.Encode(map[string]string{"err": err.Error()})
	}

	r.Body = http.MaxBytesReader(w, r.Body, s.c.MaxUploadSize)
	defer r.Body.Close()
	err := r.ParseMultipartForm(1024)
	if err != nil {
		doErr(errors.New("failed to parse form"))
		l.Println(err)
		return 0, nil
	}
	file, h, err := r.FormFile("file")
	if err != nil {
		if err == http.ErrMissingFile {
			doErr(errors.New("no file uploaded"))
			return 0, nil
		}
		doErr(errors.New("something went wrong"))
		l.Println(err)
		return 0, nil
	}
	defer file.Close()

	uri, err := s.Upload(h.Filename, file)
	if err != nil {
		doErr(err)
		return 0, nil
	}

	return 0, enc.Encode(map[string]string{"uri": uri.String()})
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request, l *log.Logger) (int, error) {
	if gz, ok := w.(*simplehttp.GZIPWriter); ok {
		w = gz.ResponseWriter
	}

	s.ws.ServeHTTP(w, r)
	return 0, nil
}

func (s *Server) unsetClient(c *client) {
	s.clientsMutex.Lock()
	for _, h := range c.channels {
		ix := -1
		if _, ok := s.clients[h]; !ok {
			continue
		}

		for i, cl := range s.clients[h][c.name] {
			if cl == c {
				ix = i
				break
			}
		}
		if ix < 0 {
			continue
		}

		s.clients[h][c.name] = append(
			s.clients[h][c.name][:ix],
			s.clients[h][c.name][ix+1:]...,
		)

		l := len(s.clients[h][c.name])
		s.c.Log.Printf("remove client '%s[%d]'  for '%s'", c.name, l, h)
	}
	s.clientsMutex.Unlock()

	go func() {
		if err := s.onUserUpdate.UserUpdate(c, channel.Disconnect); err != nil {
			s.c.Log.Printf("userupdate handler: %s", err)
		}
	}()
}

func (s *Server) setClient(c *client) {
	s.clientsMutex.Lock()
	for _, h := range c.channels {
		if _, ok := s.clients[h]; !ok {
			s.clients[h] = make(map[string][]*client)
		}
		if _, ok := s.clients[h][c.name]; !ok {
			s.clients[h][c.name] = make([]*client, 0, 1)
		}

		s.clients[h][c.name] = append(s.clients[h][c.name], c)
	}
	s.c.Log.Printf(
		"new client '%s' proto:%d frame:%t for '%s'",
		c.name,
		c.proto,
		c.frameWriter,
		strings.Join(c.channels, ", "),
	)

	s.clientsMutex.Unlock()
	go func() {
		if err := s.onUserUpdate.UserUpdate(c, channel.Connect); err != nil {
			s.c.Log.Printf("userupdate handler: %s", err)
		}
	}()
}

func (s *Server) newClient(proto channel.Proto, frameWriter bool, id channel.IdentifyMsg, conn io.Writer) (*client, error) {
	for _, h := range id.Channels {
		if _, ok := s.channels[h]; !ok {
			return nil, fmt.Errorf("invalid channel subscribe: %s", h)
		}
	}

	if id.Version != s.c.ProtocolVersion {
		return nil, errProto
	}

	name := strings.Join(strings.Fields(id.Data), "")
	if name == "" {
		return nil, errors.New("invalid name")
	}

	for _, h := range id.Channels {
		if _, ok := s.channels[h]; !ok {
			return nil, fmt.Errorf("invalid channel subscribe: %s", h)
		}
	}

	return &client{
		w:           conn,
		frameWriter: frameWriter,
		proto:       proto,
		name:        name,
		channels:    id.Channels,
		last:        make(map[string]channel.Msg),
	}, nil
}

func (s *Server) handleConn(proto channel.Proto, conn net.Conn, frameWriter bool) error {
	identify := func(r io.Reader) (channel.IdentifyMsg, io.Reader, error) {
		m, err := channel.BinaryIdentifyMsg(binary.NewReader(r))
		return m, r, err
	}
	getChannel := func(r io.Reader) (channel.ChannelMsg, io.Reader, error) {
		m, err := channel.BinaryChannelMsg(binary.NewReader(r))
		return m, r, err
	}
	do := func(r io.Reader, cl *client, h channel.Channel) (io.Reader, error) {
		return r, h.HandleBIN(cl, binary.NewReader(r))
	}
	write := func(w io.Writer, m channel.Msg) error {
		return m.Binary(binary.NewWriter(w))
	}

	if proto == channel.ProtoJSON {
		identify = func(r io.Reader) (channel.IdentifyMsg, io.Reader, error) {
			return channel.JSONIdentifyMsg(r)
		}
		getChannel = func(r io.Reader) (channel.ChannelMsg, io.Reader, error) {
			return channel.JSONChannelMsg(r)
		}
		do = func(r io.Reader, cl *client, h channel.Channel) (io.Reader, error) {
			return h.HandleJSON(cl, r)
		}
		write = func(w io.Writer, m channel.Msg) error {
			b := bytes.NewBuffer(nil)
			if err := m.JSON(b); err != nil {
				return err
			}
			_, err := w.Write(b.Bytes())
			return err
		}
	}

	reader := s.bw.NewReader(conn)
	writer := s.bw.NewWriter(conn)

	const jsonMax = 1024 * 1024 * 20
	limited := &io.LimitedReader{R: reader, N: 1024}
	id, r, err := identify(limited)
	if err != nil {
		return fmt.Errorf("identify: %w", err)
	}

	c, err := s.newClient(proto, frameWriter, id, writer)
	status := channel.StatusMsg{Code: channel.StatusOK, Err: ""}
	if err != nil {
		status.Code = channel.StatusNOK
		status.Err = err.Error()
		if err == errProto {
			status.Code = channel.StatusUpdateClient
		}
	}

	if err := write(writer, status); err != nil {
		return err
	}
	if err != nil {
		return err
	}
	if err := write(writer, channel.IdentifyMsg{Data: c.name}); err != nil {
		return err
	}
	s.setClient(c)
	defer s.unsetClient(c)

	var chnl channel.ChannelMsg
	for {
		if err := conn.SetDeadline(time.Now().Add(time.Second * 120)); err != nil {
			return err
		}

		limited.N = 255
		chnl, r, err = getChannel(r)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("channel specify: %w", err)
		}

		h, ok := s.channels[chnl.Data]
		if !ok {
			return fmt.Errorf("impossible channel '%s'", chnl.Data)
		}

		limited.N = h.LimitReader()
		if proto != channel.ProtoBinary && limited.N > jsonMax {
			limited.N = jsonMax
		}
		r, err = do(r, c, h)
		if err != nil {
			return fmt.Errorf("channel %s: %w", chnl.Data, err)
		}
	}
}

func (s *Server) onWS(conn *websocket.Conn) {
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(time.Second * 5)); err != nil {
		return
	}

	proto := channel.ReadProto(conn)

	switch proto {
	case channel.ProtoJSON, channel.ProtoBinary:
		if err := s.handleConn(proto, conn, true); err != nil {
			s.c.Log.Printf("client error: %s", err)
		}
	default:
		s.c.Log.Printf("client requested invalid protocol: %d", proto)
		return
	}
}

func (s *Server) onTCP(conn net.Conn) {
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(time.Second * 5)); err != nil {
		return
	}

	proto := channel.ReadProto(conn)

	switch proto {
	case channel.ProtoJSON, channel.ProtoBinary:
		if err := s.handleConn(proto, conn, false); err != nil {
			s.c.Log.Printf("client error: %s", err)
		}
	default:
		s.c.Log.Printf("client requested invalid protocol: %d", proto)
		return
	}
}

func (s *Server) Run() error {
	var err error
	s.tcp, err = net.Listen("tcp", s.c.TCPAddress)
	if err != nil {
		return fmt.Errorf("could not open tcp connection %s: %w", s.c.TCPAddress, err)
	}
	go func() {
		for {
			conn, err := s.tcp.Accept()
			if err != nil {
				s.c.Log.Println("tcp err:", err)
				continue
			}

			go s.onTCP(conn)
		}
	}()

	err = s.s.Start(s.c.HTTPAddress, s.tls)
	s.tcp.Close()
	return err
}
