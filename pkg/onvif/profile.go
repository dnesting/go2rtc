package onvif

// Profile describes one ONVIF media profile. go2rtc stream name = profile token.
type Profile struct {
	Token string
	Video Video
}

type Video struct {
	Encoding  string // H264
	Profile   string // H264Profile value
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

// NewProfile describes a stream with the default values.
func NewProfile(token string) *Profile {
	return &Profile{
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
}
