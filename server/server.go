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
	"unicode"

	b "github.com/frizinak/homechat/bytes"

	"github.com/frizinak/gotls/simplehttp"
	"github.com/frizinak/homechat/crypto"
	"github.com/frizinak/homechat/server/bandwidth"
	"github.com/frizinak/homechat/server/channel"
	"github.com/frizinak/homechat/server/client"
	"github.com/frizinak/homechat/vars"
	"golang.org/x/net/websocket"
)

var (
	fileRE         = regexp.MustCompile(`(?i)[^a-z0-9\-_.]+`)
	errProto       = errors.New("unsupported protocol version")
	errKeyExchange = errors.New("client/server keys mismatch")
	errNotAllowed  = errors.New("client fingerprint mismatch")
)

const (
	clientJobBuf = 100000
	clientErrBuf = 8
	outgoingBuf  = 50
	saveInterval = time.Second * 5
)

type writeJob struct {
	c *client.Client
	client.Job
}

type ClientPolicy string

const (
	PolicyWorld ClientPolicy = "world"
	PolicyAllow ClientPolicy = "allow"
	PolicyFixed ClientPolicy = "fixed"
)

type PolicyLoader interface {
	Policy() ClientPolicy
	Exists(fingerprint string) (name string, err error)
}

type Config struct {
	// Arbitrary string clients will need to be able to connect
	ProtocolVersion string

	// PolicyLoader should return all allowed users and their names
	PolicyLoader PolicyLoader

	Key *crypto.Key

	Log *log.Logger

	// HTTP link address
	HTTPPublicAddress string
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

	bw bandwidth.Bandwidth

	closing bool
}

func New(c Config) (*Server, error) {
	s := &Server{
		c:        c,
		channels: make(map[string]channel.Channel),

		outgoing: make(chan writeJob, outgoingBuf),

		clients:    make(map[string]map[string][]*client.Client),
		clientErrs: make(chan client.Error, clientErrBuf),

		bw: &bandwidth.Noop{},
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
		if s.c.LogBandwidth == 0 {
			return
		}
		for {
			time.Sleep(s.c.LogBandwidth)
			_up, _down, _tup, _tdown := s.bw.Get()
			up, down := b.New(_up, b.B).Human(), b.New(_down, b.B).Human()

			tup, tdown := b.New(float64(_tup), b.B).Human(), b.New(float64(_tdown), b.B).Human()
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
		return nil, nil
	}

	for _, c := range clients {
		j = append(j, writeJob{c, client.Job{Channel: f.Channel, Msgs: []channel.Msg{msg}}})
	}

	return j, nil
}

func (s *Server) BroadcastBatch(b []channel.Batch) error {
	s.clientsMutex.RLock()
	defer s.clientsMutex.RUnlock()

	closes := make([]func() error, 0)

	var gerr error
	jobs := make([]writeJob, 0)
	for _, bat := range b {
		closes = append(closes, bat.Msg.Close)
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

	var _wg sync.WaitGroup
	wg := &_wg

	clean := make([]writeJob, 0, len(jobs))
	for i, j := range jobs {
		if _, ok := not[i]; ok {
			continue
		}

		wg.Add(len(j.Job.Msgs))
		clean = append(clean, j)
	}

	for _, j := range clean {
		j.Job.WG = wg
		s.outgoing <- j
	}

	go func() {
		wg.Wait()
		for _, c := range closes {
			c()
		}
	}()

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
			if s, err := os.Stat(file); err != nil || s.IsDir() {
				return http.StatusNotFound, nil
			}
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

	_, err = f.ReadFrom(r)
	f.Close()
	if err != nil {
		return nil, fmt.Errorf("ERR upload write: %w", err)
	}

	if err := os.Rename(tmp, dst); err != nil {
		return nil, fmt.Errorf("ERR upload rename; %w", err)
	}

	proto := "http://"
	if s.tls {
		proto = "https://"
	}
	re := regexp.MustCompile("(?i)^[a-z]+://")
	if re.MatchString(s.c.HTTPPublicAddress) {
		// already contains protocol
		// could be https while s.tls = false we are reverse proxied
		proto = ""
	}

	u, err := url.Parse(fmt.Sprintf("%s%s", proto, s.c.HTTPPublicAddress))
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
	ch := c.Channels()
	for _, h := range ch {
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
		s.c.Log.Printf("remove client '%s[%d]' for '%s'", name, l, h)
	}
	s.clientsMutex.Unlock()

	if len(ch) == 0 {
		s.c.Log.Printf("remove client '%s[0]'", c.Name())
	}

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
	pubkey channel.PubKeyMessage,
	w channel.WriteFlusher,
	binW channel.BinaryWriter,
) (client.Config, *client.Client, error) {
	var conf client.Config
	filtered := make([]rune, 0, len(id.Data))
	for _, n := range id.Data {
		if unicode.IsPrint(n) && !unicode.IsSpace(n) {
			filtered = append(filtered, n)
		}
	}

	reqName := string(filtered)
	name := reqName

	if policy := s.c.PolicyLoader.Policy(); policy != PolicyWorld {
		fp := pubkey.Fingerprint()
		forced, err := s.c.PolicyLoader.Exists(fp)
		if policy == PolicyFixed {
			name = forced
		}

		if err != nil {
			s.c.Log.Printf("policy-loader err: %s", err)
			return conf, nil, errors.New("server error")
		} else if forced == "" {
			s.c.Log.Printf(
				"client with fingerprint %s and requested username %s is not in allow list",
				fp,
				reqName,
			)
			return conf, nil, errNotAllowed
		}
	}

	for _, h := range id.Channels {
		if _, ok := s.channels[h]; !ok {
			return conf, nil, fmt.Errorf("invalid channel subscribe: %s", h)
		}
	}

	if id.Version != s.c.ProtocolVersion {
		return conf, nil, errProto
	}

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

func (s *Server) handleConn(proto channel.Proto, conn net.Conn, addr string, frameWriter bool) error {
	read := func(r io.Reader, typ channel.Msg) (channel.Msg, io.Reader, error) {
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
		return err
	}
	clientKey := msg.(channel.PubKeyMessage)

	derive, err := channel.CommonSecret32(clientKey, server, key)
	if err != nil {
		return err
	}

	encryptedRW := &crypto.ReadWriter{
		crypto.NewDecrypter(reader, derive(channel.CryptoServerRead)),
		crypto.NewEncrypter(writeFlusher, derive(channel.CryptoServerWrite)),
	}

	macRSecret := derive(channel.CryptoServerMacRead)
	macWSecret := derive(channel.CryptoServerMacWrite)
	macR := crypto.NewSHA1HMACReader(encryptedRW, macRSecret[:])
	macW := crypto.NewSHA1HMACWriter(encryptedRW, macWSecret[:], 1<<16-1)

	writer = s.c.RWFactory.Writer(macW)
	reader = s.c.RWFactory.Reader(macR)

	writeFlusher = &channel.WriterFlusher{writer, channel.NewFlushFlusher(macW, writeFlusher)}

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
		if err == channel.ErrKeyExchange {
			return errKeyExchange
		}

		return err
	}

	if !test.Equal(msg) {
		return errKeyExchange
	}

	msg, reader, err = read(reader, channel.IdentifyMsg{})
	if err != nil {
		return fmt.Errorf("identify: %w", err)
	}
	id := msg.(channel.IdentifyMsg)

	conf, c, err := s.newClient(proto, frameWriter, id, clientKey, writeFlusher, s.c.RWFactory.BinaryWriter(writeFlusher))
	status := channel.StatusMsg{Code: channel.StatusOK}
	if err != nil {
		status.Code = channel.StatusNOK
		status.Err = err.Error()
		switch err {
		case errProto:
			status.Code = channel.StatusUpdateClient
		case errNotAllowed:
			status.Code = channel.StatusNotAllowed
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

		if chnl.Data == vars.EOFChannel {
			return nil
		}

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

	// conn.RemoteAddr().String() causes weird hang...
	address := conn.Request().RemoteAddr

	proto := channel.ReadProto(conn)
	// TODO uncomment when tls lands and when/if internal encryption is disabled
	// conn.PayloadType = websocket.TextFrame
	// if proto == channel.ProtoBinary {
	conn.PayloadType = websocket.BinaryFrame
	//}

	switch proto {
	case channel.ProtoJSON, channel.ProtoBinary:
		if err := s.handleConn(proto, conn, address, true); err != nil {
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
		if err := s.handleConn(proto, conn, conn.RemoteAddr().String(), false); err != nil {
			s.c.Log.Printf("client error: %s", err)
		}
	default:
		s.c.Log.Printf("client requested invalid protocol: %d", proto)
		return
	}
}

func (s *Server) RunHTTP() error {
	err := s.s.Start(s.c.HTTPAddress, s.tls)
	if s.closing {
		err = nil
	}
	return err
}

func (s *Server) RunTCP() error {
	var err error
	s.tcp, err = net.Listen("tcp", s.c.TCPAddress)
	if err != nil {
		return fmt.Errorf("could not open tcp connection %s: %w", s.c.TCPAddress, err)
	}

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

	return nil
}

func (s *Server) RunChannels() error {
	errs := make(chan error, 1)
	for chName, ch := range s.channels {
		go func(chName string, ch channel.Channel) {
			if err := ch.Run(); err != nil {
				errs <- fmt.Errorf("channel runtime error '%s': %w", chName, err)
			}
		}(chName, ch)
	}

	for err := range errs {
		s.c.Log.Println(err)
		return s.Close()
	}

	return nil
}

func (s *Server) Close() error {
	s.closing = true
	s.http.Close()
	strs := make([]string, 0)
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
