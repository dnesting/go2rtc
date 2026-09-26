package onvif

import (
	"math"

	"github.com/AlexxIT/go2rtc/pkg/core"
	"github.com/AlexxIT/go2rtc/pkg/h264"
	"github.com/AlexxIT/go2rtc/pkg/h265"
)

// Profile describes one ONVIF media profile. go2rtc stream name = profile token.
type Profile struct {
	Token  string
	Source *VideoSource // nil: the profile is the only one of its own video source
	Video  Video
	Audio  *Audio // nil if the stream has no audio
}

// VideoSource is a physical video input, shared by the profiles that encode it.
// It also stands for the input's audio source and both source configurations.
type VideoSource struct {
	Token     string
	Width     int
	Height    int
	FrameRate int
	Audio     bool // at least one profile has audio
}

// NewVideoSource links profiles to a shared source described by the first (highest quality) profile
func NewVideoSource(token string, profiles []*Profile) *VideoSource {
	s := &VideoSource{Token: token}
	if len(profiles) > 0 {
		v := profiles[0].Video
		s.Width, s.Height, s.FrameRate = v.Width, v.Height, v.FrameRate
	}
	for _, p := range profiles {
		p.Source = s
		if p.Audio != nil {
			s.Audio = true
		}
	}
	return s
}

func (p *Profile) source() *VideoSource {
	if p.Source != nil {
		return p.Source
	}
	return &VideoSource{
		Token: p.Token, Width: p.Video.Width, Height: p.Video.Height, FrameRate: p.Video.FrameRate, Audio: p.Audio != nil,
	}
}

// videoSources returns the distinct sources of profiles, in profile order
func videoSources(profiles []*Profile) []*VideoSource {
	var sources []*VideoSource
	seen := map[*VideoSource]bool{}
	for _, p := range profiles {
		if s := p.source(); !seen[s] {
			seen[s] = true
			sources = append(sources, s)
		}
	}
	return sources
}

type Video struct {
	Encoding  string // H264 or H265
	Profile   string // H264Profile / H265Profile value
	Width     int
	Height    int
	FrameRate int
	Bitrate   int // kbps
}

type Audio struct {
	Encoding   string // G711 or AAC
	SampleRate int    // kHz
	Bitrate    int    // kbps
}

// defaults for values the stream doesn't tell us
const (
	defaultWidth     = 1920
	defaultHeight    = 1080
	defaultFrameRate = 30
	defaultBitrate   = 8192
)

// NewProfile describes a stream from its codecs. Unknown values fall back to defaults.
func NewProfile(token string, codecs []*core.Codec) *Profile {
	p := &Profile{
		Token: token,
		Video: Video{
			Encoding:  "H264",
			Profile:   "Main",
			Width:     defaultWidth,
			Height:    defaultHeight,
			FrameRate: defaultFrameRate,
			Bitrate:   defaultBitrate,
		},
	}

	var hasVideo bool

	for _, codec := range codecs {
		switch codec.Name {
		case core.CodecH264:
			if hasVideo {
				continue
			}
			hasVideo = true
			sps, _ := h264.GetParameterSet(codec.FmtpLine)
			if s := h264.DecodeSPS(sps); s != nil {
				p.Video.Width = int(s.Width())
				p.Video.Height = int(s.Height())
				p.Video.Profile = s.Profile()
				if fps := s.FrameRate(); fps > 0 {
					p.Video.FrameRate = int(math.Round(fps))
				}
			}

		case core.CodecH265:
			if hasVideo {
				continue
			}
			hasVideo = true
			p.Video.Encoding = "H265"
			if _, sps, _ := h265.GetParameterSet(codec.FmtpLine); len(sps) > 2 {
				if s := h265.DecodeSPS(sps); s != nil {
					p.Video.Width = int(s.Width())
					p.Video.Height = int(s.Height())
				}
			}

		case core.CodecPCMA, core.CodecPCMU:
			if p.Audio == nil {
				p.Audio = &Audio{Encoding: "G711", SampleRate: int(codec.ClockRate) / 1000, Bitrate: 64}
			}

		case core.CodecAAC:
			if p.Audio == nil {
				p.Audio = &Audio{Encoding: "AAC", SampleRate: int(codec.ClockRate) / 1000, Bitrate: 64}
			}
		}
	}

	return p
}
