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

	"github.com/frizinak/gotls/simplehttp"
	"github.com/frizinak/homechat/bandwidth"
	"github.com/frizinak/homechat/crypto"
	"github.com/frizinak/homechat/server/channel"
	"github.com/frizinak/homechat/server/client"
	"golang.org/x/net/websocket"
)

var (
	fileRE         = regexp.MustCompile(`(?i)[^a-z0-9\-_.]+`)
	errProto       = errors.New("unsupported protocol version")
	errKeyExchange = errors.New("client/server keys mismatch")
)

const (
	clientJobBuf = 50
	clientErrBuf = 8
	outgoingBuf  = 50
	saveInterval = time.Second * 5
)

type writeJob struct {
	c *client.Client
	client.Job
}

type Config struct {
	// Arbitrary string clients will need to be able to connect
	ProtocolVersion string

	Key *crypto.Key

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

	Router    simplehttp.Router
	RWFactory channel.RWFactory

	// Interval to log bandwidth, 0 = no logging
	LogBandwidth time.Duration
}

type Ban struct {
	expiry time.Time
}

type Server struct {
	c    Config
	tls  bool
	http *http.Server
	s    *simplehttp.Server
	ws   websocket.Server
	tcp  net.Listener

	saveMutex sync.Mutex

	clientsMutex sync.RWMutex
	clients      map[string]map[string][]*client.Client

	clientErrs chan client.Error

	outgoing chan writeJob

	channels map[string]channel.Channel

	onUserUpdate channel.UserUpdateHandler

	bw Bandwidth

	banMutex sync.RWMutex
	bans     map[string]Ban

	closing bool
}

func New(c Config) (*Server, error) {
	s := &Server{
		c:        c,
		channels: make(map[string]channel.Channel),

		outgoing: make(chan writeJob, outgoingBuf),

		clients:    make(map[string]map[string][]*client.Client),
		clientErrs: make(chan client.Error, clientErrBuf),

		bw: &NoopBandwidth{},

		bans: make(map[string]Ban),
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
	s.http = &http.Server{TLSConfig: tlsConf, ErrorLog: nilLogger}
	s.s = simplehttp.FromHTTPServer(s.http, s.route, s.c.Log)

	return s, nil
}

func (s *Server) ban(address string) {
	b := Ban{time.Now().Add(time.Second * 30)}
	s.banMutex.Lock()
	s.bans[address] = b
	s.banMutex.Unlock()
}

func (s *Server) banned(address string) (time.Duration, bool) {
	now := time.Now()
	del := false
	s.banMutex.RLock()
	if v, ok := s.bans[address]; ok {
		del = true
		if v.expiry.After(now) {
			s.banMutex.RUnlock()
			return v.expiry.Sub(now), true
		}
	}
	s.banMutex.RUnlock()

	if del {
		s.banMutex.Lock()
		delete(s.bans, address)
		s.banMutex.Unlock()
	}

	return 0, false
}

func (s *Server) banCleanup() {
	now := time.Now()
	deletes := make([]string, 0)
	s.banMutex.RLock()
	for i, v := range s.bans {
		if !v.expiry.After(now) {
			deletes = append(deletes, i)
		}
	}
	s.banMutex.RUnlock()
	if len(deletes) < 50 {
		return
	}
	s.banMutex.Lock()
	for _, i := range deletes {
		delete(s.bans, i)
	}
	s.banMutex.Unlock()
}

func (s *Server) Init() error {
	if err := s.load(); err != nil {
		return err
	}

	go func() {
		for {
			time.Sleep(saveInterval)
			if err := s.Save(); err != nil {
				s.c.Log.Println("ERR saving ", err)
			}
		}
	}()

	go func() {
		for {
			time.Sleep(time.Second)
			s.banCleanup()
		}
	}()

	go func() {
		if s.c.LogBandwidth == 0 {
			return
		}
		for {
			time.Sleep(s.c.LogBandwidth)
			_up, _down, _tup, _tdown := s.bw.Get()
			up, down := NewBytes(_up, B).Human(), NewBytes(_down, B).Human()

			tup, tdown := NewBytes(float64(_tup), B).Human(), NewBytes(float64(_tdown), B).Human()
			s.c.Log.Printf("Bandwidth: up:%s [%s/s] down:%s [%s/s]", tup, up, tdown, down)
		}
	}()

	go func() {
		for j := range s.outgoing {
			j.c.Queue(j.Job)
		}
	}()

	go func() {
		for err := range s.clientErrs {
			s.c.Log.Printf("ERR send to '%s': %s", err.Client.Name(), err.Err)
			break
		}
	}()

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

	clients := make([]*client.Client, 0)
	for n, c := range h {
		if !f.CheckName(n) {
			continue
		}

		for _, cl := range c {
			if !f.CheckChannels(cl.Channels()) {
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
		j = append(j, writeJob{c, client.Job{f.Channel, []channel.Msg{msg}}})
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
			if jobs[i].Channel == jobs[j].Channel && jobs[i].c == jobs[j].c {
				not[j] = struct{}{}
				jobs[i].Msgs = append(jobs[i].Msgs, jobs[j].Msgs...)
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
	return s.BroadcastBatch([]channel.Batch{{f, m}})
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

func (s *Server) Save() error {
	s.saveMutex.Lock()
	err := s.save()
	s.saveMutex.Unlock()
	return err
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
	hstr := base64.RawURLEncoding.EncodeToString(h)

	webfile := fmt.Sprintf("%s/%s", hstr, fn)
	fn = fmt.Sprintf("%s-%s", hstr, fn)

	dst := filepath.Join(s.c.UploadsPath, fn)
	tmp := dst + ".__temp"
	f, err := os.Create(tmp)
	if err != nil {
		return nil, fmt.Errorf("ERR upload create: %w", err)
	}

	if _, err = f.ReadFrom(r); err != nil {
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

func (s *Server) unsetClient(c *client.Client) {
	s.clientsMutex.Lock()
	c.Stop()
	for _, h := range c.Channels() {
		ix := -1
		if _, ok := s.clients[h]; !ok {
			continue
		}

		name := c.Name()
		for i, cl := range s.clients[h][c.Name()] {
			if cl == c {
				ix = i
				break
			}
		}
		if ix < 0 {
			continue
		}

		s.clients[h][name] = append(
			s.clients[h][name][:ix],
			s.clients[h][name][ix+1:]...,
		)

		l := len(s.clients[h][name])
		s.c.Log.Printf("remove client '%s[%d]'  for '%s'", name, l, h)
	}
	s.clientsMutex.Unlock()

	go func() {
		if err := s.onUserUpdate.UserUpdate(c, channel.Disconnect); err != nil {
			s.c.Log.Printf("userupdate handler: %s", err)
		}
	}()
}

func (s *Server) setClient(conf client.Config, c *client.Client) {
	s.clientsMutex.Lock()
	for _, h := range conf.Channels {
		if _, ok := s.clients[h]; !ok {
			s.clients[h] = make(map[string][]*client.Client)
		}

		if _, ok := s.clients[h][conf.Name]; !ok {
			s.clients[h][conf.Name] = make([]*client.Client, 0, 1)
		}

		s.clients[h][conf.Name] = append(s.clients[h][conf.Name], c)
	}
	s.c.Log.Printf(
		"new client '%s' proto:%d frame:%t for '%s'",
		conf.Name,
		conf.Proto,
		conf.FrameWriter,
		strings.Join(conf.Channels, ", "),
	)

	c.Run()

	s.clientsMutex.Unlock()
	go func() {
		if err := s.onUserUpdate.UserUpdate(c, channel.Connect); err != nil {
			s.c.Log.Printf("userupdate handler: %s", err)
		}
	}()
}

func (s *Server) newClient(
	proto channel.Proto,
	frameWriter bool,
	id channel.IdentifyMsg,
	w channel.WriteFlusher,
	binW channel.BinaryWriter,
) (client.Config, *client.Client, error) {
	var conf client.Config
	for _, h := range id.Channels {
		if _, ok := s.channels[h]; !ok {
			return conf, nil, fmt.Errorf("invalid channel subscribe: %s", h)
		}
	}

	if id.Version != s.c.ProtocolVersion {
		return conf, nil, errProto
	}

	name := strings.Join(strings.Fields(id.Data), "")
	if name == "" {
		return conf, nil, errors.New("invalid name")
	}

	for _, h := range id.Channels {
		if _, ok := s.channels[h]; !ok {
			return conf, nil, fmt.Errorf("invalid channel subscribe: %s", h)
		}
	}

	conf.FrameWriter = frameWriter
	conf.Proto = proto
	conf.Name = name
	conf.Channels = id.Channels
	conf.JobBuffer = clientJobBuf

	return conf, client.New(conf, w, binW, s.clientErrs), nil
}

func (s *Server) handleConn(proto channel.Proto, conn net.Conn, frameWriter bool) error {
	paddress := strings.Split(conn.RemoteAddr().String(), ":")
	if len(paddress) > 1 {
		paddress = paddress[:len(paddress)-1]
	}
	address := strings.Join(paddress, ":")
	if dur, banned := s.banned(address); banned {
		return fmt.Errorf("still banned [%s]: %s", dur, address)
	}

	read := func(r io.Reader, typ channel.Msg) (channel.Msg, io.Reader, error) {
		//m, err := typ.FromBinary(binary.NewReader(r))
		m, err := typ.FromBinary(s.c.RWFactory.BinaryReader(r))
		return m, r, err
	}
	do := func(r io.Reader, cl *client.Client, h channel.Channel) (io.Reader, error) {
		return r, h.HandleBIN(cl, s.c.RWFactory.BinaryReader(r))
	}
	write := func(w channel.WriteFlusher, m channel.Msg) error {
		if err := m.Binary(s.c.RWFactory.BinaryWriter(w)); err != nil {
			return err
		}
		return w.Flush()
	}

	if proto == channel.ProtoJSON {
		read = func(r io.Reader, typ channel.Msg) (channel.Msg, io.Reader, error) {
			return typ.FromJSON(r)
		}
		do = func(r io.Reader, cl *client.Client, h channel.Channel) (io.Reader, error) {
			return h.HandleJSON(cl, r)
		}
		write = func(w channel.WriteFlusher, m channel.Msg) error {
			if err := m.JSON(w); err != nil {
				return err
			}
			return w.Flush()
		}
	}

	reader := s.bw.NewReader(conn)
	writer := s.bw.NewWriter(conn)

	const jsonMax = 1024 * 1024 * 20
	limited := &io.LimitedReader{R: reader, N: 1024 * 10}
	reader = limited

	var writeFlusher channel.WriteFlusher = channel.NewPassthrough(writer)
	if frameWriter {
		writeFlusher = channel.NewBuffered(writer)
	}

	var msg channel.Msg

	key := s.c.Key
	server, err := channel.NewPubKeyServerMessage(key)
	if err != nil {
		return err
	}

	if err := write(writeFlusher, server); err != nil {
		return err
	}

	msg, reader, err = read(reader, channel.PubKeyMessage{})
	if err != nil {
		s.ban(address)
		return err
	}
	client := msg.(channel.PubKeyMessage)

	derive, err := channel.CommonSecret(client, server, key)
	if err != nil {
		s.ban(address)
		return err
	}

	encryptedRW := crypto.NewEncDec(
		reader,
		writeFlusher,
		derive(channel.CryptoServerRead),
		derive(channel.CryptoServerWrite),
		crypto.EncrypterConfig{SaltSize: 32, Cost: 15},
		crypto.DecrypterConfig{MinSaltSize: 16, MinCost: 12},
	)

	writer = s.c.RWFactory.Writer(encryptedRW)
	reader = s.c.RWFactory.Reader(encryptedRW)
	writeFlusher = &channel.WriterFlusher{writer, writeFlusher}

	test, err := channel.NewSymmetricTestMessage()
	if err != nil {
		return err
	}

	if err := write(writeFlusher, test); err != nil {
		return err
	}

	limited.N = 1024 * 10
	msg, reader, err = read(reader, channel.SymmetricTestMessage{})
	if err != nil {
		s.ban(address)
		if err == channel.ErrKeyExchange {
			return errKeyExchange
		}

		return err
	}

	if !test.Equal(msg) {
		s.ban(address)
		return errKeyExchange
	}

	msg, reader, err = read(reader, channel.IdentifyMsg{})
	if err != nil {
		return fmt.Errorf("identify: %w", err)
	}
	id := msg.(channel.IdentifyMsg)

	conf, c, err := s.newClient(proto, frameWriter, id, writeFlusher, s.c.RWFactory.BinaryWriter(writeFlusher))
	status := channel.StatusMsg{Code: channel.StatusOK}
	if err != nil {
		status.Code = channel.StatusNOK
		status.Err = err.Error()
		if err == errProto {
			status.Code = channel.StatusUpdateClient
		}
	}

	if err := write(writeFlusher, status); err != nil {
		return err
	}
	if err != nil {
		return err
	}
	if err := write(writeFlusher, channel.IdentifyMsg{Data: conf.Name}); err != nil {
		return err
	}
	s.setClient(conf, c)
	defer s.unsetClient(c)

	var chnl channel.ChannelMsg
	for {
		if s.closing {
			return nil
		}

		if err := conn.SetDeadline(time.Now().Add(time.Second * 120)); err != nil {
			return err
		}

		limited.N = 255
		msg, reader, err = read(reader, channel.ChannelMsg{})
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("channel specify: %w", err)
		}
		chnl = msg.(channel.ChannelMsg)

		h, ok := s.channels[chnl.Data]
		if !ok {
			return fmt.Errorf("impossible channel '%s'", chnl.Data)
		}

		limited.N = h.LimitReader()
		if proto != channel.ProtoBinary && limited.N > jsonMax {
			limited.N = jsonMax
		}
		reader, err = do(reader, c, h)
		if err != nil {
			return fmt.Errorf("channel %s: %w", chnl.Data, err)
		}
	}
}

func (s *Server) onWS(conn *websocket.Conn) {
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(time.Second * 10)); err != nil {
		return
	}

	proto := channel.ReadProto(conn)
	// TODO uncomment when tls lands and when/if internal encryption is disabled
	// conn.PayloadType = websocket.TextFrame
	// if proto == channel.ProtoBinary {
	conn.PayloadType = websocket.BinaryFrame
	//}

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
	if err := conn.SetDeadline(time.Now().Add(time.Second * 10)); err != nil {
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

	errs := make(chan error, 1)
	for chName, ch := range s.channels {
		go func(chName string, ch channel.Channel) {
			if err := ch.Run(); err != nil {
				errs <- fmt.Errorf("channel runtime error '%s': %w", chName, err)
			}
		}(chName, ch)
	}

	go func() {
		for err := range errs {
			s.c.Log.Println(err)
			s.closing = true
			s.Close()
		}
	}()

	go func() {
		for {
			if s.closing {
				s.tcp.Close()
				break
			}
			conn, err := s.tcp.Accept()
			if err != nil {
				s.c.Log.Println("tcp err:", err)
				continue
			}

			go s.onTCP(conn)
		}
	}()

	err = s.s.Start(s.c.HTTPAddress, s.tls)
	if s.closing {
		err = nil
	}
	s.closing = true
	strs := make([]string, 0)
	if err != nil {
		strs = append(strs, err.Error())
	}
	for _, ch := range s.channels {
		if err := ch.Close(); err != nil {
			strs = append(strs, err.Error())
		}
	}

	if len(strs) == 0 {
		return nil
	}

	return errors.New(strings.Join(strs, ", "))
}

func (s *Server) Close() {
	s.closing = true
	s.http.Close()
}
