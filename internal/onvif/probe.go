package onvif

import (
	"encoding/base64"
	"encoding/binary"
	"net/url"
	"sync"
	"time"

	"github.com/AlexxIT/go2rtc/internal/streams"
	"github.com/AlexxIT/go2rtc/pkg/core"
	"github.com/AlexxIT/go2rtc/pkg/h264"
	"github.com/AlexxIT/go2rtc/pkg/h265"
	"github.com/AlexxIT/go2rtc/pkg/onvif"
	"github.com/pion/rtp"
)

// keyframeTimeout limits how long a probe waits for in-band parameter sets
const keyframeTimeout = 5 * time.Second

// getProfile describes a stream from the codecs its source provides.
// A running source answers immediately; an idle source is connected for the probe.
// When the SDP has no video parameter sets, the probe waits for them in the bitstream.
func getProfile(name string) *onvif.Profile {
	stream := streams.Get(name)
	if stream == nil {
		return onvif.NewProfile(name, nil)
	}

	cons := newProber()
	if err := stream.AddConsumer(cons); err != nil {
		log.Debug().Err(err).Str("stream", name).Msg("[onvif] probe")
		return onvif.NewProfile(name, nil)
	}

	if cons.wait {
		select {
		case <-cons.done:
		case <-time.After(keyframeTimeout):
			log.Debug().Str("stream", name).Msg("[onvif] probe: no video parameters")
		}
	}

	stream.RemoveConsumer(cons)

	return onvif.NewProfile(name, cons.Codecs())
}

type prober struct {
	core.Connection

	mu     sync.Mutex
	codecs []*core.Codec
	done   chan struct{}
	wait   bool
}

func newProber() *prober {
	return &prober{
		Connection: core.Connection{
			ID:         core.NewID(),
			FormatName: "onvif",
			Medias:     core.ParseQuery(url.Values{"video": {""}, "audio": {""}}),
		},
		done: make(chan struct{}),
	}
}

func (p *prober) AddTrack(media *core.Media, _ *core.Codec, track *core.Receiver) error {
	codec := track.Codec.Clone()

	p.mu.Lock()
	p.codecs = append(p.codecs, codec)
	p.mu.Unlock()

	sender := core.NewSender(media, track.Codec)

	if handler := p.spsHandler(codec); handler != nil {
		p.wait = true
		if codec.IsRTP() {
			switch codec.Name {
			case core.CodecH264:
				handler = h264.RTPDepay(track.Codec, handler)
			case core.CodecH265:
				handler = h265.RTPDepay(track.Codec, handler)
			}
		}
		sender.Handler = handler
	} else {
		sender.Handler = func(*rtp.Packet) {}
	}

	sender.HandleRTP(track)
	p.Senders = append(p.Senders, sender)
	return nil
}

func (p *prober) Start() error {
	return nil
}

func (p *prober) Codecs() []*core.Codec {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.codecs
}

// spsHandler returns a handler that fills codec.FmtpLine from the first in-band SPS,
// or nil if the codec doesn't need one.
func (p *prober) spsHandler(codec *core.Codec) core.HandlerFunc {
	var prefix string

	switch codec.Name {
	case core.CodecH264:
		if sps, _ := h264.GetParameterSet(codec.FmtpLine); len(sps) > 0 {
			return nil
		}
		prefix = "sprop-parameter-sets="
	case core.CodecH265:
		if _, sps, _ := h265.GetParameterSet(codec.FmtpLine); len(sps) > 0 {
			return nil
		}
		prefix = "sprop-sps="
	default:
		return nil
	}

	var once sync.Once

	return func(packet *rtp.Packet) {
		// AVCC: 4-byte length prefixed NAL units
		for b := packet.Payload; len(b) > 4; {
			size := int(binary.BigEndian.Uint32(b)) + 4
			if size > len(b) {
				return
			}
			avcc := b[:size]
			b = b[size:]

			if isSPS(codec.Name, avcc) {
				once.Do(func() {
					p.mu.Lock()
					codec.FmtpLine = prefix + base64.StdEncoding.EncodeToString(avcc[4:])
					p.mu.Unlock()
					close(p.done)
				})
				return
			}
		}
	}
}

// isSPS checks a length-prefixed (AVCC) NAL unit
func isSPS(codecName string, avcc []byte) bool {
	if codecName == core.CodecH264 {
		return h264.NALUType(avcc) == h264.NALUTypeSPS
	}
	return h265.NALUType(avcc) == h265.NALUTypeSPS
}
