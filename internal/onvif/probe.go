package onvif

import (
	"context"
	"net/url"

	"github.com/AlexxIT/go2rtc/internal/streams"
	"github.com/AlexxIT/go2rtc/pkg/core"
	"github.com/AlexxIT/go2rtc/pkg/onvif"
	"github.com/pion/rtp"
)

// getProfile describes a stream from the codecs its source provides.
// A running source answers immediately; an idle source is connected for the probe,
// unless the client has already disconnected (ctx is done).
// If the source doesn't declare the video parameter sets, the probe waits for them
// in the stream (see core.Receiver.WaitCodec).
func getProfile(ctx context.Context, name string) *onvif.Profile {
	stream := streams.Get(name)
	if stream == nil || ctx.Err() != nil {
		return onvif.NewProfile(name, nil)
	}

	cons := newProber()
	if err := stream.AddConsumer(cons); err != nil {
		log.Debug().Err(err).Str("stream", name).Msg("[onvif] probe")
		return onvif.NewProfile(name, nil)
	}

	codecs := make([]*core.Codec, len(cons.tracks))
	for i, track := range cons.tracks {
		codecs[i] = track.WaitCodec(ctx)
	}

	stream.RemoveConsumer(cons)

	return onvif.NewProfile(name, codecs)
}

type prober struct {
	core.Connection

	tracks []*core.Receiver
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
	p.tracks = append(p.tracks, track)

	sender := core.NewSender(media, track.Codec)
	sender.Handler = func(*rtp.Packet) {}
	sender.HandleRTP(track)
	p.Senders = append(p.Senders, sender)
	return nil
}

func (p *prober) Start() error {
	return nil
}
