package core

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/rtp"
)

var ErrCantGetTrack = errors.New("can't get track")

// CodecWatcher is fed the packets of a receiver whose codec lacks information
// that the stream carries in-band (e.g. H264/H265 parameter sets). It returns
// a completed copy of the codec once it has found that information, nil until then.
type CodecWatcher func(packet *Packet) *Codec

var codecWatchers = map[string]func(codec *Codec) CodecWatcher{}

// RegisterCodecWatcher sets the function that NewReceiver calls for codecs
// with this name. The function returns nil if the codec is already complete.
// Call it from init().
func RegisterCodecWatcher(name string, newWatcher func(codec *Codec) CodecWatcher) {
	codecWatchers[name] = newWatcher
}

// codecWatchTimeout - how long a receiver looks for missing codec information
// after its first packet
var codecWatchTimeout = 2 * ProbeTimeout

type Receiver struct {
	Node

	// Deprecated: should be removed
	Media *Media `json:"-"`
	// Deprecated: should be removed
	ID byte `json:"-"` // Channel for RTSP, PayloadType for MPEG-TS

	Bytes   int `json:"bytes,omitempty"`
	Packets int `json:"packets,omitempty"`

	watch     CodecWatcher // used only from Input
	watchEnd  time.Time
	codec     atomic.Pointer[Codec] // Codec completed from the stream
	ready     chan struct{}         // closed when watching is over
	closed    chan struct{}
	closeOnce sync.Once
}

func NewReceiver(media *Media, codec *Codec) *Receiver {
	r := &Receiver{
		Node:  Node{id: NewID(), Codec: codec},
		Media: media,
	}
	if newWatcher := codecWatchers[codec.Name]; newWatcher != nil {
		if r.watch = newWatcher(codec); r.watch != nil {
			r.ready = make(chan struct{})
			r.closed = make(chan struct{})
		}
	}
	r.Input = func(packet *Packet) {
		r.Bytes += len(packet.Payload)
		r.Packets++
		if r.watch != nil {
			r.watchCodec(packet)
		}
		for _, child := range r.childs {
			child.Input(packet)
		}
	}
	return r
}

func (r *Receiver) watchCodec(packet *Packet) {
	if codec := r.watch(packet); codec != nil {
		r.codec.Store(codec)
	} else if now := time.Now(); r.watchEnd.IsZero() {
		r.watchEnd = now.Add(codecWatchTimeout)
		return
	} else if now.Before(r.watchEnd) {
		return
	}
	r.watch = nil
	close(r.ready)
}

// WaitCodec returns r.Codec, completed with information from the stream
// (e.g. H264/H265 parameter sets) if the source didn't declare it.
// It returns once the information is found, the receiver gives up looking,
// ctx is done, ProbeTimeout passes or the receiver closes.
// Never nil: without the information it returns r.Codec.
// The returned codec must not be modified.
func (r *Receiver) WaitCodec(ctx context.Context) *Codec {
	if r.ready != nil {
		timer := time.NewTimer(ProbeTimeout)
		defer timer.Stop()

		select {
		case <-r.ready:
		case <-r.closed:
		case <-ctx.Done():
		case <-timer.C:
		}
	}
	return r.currentCodec()
}

func (r *Receiver) currentCodec() *Codec {
	if codec := r.codec.Load(); codec != nil {
		return codec
	}
	return r.Codec
}

// Deprecated: should be removed
func (r *Receiver) WriteRTP(packet *rtp.Packet) {
	r.Input(packet)
}

// Deprecated: should be removed
func (r *Receiver) Senders() []*Sender {
	if len(r.childs) > 0 {
		return []*Sender{{}}
	} else {
		return nil
	}
}

// Deprecated: should be removed
func (r *Receiver) Replace(target *Receiver) {
	MoveNode(&target.Node, &r.Node)
}

func (r *Receiver) Close() {
	if r.closed != nil {
		r.closeOnce.Do(func() { close(r.closed) })
	}
	r.Node.Close()
}

type Sender struct {
	Node

	// Deprecated:
	Media *Media `json:"-"`
	// Deprecated:
	Handler HandlerFunc `json:"-"`

	Bytes   int `json:"bytes,omitempty"`
	Packets int `json:"packets,omitempty"`
	Drops   int `json:"drops,omitempty"`

	buf  chan *Packet
	done chan struct{}
}

func NewSender(media *Media, codec *Codec) *Sender {
	var bufSize uint16

	if GetKind(codec.Name) == KindVideo {
		if codec.IsRTP() {
			// in my tests 40Mbit/s 4K-video can generate up to 1500 items
			// for the h264.RTPDepay => RTPPay queue
			bufSize = 4096
		} else {
			bufSize = 64
		}
	} else {
		bufSize = 128
	}

	buf := make(chan *Packet, bufSize)
	s := &Sender{
		Node:  Node{id: NewID(), Codec: codec},
		Media: media,
		buf:   buf,
	}
	s.Input = func(packet *Packet) {
		s.mu.Lock()
		// unblock write to nil chan - OK, write to closed chan - panic
		select {
		case s.buf <- packet:
			s.Bytes += len(packet.Payload)
			s.Packets++
		default:
			s.Drops++
		}
		s.mu.Unlock()
	}
	s.Output = func(packet *Packet) {
		s.Handler(packet)
	}
	return s
}

// Deprecated: should be removed
func (s *Sender) HandleRTP(parent *Receiver) {
	s.WithParent(parent)
	s.Start()
}

// Deprecated: should be removed
func (s *Sender) Bind(parent *Receiver) {
	s.WithParent(parent)
}

func (s *Sender) WithParent(parent *Receiver) *Sender {
	s.Node.WithParent(&parent.Node)
	return s
}

func (s *Sender) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.buf == nil || s.done != nil {
		return
	}
	s.done = make(chan struct{})

	// pass buf directly so that it's impossible for buf to be nil
	go func(buf chan *Packet) {
		for packet := range buf {
			s.Output(packet)
		}
		close(s.done)
	}(s.buf)
}

func (s *Sender) Wait() {
	if done := s.done; done != nil {
		<-done
	}
}

func (s *Sender) State() string {
	if s.buf == nil {
		return "closed"
	}
	if s.done == nil {
		return "new"
	}
	return "connected"
}

func (s *Sender) Close() {
	// close buffer if exists
	s.mu.Lock()
	if s.buf != nil {
		close(s.buf) // exit from for range loop
		s.buf = nil  // prevent writing to closed chan
	}
	s.mu.Unlock()

	s.Node.Close()
}

func (r *Receiver) MarshalJSON() ([]byte, error) {
	v := struct {
		ID      uint32   `json:"id"`
		Codec   *Codec   `json:"codec"`
		Childs  []uint32 `json:"childs,omitempty"`
		Bytes   int      `json:"bytes,omitempty"`
		Packets int      `json:"packets,omitempty"`
	}{
		ID:      r.Node.id,
		Codec:   r.currentCodec(),
		Bytes:   r.Bytes,
		Packets: r.Packets,
	}
	for _, child := range r.childs {
		v.Childs = append(v.Childs, child.id)
	}
	return json.Marshal(v)
}

func (s *Sender) MarshalJSON() ([]byte, error) {
	v := struct {
		ID      uint32 `json:"id"`
		Codec   *Codec `json:"codec"`
		Parent  uint32 `json:"parent,omitempty"`
		Bytes   int    `json:"bytes,omitempty"`
		Packets int    `json:"packets,omitempty"`
		Drops   int    `json:"drops,omitempty"`
	}{
		ID:      s.Node.id,
		Codec:   s.Node.Codec,
		Bytes:   s.Bytes,
		Packets: s.Packets,
		Drops:   s.Drops,
	}
	if s.parent != nil {
		v.Parent = s.parent.id
	}
	return json.Marshal(v)
}
