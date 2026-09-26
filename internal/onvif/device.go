package onvif

import (
	"net/url"
	"strings"
	"text/template"

	"github.com/AlexxIT/go2rtc/internal/app"
)

// Device is the ONVIF device that this server presents.
// All strings except video source tokens and profiles are text/template.
type Device struct {
	Manufacturer    string `yaml:"manufacturer"`     // GetDeviceInformation
	Model           string `yaml:"model"`            // GetDeviceInformation
	FirmwareVersion string `yaml:"firmware_version"` // GetDeviceInformation
	SerialNumber    string `yaml:"serial_number"`    // GetDeviceInformation

	// Scopes are URIs, relative to onvif://www.onvif.org/. They replace the
	// default scopes of the same category (name, hardware, Profile, type).
	Scopes []string `yaml:"scopes"`

	// VideoSources are advertised in order. When set, only the listed streams
	// are profiles; otherwise every stream is a profile with its own video source.
	VideoSources []VideoSource `yaml:"video_sources"`
}

type VideoSource struct {
	Token    string   `yaml:"token"`    // default: the first profile
	Profiles []string `yaml:"profiles"` // go2rtc streams, highest quality first
}

// TemplateData is available to the device information and scope templates
type TemplateData struct {
	Version      string
	VideoSources []VideoSource
	Request      *Request // nil outside of an ONVIF request

	// rendered device information, for scope templates only
	Manufacturer    string
	Model           string
	FirmwareVersion string
	SerialNumber    string
}

type Request struct {
	Host string // HTTP Host header
}

var defaultDevice = Device{
	Model:           "go2rtc",
	FirmwareVersion: "{{.Version}}",
	SerialNumber:    "{{with .Request}}{{.Host}}{{end}}", // important for Hass: unique server ID
}

// defaultScopes include the name, hardware and Profile scopes that ONVIF requires
var defaultScopes = []string{
	"type/Network_Video_Transmitter",
	"Profile/Streaming",
	"name/go2rtc",
	"hardware/{{.Model}}",
}

var scopeBase = &url.URL{Scheme: "onvif", Host: "www.onvif.org", Path: "/"}

// field is a configured template with its default as a fallback
type field struct {
	name     string
	tmpl     *template.Template
	fallback *template.Template
}

var (
	manufacturer, model, firmwareVersion, serialNumber field

	configuredScopes []*template.Template
	defaultScopeTmpl []*template.Template

	videoSources []VideoSource // validated config; empty: one video source per stream
)

func init() {
	setDevice(defaultDevice)
}

func setDevice(d Device) {
	manufacturer = newField("manufacturer", d.Manufacturer, defaultDevice.Manufacturer)
	model = newField("model", d.Model, defaultDevice.Model)
	firmwareVersion = newField("firmware_version", d.FirmwareVersion, defaultDevice.FirmwareVersion)
	serialNumber = newField("serial_number", d.SerialNumber, defaultDevice.SerialNumber)

	configuredScopes = nil
	for _, s := range d.Scopes {
		if t, err := template.New("scope").Parse(s); err == nil {
			configuredScopes = append(configuredScopes, t)
		} else {
			log.Error().Err(err).Msg("[onvif] scopes")
		}
	}

	defaultScopeTmpl = nil
	for _, s := range defaultScopes {
		defaultScopeTmpl = append(defaultScopeTmpl, template.Must(template.New("scope").Parse(s)))
	}

	videoSources = validVideoSources(d.VideoSources)
}

func newField(name, text, fallback string) field {
	f := field{name: name, fallback: template.Must(template.New(name).Parse(fallback))}
	t, err := template.New(name).Parse(text)
	if err != nil {
		log.Error().Err(err).Msgf("[onvif] %s", name)
		t = f.fallback
	}
	f.tmpl = t
	return f
}

func (f *field) render(data *TemplateData) string {
	s, err := execute(f.tmpl, data)
	if err != nil {
		log.Warn().Err(err).Msgf("[onvif] %s", f.name)
		s, _ = execute(f.fallback, data)
	}
	return s
}

func execute(t *template.Template, data *TemplateData) (string, error) {
	var sb strings.Builder
	err := t.Execute(&sb, data)
	return sb.String(), err
}

// deviceInformation renders the device information; req is nil outside of a request
func deviceInformation(req *Request) *TemplateData {
	data := &TemplateData{Version: app.Version, VideoSources: sourceDefs(), Request: req}
	info := *data // device information templates don't see each other
	info.Manufacturer = manufacturer.render(data)
	info.Model = model.render(data)
	info.FirmwareVersion = firmwareVersion.render(data)
	info.SerialNumber = serialNumber.render(data)
	return &info
}

// scopes renders the scopes: the configured ones, and the defaults of other categories
func scopes(data *TemplateData) []string {
	var configured []string
	categories := map[string]bool{}
	for _, t := range configuredScopes {
		s, err := execute(t, data)
		if err == nil {
			s, err = resolveScope(s)
		}
		if err != nil {
			log.Warn().Err(err).Msg("[onvif] scopes")
			continue
		}
		configured = append(configured, s)
		categories[scopeCategory(s)] = true
	}

	var all []string
	for _, t := range defaultScopeTmpl {
		s, _ := execute(t, data)
		if s, err := resolveScope(s); err == nil && !categories[scopeCategory(s)] {
			all = append(all, s)
		}
	}
	return append(all, configured...)
}

// resolveScope resolves a URI reference against onvif://www.onvif.org/ (RFC 3986)
func resolveScope(s string) (string, error) {
	ref, err := url.Parse(s)
	if err != nil {
		return "", err
	}
	return scopeBase.ResolveReference(ref).String(), nil
}

// scopeCategory returns the category of an ONVIF defined scope, e.g. "name"
func scopeCategory(s string) string {
	u, err := url.Parse(s)
	if err != nil || u.Scheme != scopeBase.Scheme || !strings.EqualFold(u.Host, scopeBase.Host) {
		return ""
	}
	category, _, _ := strings.Cut(strings.TrimPrefix(u.Path, "/"), "/")
	return strings.ToLower(category)
}

// validVideoSources applies default tokens and skips invalid or duplicate entries
func validVideoSources(sources []VideoSource) []VideoSource {
	var valid []VideoSource
	tokens := map[string]bool{}
	streams := map[string]bool{} // each stream can be only one profile
	for _, vs := range sources {
		if len(vs.Profiles) == 0 {
			log.Error().Msgf("[onvif] video source %q has no profiles", vs.Token)
			continue
		}
		if vs.Token == "" {
			vs.Token = vs.Profiles[0]
		}
		if tokens[vs.Token] {
			log.Error().Msgf("[onvif] duplicate video source %q", vs.Token)
			continue
		}
		if p := repeatedStream(vs.Profiles, streams); p != "" {
			log.Error().Msgf("[onvif] video source %q: stream %q is already a profile", vs.Token, p)
			continue
		}
		tokens[vs.Token] = true
		for _, p := range vs.Profiles {
			streams[p] = true
		}
		valid = append(valid, vs)
	}
	return valid
}

// repeatedStream returns a stream that is in seen or listed twice, or ""
func repeatedStream(profiles []string, seen map[string]bool) string {
	listed := map[string]bool{}
	for _, p := range profiles {
		if seen[p] || listed[p] {
			return p
		}
		listed[p] = true
	}
	return ""
}
