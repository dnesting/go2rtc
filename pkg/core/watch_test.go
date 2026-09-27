package core

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const codecTest = "TEST"

// test watcher: completes the codec with the payload of the first packet that starts with '!'
func init() {
	RegisterCodecWatcher(codecTest, func(codec *Codec) CodecWatcher {
		if codec.FmtpLine != "" {
			return nil
		}
		return func(packet *Packet) *Codec {
			if len(packet.Payload) == 0 || packet.Payload[0] != '!' {
				return nil
			}
			found := codec.Clone()
			found.FmtpLine = string(packet.Payload[1:])
			return found
		}
	})
}

func cancelled() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestWaitCodecComplete(t *testing.T) {
	for _, codec := range []*Codec{
		{Name: codecTest, FmtpLine: "complete"},
		{Name: CodecPCMU},
	} {
		r := NewReceiver(nil, codec)
		require.Same(t, codec, r.WaitCodec(cancelled()))
	}
}

func TestWaitCodecFound(t *testing.T) {
	codec := &Codec{Name: codecTest}
	r := NewReceiver(nil, codec)

	var received int
	child := &Node{Input: func(*Packet) { received++ }}
	r.AppendChild(child)

	r.Input(&Packet{Payload: []byte("data")})
	require.Same(t, codec, r.WaitCodec(cancelled()))

	r.Input(&Packet{Payload: []byte("!a=1")})
	found := r.WaitCodec(context.Background())
	require.Equal(t, "a=1", found.FmtpLine)
	require.Equal(t, "", r.Codec.FmtpLine)
	require.Same(t, codec, r.Codec)
	require.Equal(t, 2, received)

	// the first find wins
	r.Input(&Packet{Payload: []byte("!a=2")})
	require.Same(t, found, r.WaitCodec(cancelled()))

	b, err := json.Marshal(r)
	require.Nil(t, err)
	require.Contains(t, string(b), `"codec":{"codec_name":"TEST"`)
}

func TestWaitCodecContext(t *testing.T) {
	codec := &Codec{Name: codecTest}
	r := NewReceiver(nil, codec)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	require.Same(t, codec, r.WaitCodec(ctx))
	require.GreaterOrEqual(t, time.Since(start), 20*time.Millisecond)
}

func TestWaitCodecClose(t *testing.T) {
	codec := &Codec{Name: codecTest}
	r := NewReceiver(nil, codec)

	time.AfterFunc(20*time.Millisecond, r.Close)
	require.Same(t, codec, r.WaitCodec(context.Background()))

	r.Close() // twice is OK
	require.Same(t, codec, r.WaitCodec(context.Background()))
}

func TestWaitCodecGiveUp(t *testing.T) {
	defer func(d time.Duration) { codecWatchTimeout = d }(codecWatchTimeout)
	codecWatchTimeout = 10 * time.Millisecond

	codec := &Codec{Name: codecTest}
	r := NewReceiver(nil, codec)

	r.Input(&Packet{Payload: []byte("data")})
	time.Sleep(20 * time.Millisecond)
	r.Input(&Packet{Payload: []byte("data")})

	require.Same(t, codec, r.WaitCodec(context.Background()))

	// too late
	r.Input(&Packet{Payload: []byte("!a=1")})
	require.Same(t, codec, r.WaitCodec(context.Background()))
}

func TestWaitCodecRace(t *testing.T) {
	r := NewReceiver(nil, &Codec{Name: codecTest})

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				// as MarshalJSON (which itself races on Bytes and Packets)
				_ = r.currentCodec().FmtpLine
				_ = r.WaitCodec(cancelled()).FmtpLine
			}
			require.Equal(t, "a=1", r.WaitCodec(context.Background()).FmtpLine)
		}()
	}

	for i := 0; i < 100; i++ {
		r.Input(&Packet{Payload: []byte("data")})
	}
	r.Input(&Packet{Payload: []byte("!a=1")})

	wg.Wait()
}

func TestSetFmtp(t *testing.T) {
	require.Equal(t, "a=1", SetFmtp("", "a", "1"))
	require.Equal(t, "a=1;b=2", SetFmtp("a=1", "b", "2"))
	require.Equal(t, "a=1;b=3;c=4", SetFmtp("a=1;b=2;c=4", "b", "3"))
	require.Equal(t, "a=1;b=3", SetFmtp("a=1; b=2", "b", "3"))
	require.Equal(t, "ab=1;b=2", SetFmtp("ab=1", "b", "2"))
}
