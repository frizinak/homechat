package data

import (
	"errors"
	"fmt"
	"io"

	"github.com/frizinak/homechat/server/channel"
)

// type Upload struct {
// 	full io.Reader
// 	file io.Reader
// 	hash hash.Hash
// }
//
// func (u *Upload) Read(b []byte) (n int, err error) {
// 	n, err = u.file.Read(b)
// 	u.hash.Write(b[:n])
// 	if err == io.EOF {
// 		sum := make([]byte, u.hash.Size())
// 		_, err = io.ReadFull(u.full, sum)
// 		if err != io.EOF && err != nil {
// 			panic(err)
// 			return
// 		}
// 		if !bytes.Equal(u.hash.Sum(nil), sum) {
// 			err = errors.New("upload checksum mismatch")
// 		}
//
// 		if err == nil {
// 			err = io.EOF
// 		}
// 	}
//
// 	return
// }

type Message struct {
	Filename string
	Message  string
	Size     int64
	Hash     []byte

	r      io.Reader
	rs     io.ReadSeeker
	upload io.Reader

	channel.NeverEqual
}

func NewMessage(filename, msg string, r io.ReadSeeker) Message {
	return Message{Filename: filename, Message: msg, r: r, rs: r}
}

func NewSizedMessage(filename, msg string, size int64, r io.Reader) Message {
	return Message{Filename: filename, Message: msg, Size: size, r: r}
}

func (m Message) Upload() io.Reader {
	return m.upload
}

func (m Message) Binary(w channel.BinaryWriter) error {
	if m.rs != nil {
		if _, err := m.rs.Seek(0, io.SeekStart); err != nil {
			return err
		}

		var err error
		var n int
		buf := make([]byte, 10*1024)
		for err == nil {
			n, err = m.rs.Read(buf)
			m.Size += int64(n)
		}

		if err != io.EOF {
			return fmt.Errorf("unexpected error while getting filesize: %w", err)
		}
		if _, err = m.rs.Seek(0, io.SeekStart); err != nil {
			return err
		}
	}

	w.WriteString(m.Filename, 8)
	w.WriteString(m.Message, 16)
	w.WriteUint64(uint64(m.Size))

	// hash := fnv.New64()
	if err := w.Err(); err != nil {
		return err
	}

	conn := w.Writer()
	_, err := io.Copy(conn, m.r)
	return err
	// rw := io.MultiWriter(conn, hash)
	// if _, err := io.Copy(rw, m.r); err != nil {
	// 	return err
	// }

	// if _, err := conn.Write(hash.Sum(nil)); err != nil {
	// 	return err
	// }
}

func (m Message) FromBinary(r channel.BinaryReader) (channel.Msg, error) { return BinaryMessage(r) }
func (m Message) FromJSON(r io.Reader) (channel.Msg, io.Reader, error)   { return JSONMessage(r) }

func (m Message) JSON(w io.Writer) error {
	return errors.New("can't serialize an upload")
}

func BinaryMessage(r channel.BinaryReader) (Message, error) {
	m := Message{}
	m.Filename = r.ReadString(8)
	m.Message = r.ReadString(16)
	m.Size = int64(r.ReadUint64())
	data := r.Reader()
	m.upload = io.LimitReader(data, m.Size)
	// m.upload = &Upload{
	// 	full: data,
	// 	file: io.LimitReader(data, m.Size),
	// 	// hash: fnv.New64(),
	// }
	return m, r.Err()
}

func JSONMessage(r io.Reader) (Message, io.Reader, error) {
	return Message{}, r, errors.New("can't deserialize an upload")
}
