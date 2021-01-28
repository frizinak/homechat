package channel

import (
	"bytes"
	"encoding/json"
	"io"
)

type Proto byte

func (p Proto) Write(w io.Writer) error {
	_, err := w.Write([]byte{byte(p)})
	return err
}

func ReadProto(r io.Reader) Proto {
	b := make([]byte, 1)
	_, err := io.ReadFull(r, b)
	if err != nil {
		return ProtoNone
	}
	return Proto(b[0])
}

const (
	ProtoNone Proto = iota
	ProtoJSON
	ProtoBinary
)

func StripUnprintable(str string) string {
	runes := make([]rune, 0, len(str))
	for _, n := range str {
		switch {
		case n == 9 || n == '\n':
		case n < 32:
			continue
		}
		runes = append(runes, n)
	}
	return string(runes)
}

type Msg interface {
	Binary(BinaryWriter) error
	JSON(io.Writer) error
	Equal(Msg) bool

	FromBinary(BinaryReader) (Msg, error)
	FromJSON(io.Reader) (Msg, io.Reader, error)

	Close() error
}

type StatusCode byte

const (
	StatusOK StatusCode = iota
	StatusNOK
	StatusUpdateClient
	StatusNotAllowed
)

type StatusMsg struct {
	Code StatusCode `json:"code"`
	Err  string     `json:"err"`

	NeverEqual
	NoClose
}

func (m StatusMsg) Is(s StatusCode) bool { return m.Code == s }
func (m StatusMsg) OK() bool             { return m.Code == StatusOK }

func (m StatusMsg) Binary(w BinaryWriter) error {
	w.WriteUint8(byte(m.Code))
	w.WriteString(m.Err, 8)
	return w.Err()
}

func (m StatusMsg) JSON(w io.Writer) error {
	return json.NewEncoder(w).Encode(m)
}

func (m StatusMsg) FromBinary(r BinaryReader) (Msg, error)       { return BinaryStatusMsg(r) }
func (m StatusMsg) FromJSON(r io.Reader) (Msg, io.Reader, error) { return JSONStatusMsg(r) }

func BinaryStatusMsg(r BinaryReader) (StatusMsg, error) {
	var m StatusMsg
	m.Code = StatusCode(r.ReadUint8())
	m.Err = r.ReadString(8)
	return m, nil
}

func JSONStatusMsg(r io.Reader) (StatusMsg, io.Reader, error) {
	msg := StatusMsg{}
	nr, err := JSON(r, &msg)
	return msg, nr, err
}

type IdentifyMsg struct {
	Data     string   `json:"d"`
	Channels []string `json:"c"`
	Version  string   `json:"v"`

	NeverEqual
	NoClose
}

func (h IdentifyMsg) Binary(w BinaryWriter) error {
	w.WriteString(h.Version, 8)
	w.WriteString(h.Data, 8)
	w.WriteUint8(uint8(len(h.Channels)))
	for _, h := range h.Channels {
		w.WriteString(h, 8)
	}
	return w.Err()
}

func (h IdentifyMsg) JSON(w io.Writer) error {
	return json.NewEncoder(w).Encode(h)
}

func (m IdentifyMsg) FromBinary(r BinaryReader) (Msg, error)       { return BinaryIdentifyMsg(r) }
func (m IdentifyMsg) FromJSON(r io.Reader) (Msg, io.Reader, error) { return JSONIdentifyMsg(r) }

func BinaryIdentifyMsg(r BinaryReader) (IdentifyMsg, error) {
	v := r.ReadString(8)
	n := StripUnprintable(r.ReadString(8))
	nh := int(r.ReadUint8())
	l := make([]string, 0, nh)
	for i := 0; i < nh; i++ {
		l = append(l, StripUnprintable(r.ReadString(8)))
	}
	return IdentifyMsg{Data: n, Channels: l, Version: v}, r.Err()
}

func JSONIdentifyMsg(r io.Reader) (IdentifyMsg, io.Reader, error) {
	msg := IdentifyMsg{}
	nr, err := JSON(r, &msg)
	return msg, nr, err
}

type ChannelMsg struct {
	Data string `json:"d"`
	NoClose
}

func (h ChannelMsg) Binary(w BinaryWriter) error {
	w.WriteString(h.Data, 8)
	return w.Err()
}

func (h ChannelMsg) JSON(w io.Writer) error {
	return json.NewEncoder(w).Encode(h)
}

func (m ChannelMsg) FromBinary(r BinaryReader) (Msg, error)       { return BinaryChannelMsg(r) }
func (m ChannelMsg) FromJSON(r io.Reader) (Msg, io.Reader, error) { return JSONChannelMsg(r) }
func (m ChannelMsg) Equal(Msg) bool                               { return false }

func BinaryChannelMsg(r BinaryReader) (ChannelMsg, error) {
	n := StripUnprintable(r.ReadString(8))
	return ChannelMsg{Data: n}, r.Err()
}

func JSONChannelMsg(r io.Reader) (ChannelMsg, io.Reader, error) {
	msg := ChannelMsg{}
	nr, err := JSON(r, &msg)
	return msg, nr, err
}

type EOF struct {
	NilMsg
}

func (m EOF) FromBinary(r BinaryReader) (Msg, error)       { return BinaryMessage(r) }
func (m EOF) FromJSON(r io.Reader) (Msg, io.Reader, error) { return JSONMessage(r) }

func BinaryMessage(r BinaryReader) (EOF, error) {
	n, err := BinaryNilMessage(r)
	c := EOF{n}
	return c, err
}

func JSONMessage(r io.Reader) (EOF, io.Reader, error) {
	n, nr, err := JSONNilMessage(r)
	c := EOF{n}
	return c, nr, err
}

func JSON(r io.Reader, data interface{}) (io.Reader, error) {
	d := json.NewDecoder(r)
	err := d.Decode(data)
	buf := d.Buffered()
	if bbuf, ok := buf.(*bytes.Reader); ok {
		ln := bbuf.Len()
		if ln == 1 { // almost always the case
			// not sure if we gain a lot here, works even without this case
			// newline is only required if were encoding raw ints.
			// also: we could do this in any case (ln>0)
			b, rerr := bbuf.ReadByte()
			if rerr == nil && b == '\n' {
				return r, err
			}
			bbuf.UnreadByte()
		} else if ln == 0 {
			return r, err
		}
	}

	// if !d.More() { // will cause a read, nooope

	// Is this correct? docs are not very clear.
	// But even if correct, it might not be as optimal as just
	// returning the multireader with remaining buffer.
	// Type checking the reader (should be bytes.Buffer)
	// and just checking its length seems the most optimal way to go.

	//return r, err
	//}
	return io.MultiReader(buf, r), err
}
