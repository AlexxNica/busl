package broker

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"
	"testing"

	"github.com/heroku/busl/util"
	"github.com/stretchr/testify/assert"
)

func setup() string {
	registrar := NewRedisRegistrar()
	uuid, _ := util.NewUUID()
	registrar.Register(uuid)

	return uuid
}

func newReaderWriter() (io.ReadCloser, io.WriteCloser) {
	registrar := NewRedisRegistrar()
	uuid, _ := util.NewUUID()
	registrar.Register(uuid)
	r, _ := NewReader(uuid)
	w, _ := NewWriter(uuid)

	return r, w
}

func Example_pub_sub() {
	uuid := setup()

	r, _ := NewReader(uuid)
	defer r.(io.Closer).Close()

	pub := make(chan bool)
	done := make(chan bool)

	go func() {
		<-pub
		io.Copy(os.Stdout, r)
		done <- true
	}()

	go func() {
		pub <- true

		w, _ := NewWriter(uuid)
		w.Write([]byte("busl"))
		w.Write([]byte(" hello"))
		w.Write([]byte(" world"))
		w.Close()
	}()

	<-done

	//Output:
	// busl hello world
}

func Example_full_replay() {
	uuid := setup()

	w, _ := NewWriter(uuid)
	w.Write([]byte("busl"))
	w.Write([]byte(" hello"))
	w.Write([]byte(" world"))

	r, _ := NewReader(uuid)
	defer r.(io.Closer).Close()

	buf := make([]byte, 16)
	io.ReadAtLeast(r, buf, 16)

	fmt.Printf("%s", buf)

	//Output:
	// busl hello world
}

func TestSeekCorrect(t *testing.T) {
	uuid := setup()

	w, _ := NewWriter(uuid)
	w.Write([]byte("busl"))
	w.Write([]byte(" hello"))
	w.Write([]byte(" world"))
	w.Close()

	r, _ := NewReader(uuid)
	r.(io.Seeker).Seek(10, 0)
	defer r.(io.Closer).Close()

	buf, _ := ioutil.ReadAll(r)
	assert.Equal(t, " world", string(buf))
}

func TestSeekBeyond(t *testing.T) {
	uuid := setup()

	w, _ := NewWriter(uuid)
	w.Write([]byte("busl"))
	w.Write([]byte(" hello"))
	w.Write([]byte(" world"))
	w.Close()

	r, _ := NewReader(uuid)
	r.(io.Seeker).Seek(16, 0)
	defer r.Close()

	buf, _ := ioutil.ReadAll(r)
	assert.Equal(t, []byte{}, buf)
}

func Example_half_replay_half_subscribed() {
	uuid := setup()

	w, _ := NewWriter(uuid)
	w.Write([]byte("busl"))

	r, _ := NewReader(uuid)

	pub := make(chan bool)
	done := make(chan bool)

	go func() {
		<-pub
		io.Copy(os.Stdout, r)
		done <- true
	}()

	go func() {
		pub <- true

		w.Write([]byte(" hello"))
		w.Write([]byte(" world"))
		w.Close()
	}()

	<-done

	//Output:
	// busl hello world
}

func TestOverflowingBuffer(t *testing.T) {
	uuid := setup()

	w, _ := NewWriter(uuid)
	w.Write(bytes.Repeat([]byte("0"), 4096))
	w.Write(bytes.Repeat([]byte("1"), 4096))
	w.Write(bytes.Repeat([]byte("2"), 4096))
	w.Write(bytes.Repeat([]byte("3"), 4096))
	w.Write(bytes.Repeat([]byte("4"), 4096))
	w.Write(bytes.Repeat([]byte("5"), 4096))
	w.Write(bytes.Repeat([]byte("6"), 4096))
	w.Write(bytes.Repeat([]byte("7"), 4096))
	w.Write(bytes.Repeat([]byte("A"), 1))

	r, _ := NewReader(uuid)
	defer r.(io.Closer).Close()

	done := make(chan int64)
	go func() {
		n, _ := io.Copy(ioutil.Discard, r)
		done <- n
	}()
	w.Close()
	assert.Equal(t, int64(32769), <-done)
}

func Example_subscribe_concurrent() {
	r, w := newReaderWriter()

	pub := make(chan bool)
	done := make(chan bool)

	go func() {
		pub <- true
		w.Write([]byte("busl"))
		w.Close()
	}()

	go func() {
		<-pub
		io.Copy(os.Stdout, r)
		done <- true
	}()

	<-done
	//Output:
	// busl
}

func TestRedisReadFromClosed(t *testing.T) {
	r, w := newReaderWriter()
	p := make([]byte, 10)

	r.Read(p)
	w.Write([]byte("hello"))
	w.Close()

	// this read should short circuit with EOF
	_, err := r.Read(p)
	assert.Equal(t, err, io.EOF)

	// We'll get true here because r.closed is already set
	assert.True(t, readerDone(r))

	// We should still get true here because doneID is set
	r, _ = NewReader(string(r.(*reader).channel))
	assert.True(t, readerDone(r))

	// Reader done on a regular io.Reader should return false
	// and not panic
	assert.False(t, readerDone(strings.NewReader("hello")))
}

func TestLen(t *testing.T) {
	_, w := newReaderWriter()

	l, err := Len(w)
	assert.Nil(t, err)
	assert.Equal(t, int64(0), l)

	w.Write([]byte("hello"))

	l, err = Len(w)
	assert.Nil(t, err)
	assert.Equal(t, int64(5), l)
}
