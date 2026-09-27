package onvif

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/AlexxIT/go2rtc/internal/streams"
	"github.com/AlexxIT/go2rtc/pkg/core"
	"github.com/AlexxIT/go2rtc/pkg/onvif"
	"github.com/pion/rtp"
)

// probeTimeout keeps a probe under UniFi Protect's 10 second request timeout
var probeTimeout = 8 * time.Second

// probeRetryDelay is the pause before retrying a source that failed to connect
var probeRetryDelay = time.Second

// getProfile describes a stream from the codecs its source provides.
// A running source answers immediately; an idle source is connected for the probe.
// It retries until ctx is done, and returns an error rather than guessing
// when the stream can't be described in time.
func getProfile(ctx context.Context, name string) (*onvif.Profile, error) {
	stream := streams.Get(name)
	if stream == nil {
		return nil, errors.New("onvif: unknown profile " + name)
	}

	for {
		codecs, err := probe(ctx, stream)
		if err == nil {
			return onvif.NewProfile(name, codecs), nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("onvif: probe %s: %w", name, err)
		case <-time.After(probeRetryDelay):
			log.Debug().Err(err).Str("stream", name).Msg("[onvif] probe retry")
		}
	}
}

// probe attaches a prober to the stream to learn its codecs, or gives up when ctx is done.
// The prober is removed from the stream even if the caller has already given up.
func probe(ctx context.Context, stream *streams.Stream) ([]*core.Codec, error) {
	type result struct {
		codecs []*core.Codec
		err    error
	}
	ch := make(chan result, 1)

	go func() {
		cons := newProber()
		if err := stream.AddConsumer(cons); err != nil {
			ch <- result{err: err}
			return
		}
		stream.RemoveConsumer(cons)
		ch <- result{codecs: cons.Codecs()}
	}()

	select {
	case r := <-ch:
		return r.codecs, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type prober struct {
	core.Connection

	codecs []*core.Codec
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
	p.codecs = append(p.codecs, track.Codec.Clone())

	sender := core.NewSender(media, track.Codec)
	sender.Handler = func(*rtp.Packet) {}
	sender.HandleRTP(track)
	p.Senders = append(p.Senders, sender)
	return nil
}

func (p *prober) Start() error {
	return nil
}

func (p *prober) Codecs() []*core.Codec {
	return p.codecs
}
