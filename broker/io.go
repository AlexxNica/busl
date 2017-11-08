package broker

import (
	"errors"
	"io"
	"sync"

	"github.com/garyburd/redigo/redis"
	"github.com/heroku/busl/util"
)

type writer struct {
	channel channel
}

// known errors
var (
	ErrNotRegistered = errors.New("Channel is not registered.")
	ErrClosed        = errors.New("Channel is closed.")
)

// NewWriter creates a new redis channel writer
func NewWriter(key string) (io.WriteCloser, error) {
	r, err := NewRedisRegistrar().IsRegistered(key)
	if err != nil {
		return nil, err
	}

	if !r {
		return nil, ErrNotRegistered
	}

	return &writer{channel(key)}, nil
}

func (w *writer) Close() error {
	conn := redisPool.Get()
	defer conn.Close()

	conn.Send("MULTI")
	conn.Send("EXPIRE", w.channel.id(), redisKeyExpire)
	conn.Send("SETEX", w.channel.doneID(), redisChannelExpire, []byte{1})
	conn.Send("PUBLISH", w.channel.killID(), 1)
	_, err := conn.Do("EXEC")
	return err
}

func (w *writer) Write(p []byte) (int, error) {
	conn := redisPool.Get()
	defer conn.Close()

	conn.Send("MULTI")
	conn.Send("APPEND", w.channel.id(), p)
	conn.Send("EXPIRE", w.channel.id(), redisChannelExpire)
	conn.Send("DEL", w.channel.doneID())
	conn.Send("PUBLISH", w.channel.id(), 1)

	_, err := conn.Do("EXEC")
	return len(p), err
}

type reader struct {
	channel  channel
	psc      redis.PubSubConn
	offset   int64
	replayed bool
	closed   bool
	mutex    *sync.Mutex
	buffered bool
}

// NewReader creates a new redis channel reader
func NewReader(key string) (io.ReadCloser, error) {
	r, err := NewRedisRegistrar().IsRegistered(key)
	if err != nil {
		return nil, err
	}

	if !r {
		return nil, ErrNotRegistered
	}

	psc := redis.PubSubConn{Conn: redisPool.Get()}
	channel := channel(key)
	psc.PSubscribe(channel.wildcardID())

	rd := &reader{
		channel: channel,
		psc:     psc,
		mutex:   &sync.Mutex{}}

	return rd, nil
}

var errWhence = errors.New("Seek: invalid whence")
var errOffset = errors.New("Seek: invalid offset")

func (r *reader) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	default:
		return 0, errWhence
	case 0:
		r.offset = offset
	case 1:
		r.offset += offset
	}
	if offset < 0 {
		return 0, errOffset
	}

	return r.offset, nil
}

func (r *reader) Read(p []byte) (n int, err error) {
	if r.closed { // Don't read from a closed redigo connection
		return 0, io.EOF
	}

	if n, err := r.replay(p); n > 0 || err != nil {
		return n, err
	}

	switch msg := r.psc.Receive().(type) {
	case redis.Message:
	case redis.PMessage:
		return r.read(msg, p)
	case redis.Subscription:
	case error:
		util.CountWithData("RedisBroker.redisSubscribe.ReceiveError", 1, "err=%s", msg)
		err = msg
		return
	}
	return
}

func (r *reader) replay(p []byte) (n int, err error) {
	var buf []byte

	if !r.replayed {
		buf, err = r.fetch(len(p))
		n = len(buf)

		if n > 0 {
			r.offset += int64(n)
			copy(p, buf)
		}

		// Only mark as fully replayed if
		// we're no longer in buffered state.
		r.replayed = (!r.buffered || err == io.EOF)

		if err == io.EOF {
			util.Count("RedisBroker.replay.channelDone")
		}
	}

	return n, err
}

func (r *reader) read(msg redis.PMessage, p []byte) (n int, err error) {
	var buf []byte

	if msg.Channel == r.channel.killID() || msg.Channel == r.channel.id() {
		buf, err = r.fetch(len(p))

		if n = len(buf); n > 0 {
			copy(p, buf)
			r.offset += int64(n)
		}
	}

	if msg.Channel == r.channel.killID() || err == io.EOF {
		util.Count("RedisBroker.redisSubscribe.Channel.kill")
		r.Close()
		err = io.EOF
	}

	return n, err
}

func (r *reader) fetch(length int) ([]byte, error) {
	conn := redisPool.Get()
	defer conn.Close()

	start, end := r.offset, r.offset+int64(length)

	err := conn.Send("MULTI")
	if err != nil {
		return nil, err
	}
	err = conn.Send("GETRANGE", r.channel.id(), start, end-1)
	if err != nil {
		return nil, err
	}
	err = conn.Send("STRLEN", r.channel.id())
	if err != nil {
		return nil, err
	}
	err = conn.Send("EXISTS", r.channel.doneID())
	if err != nil {
		return nil, err
	}
	err = conn.Send("EXPIRE", r.channel.id(), redisChannelExpire)
	if err != nil {
		return nil, err
	}

	list, err := redis.Values(conn.Do("EXEC"))
	if err != nil {
		return nil, err
	}
	data, err := redis.Bytes(list[0], err)
	if err != nil {
		return nil, err
	}
	size, err := redis.Int64(list[1], err)
	if err != nil {
		return nil, err
	}
	done, err := redis.Bool(list[2], err)
	if err != nil {
		return nil, err
	}

	if r.buffered = end < size; !r.buffered && done {
		err = io.EOF
	}

	return data, err
}

func (r *reader) Close() error {
	if r.closed {
		return nil
	}

	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.closed = true
	r.psc.Unsubscribe()
	return r.psc.Close()
}

func readerDone(rd io.Reader) bool {
	r, ok := rd.(*reader)
	if !ok {
		return false
	}

	if r.closed {
		return true
	}

	conn := redisPool.Get()
	defer conn.Close()

	done, _ := redis.Bool(conn.Do("EXISTS", r.channel.doneID()))
	return done
}

// RenewExpiry renews the channel expiration
func RenewExpiry(rd io.Reader) {
	r, ok := rd.(*reader)
	if !ok {
		return
	}

	conn := redisPool.Get()
	defer conn.Close()

	conn.Send("MULTI")
	conn.Send("EXPIRE", r.channel.id(), redisChannelExpire)
	conn.Do("EXEC")
}

// Len returns the length of data already send to the reader
func Len(wd io.WriteCloser) (int64, error) {
	w, ok := wd.(*writer)
	if !ok {
		return 0, errors.New("Cannot cast argument to `writer`")
	}
	conn := redisPool.Get()
	defer conn.Close()

	strlen, err := redis.Int64(conn.Do("STRLEN", w.channel.id()))
	if err != nil {
		return 0, err
	}

	return strlen, nil
}
