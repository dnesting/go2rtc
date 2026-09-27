package onvif

import (
	"context"
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

// framesToMeasure is how many frames a probe times when the stream doesn't declare a frame rate
const framesToMeasure = 10

// frameRateWindow limits how long a probe waits for those frames
var frameRateWindow = 2 * time.Second

// getProfile describes a stream from the codecs its source provides.
// A running source answers immediately; an idle source is connected for the probe,
// unless the client has already disconnected (ctx is done).
// When the SDP has no video parameter sets, the probe waits for them in the bitstream,
// and when the SPS declares no frame rate, it times the first frames.
func getProfile(ctx context.Context, name string) *onvif.Profile {
	stream := streams.Get(name)
	if stream == nil || ctx.Err() != nil {
		return onvif.NewProfile(name, nil, 0)
	}

	cons := newProber()
	if err := stream.AddConsumer(cons); err != nil {
		log.Debug().Err(err).Str("stream", name).Msg("[onvif] probe")
		return onvif.NewProfile(name, nil, 0)
	}

	if w := cons.video; w != nil {
		select {
		case <-w.params:
			select {
			case <-w.measured:
			case <-ctx.Done():
			case <-time.After(frameRateWindow):
			}
		case <-ctx.Done():
		case <-time.After(keyframeTimeout):
			log.Debug().Str("stream", name).Msg("[onvif] probe: no video parameter sets")
		}
	}

	stream.RemoveConsumer(cons)

	return onvif.NewProfile(name, cons.Codecs(), cons.FrameRate())
}

type prober struct {
	core.Connection

	mu     sync.Mutex
	codecs []*core.Codec
	video  *videoWatch // the first H264 or H265 track
}

// videoWatch fills in parameter sets that the SDP lacks from the bitstream,
// and times frames for the frame rate
type videoWatch struct {
	codec    *core.Codec
	needSPS  bool          // the SDP has no parameter sets
	declared bool          // the SPS declares a frame rate
	stamps   []uint32      // RTP timestamps of the first frames
	params   chan struct{} // closed when the parameter sets are known
	measured chan struct{} // closed when the frame rate is known
}

func newProber() *prober {
	return &prober{
		Connection: core.Connection{
			ID:         core.NewID(),
			FormatName: "onvif",
			Medias:     core.ParseQuery(url.Values{"video": {""}}),
		},
	}
}

func (p *prober) AddTrack(media *core.Media, _ *core.Codec, track *core.Receiver) error {
	codec := track.Codec.Clone()

	p.mu.Lock()
	p.codecs = append(p.codecs, codec)
	p.mu.Unlock()

	sender := core.NewSender(media, track.Codec)
	sender.Handler = func(*rtp.Packet) {}

	if p.video == nil && (codec.Name == core.CodecH264 || codec.Name == core.CodecH265) {
		w := newVideoWatch(codec)
		p.video = w
		handler := func(packet *rtp.Packet) {
			p.mu.Lock()
			w.frame(packet)
			p.mu.Unlock()
		}
		if codec.IsRTP() {
			if codec.Name == core.CodecH264 {
				handler = h264.RTPDepay(track.Codec, handler)
			} else {
				handler = h265.RTPDepay(track.Codec, handler)
			}
		}
		sender.Handler = handler
	}

	sender.HandleRTP(track)
	p.Senders = append(p.Senders, sender)
	return nil
}

func (p *prober) Start() error {
	return nil
}

// Codecs returns copies of the track codecs, with the parameter sets found so far
func (p *prober) Codecs() []*core.Codec {
	p.mu.Lock()
	defer p.mu.Unlock()
	codecs := make([]*core.Codec, len(p.codecs))
	for i, codec := range p.codecs {
		codecs[i] = codec.Clone()
	}
	return codecs
}

// FrameRate returns the frame rate measured from frame timestamps, or 0
func (p *prober) FrameRate() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.video == nil || p.video.declared {
		return 0
	}
	return p.video.frameRate()
}

func newVideoWatch(codec *core.Codec) *videoWatch {
	w := &videoWatch{codec: codec, params: make(chan struct{}), measured: make(chan struct{})}
	if codec.Name == core.CodecH264 {
		sps, _ := h264.GetParameterSet(codec.FmtpLine)
		w.needSPS = len(sps) == 0
	} else {
		_, sps, _ := h265.GetParameterSet(codec.FmtpLine)
		w.needSPS = len(sps) == 0
	}
	if !w.needSPS {
		w.paramsKnown()
	}
	return w
}

// frame handles one access unit: length prefixed (AVCC) NAL units
func (w *videoWatch) frame(packet *rtp.Packet) {
	if w.needSPS {
		if sps := findSPS(w.codec.Name, packet.Payload); sps != nil {
			if w.codec.Name == core.CodecH264 {
				w.codec.FmtpLine = "sprop-parameter-sets=" + base64.StdEncoding.EncodeToString(sps)
			} else {
				w.codec.FmtpLine = "sprop-sps=" + base64.StdEncoding.EncodeToString(sps)
			}
			w.needSPS = false
			w.paramsKnown()
		}
	}

	if n := len(w.stamps); n < framesToMeasure && (n == 0 || w.stamps[n-1] != packet.Timestamp) {
		w.stamps = append(w.stamps, packet.Timestamp)
	}

	if !w.needSPS && !w.declared && len(w.stamps) == framesToMeasure && !closed(w.measured) {
		close(w.measured)
	}
}

func (w *videoWatch) paramsKnown() {
	if w.codec.Name == core.CodecH264 {
		sps, _ := h264.GetParameterSet(w.codec.FmtpLine)
		if s := h264.DecodeSPS(sps); s != nil && s.FrameRate() > 0 {
			w.declared = true
		}
	}
	close(w.params)
	if w.declared {
		close(w.measured)
	}
}

// frameRate is frames per second from the RTP timestamps of the first frames, or 0
func (w *videoWatch) frameRate() float64 {
	if len(w.stamps) < 2 || w.codec.ClockRate == 0 {
		return 0
	}
	var span int32 // RTP timestamps wrap around; B-frames can be out of order
	for _, ts := range w.stamps {
		span = max(span, int32(ts-w.stamps[0]))
	}
	if span <= 0 {
		return 0
	}
	return float64(len(w.stamps)-1) * float64(w.codec.ClockRate) / float64(span)
}

func closed(ch chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// findSPS returns the first SPS NAL unit in length prefixed (AVCC) NAL units
func findSPS(codecName string, b []byte) []byte {
	for len(b) > 4 {
		size := int(binary.BigEndian.Uint32(b)) + 4
		if size > len(b) {
			return nil
		}
		if isSPS(codecName, b[:size]) {
			return b[4:size]
		}
		b = b[size:]
	}
	return nil
}

// isSPS checks a length-prefixed (AVCC) NAL unit
func isSPS(codecName string, avcc []byte) bool {
	if codecName == core.CodecH264 {
		return h264.NALUType(avcc) == h264.NALUTypeSPS
	}
	return h265.NALUType(avcc) == h265.NALUTypeSPS
}
