package onvif

import (
	"math"

	"github.com/AlexxIT/go2rtc/pkg/core"
	"github.com/AlexxIT/go2rtc/pkg/h264"
	"github.com/AlexxIT/go2rtc/pkg/h265"
)

// Profile describes one ONVIF media profile. go2rtc stream name = profile token.
type Profile struct {
	Token string
	Video Video
}

type Video struct {
	Encoding  string // H264 or H265
	Profile   string // H264Profile / H265Profile value
	Width     int
	Height    int
	FrameRate int
	Bitrate   int // kbps
}

// defaults for values the stream doesn't tell us
const (
	defaultWidth     = 1920
	defaultHeight    = 1080
	defaultFrameRate = 30
	defaultBitrate   = 8192
)

// NewProfile describes a stream from its codecs. The frame rate comes from the SPS,
// else from frameRate (e.g. measured from timestamps) if not 0. Unknown values fall back to defaults.
func NewProfile(token string, codecs []*core.Codec, frameRate float64) *Profile {
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
					frameRate = fps
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
		}
	}

	if hasVideo && frameRate > 0 {
		p.Video.FrameRate = int(math.Round(frameRate))
	}

	return p
}
