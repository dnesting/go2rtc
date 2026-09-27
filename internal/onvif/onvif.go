package onvif

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AlexxIT/go2rtc/internal/api"
	"github.com/AlexxIT/go2rtc/internal/app"
	"github.com/AlexxIT/go2rtc/internal/rtsp"
	"github.com/AlexxIT/go2rtc/internal/streams"
	"github.com/AlexxIT/go2rtc/pkg/core"
	"github.com/AlexxIT/go2rtc/pkg/onvif"
	"github.com/rs/zerolog"
)

func Init() {
	log = app.GetLogger("onvif")

	streams.HandleFunc("onvif", streamOnvif)

	// ONVIF server on all suburls
	api.HandleFunc("/onvif/", onvifDeviceService)

	// ONVIF client autodiscovery
	api.HandleFunc("api/onvif", apiOnvif)
}

var log zerolog.Logger

func streamOnvif(rawURL string) (core.Producer, error) {
	client, err := onvif.NewClient(rawURL)
	if err != nil {
		return nil, err
	}

	uri, err := client.GetURI()
	if err != nil {
		return nil, err
	}

	// Append hash-based arguments to the retrieved URI
	if i := strings.IndexByte(rawURL, '#'); i > 0 {
		uri += rawURL[i:]
	}

	log.Debug().Msgf("[onvif] new uri=%s", uri)

	if err = streams.Validate(uri); err != nil {
		return nil, err
	}

	return streams.GetProducer(uri)
}

func onvifDeviceService(w http.ResponseWriter, r *http.Request) {
	b, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	operation := onvif.GetRequestAction(b)
	if operation == "" {
		http.Error(w, "malformed request body", http.StatusBadRequest)
		return
	}

	log.Trace().Msgf("[onvif] server request %s %s:\n%s", r.Method, r.RequestURI, b)

	ctx := r.Context() // stop probing streams when the client drops the connection

	switch operation {
	case onvif.ServiceGetServiceCapabilities, // important for Hass
		onvif.DeviceGetNetworkInterfaces, // important for Hass
		onvif.DeviceGetSystemDateAndTime, // important for Hass
		onvif.DeviceSetSystemDateAndTime, // return just OK
		onvif.DeviceGetDiscoveryMode,
		onvif.DeviceGetDNS,
		onvif.DeviceGetHostname,
		onvif.DeviceGetNetworkDefaultGateway,
		onvif.DeviceGetNetworkProtocols,
		onvif.DeviceGetNTP,
		onvif.DeviceGetScopes,
		onvif.MediaGetAudioEncoderConfigurations,
		onvif.MediaGetAudioSources,
		onvif.MediaGetAudioSourceConfigurations:
		b = onvif.StaticResponse(operation)

	case onvif.DeviceGetCapabilities:
		// important for Hass: Media section
		b = onvif.GetCapabilitiesResponse(r.Host)

	case onvif.DeviceGetServices:
		b = onvif.GetServicesResponse(r.Host)

	case onvif.DeviceGetDeviceInformation:
		// important for Hass: SerialNumber (unique server ID)
		b = onvif.GetDeviceInformationResponse("", "go2rtc", app.Version, r.Host)

	case onvif.DeviceSystemReboot:
		b = onvif.StaticResponse(operation)

		time.AfterFunc(time.Second, func() {
			os.Exit(0)
		})

	case onvif.MediaGetVideoSources,
		onvif.MediaGetProfiles,
		onvif.MediaGetProfile,
		onvif.MediaGetVideoSourceConfigurations,
		onvif.MediaGetVideoSourceConfiguration,
		onvif.MediaGetVideoEncoderConfigurations,
		onvif.MediaGetVideoEncoderConfiguration,
		onvif.MediaGetVideoEncoderConfigurationOptions:
		if b, err = mediaResponse(ctx, operation, b); err != nil {
			// a fault instead of guessed values: clients keep what they know and retry
			log.Warn().Err(err).Msgf("[onvif] %s", operation)
			w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write(onvif.FaultResponse(err.Error()))
			return
		}

	case onvif.MediaGetStreamUri:
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host // in case of Host without port
		}

		uri := "rtsp://" + host + ":" + rtsp.Port + "/" + onvif.FindTagValue(b, "ProfileToken")
		b = onvif.GetStreamUriResponse(uri)

	case onvif.MediaGetSnapshotUri:
		uri := "http://" + r.Host + "/api/frame.jpeg?src=" + onvif.FindTagValue(b, "ProfileToken")
		b = onvif.GetSnapshotUriResponse(uri)

	default:
		http.Error(w, "unsupported operation", http.StatusBadRequest)
		log.Warn().Msgf("[onvif] unsupported operation: %s", operation)
		log.Debug().Msgf("[onvif] unsupported request:\n%s", b)
		return
	}

	log.Trace().Msgf("[onvif] server response:\n%s", b)

	w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
	if _, err = w.Write(b); err != nil {
		log.Error().Err(err).Caller().Send()
	}
}

// mediaResponse answers media requests that describe streams (profiles, sources, encoders).
// Probing the streams takes at most probeTimeout.
func mediaResponse(ctx context.Context, operation string, req []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	switch operation {
	case onvif.MediaGetProfile:
		p, err := getProfile(ctx, onvif.FindTagValue(req, "ProfileToken"))
		if err != nil {
			return nil, err
		}
		return onvif.GetProfileResponse(p), nil

	case onvif.MediaGetVideoSourceConfiguration:
		p, err := getProfile(ctx, onvif.FindTagValue(req, "ConfigurationToken"))
		if err != nil {
			return nil, err
		}
		return onvif.GetVideoSourceConfigurationResponse(p), nil

	case onvif.MediaGetVideoEncoderConfiguration:
		p, err := getProfile(ctx, onvif.FindTagValue(req, "ConfigurationToken"))
		if err != nil {
			return nil, err
		}
		return onvif.GetVideoEncoderConfigurationResponse(p), nil

	case onvif.MediaGetVideoEncoderConfigurationOptions:
		// profile token and encoder configuration token are the same
		token := onvif.FindTagValue(req, "ProfileToken")
		if token == "" {
			token = onvif.FindTagValue(req, "ConfigurationToken")
		}
		p, err := getProfile(ctx, token)
		if err != nil {
			return nil, err
		}
		return onvif.GetVideoEncoderConfigurationOptionsResponse(p), nil
	}

	profiles, err := getProfiles(ctx)
	if err != nil {
		return nil, err
	}

	switch operation {
	case onvif.MediaGetVideoSources:
		return onvif.GetVideoSourcesResponse(profiles), nil
	case onvif.MediaGetProfiles:
		// important for Hass: H264 codec, width, height
		return onvif.GetProfilesResponse(profiles), nil
	case onvif.MediaGetVideoSourceConfigurations:
		// important for Happytime Onvif Client
		return onvif.GetVideoSourceConfigurationsResponse(profiles), nil
	default: // MediaGetVideoEncoderConfigurations
		return onvif.GetVideoEncoderConfigurationsResponse(profiles), nil
	}
}

// getProfiles describes all streams, probing them in parallel.
// Any failure fails the whole list, so a client never sees a profile disappear.
func getProfiles(ctx context.Context) ([]*onvif.Profile, error) {
	names := streams.GetAllNames()

	profiles := make([]*onvif.Profile, len(names))
	errs := make([]error, len(names))

	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func() {
			profiles[i], errs[i] = getProfile(ctx, name)
			wg.Done()
		}()
	}
	wg.Wait()

	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return profiles, nil
}

func apiOnvif(w http.ResponseWriter, r *http.Request) {
	src := r.URL.Query().Get("src")

	var items []*api.Source

	if src == "" {
		devices, err := onvif.DiscoveryStreamingDevices()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		for _, device := range devices {
			u, err := url.Parse(device.URL)
			if err != nil {
				log.Warn().Str("url", device.URL).Msg("[onvif] broken")
				continue
			}

			if u.Scheme != "http" {
				log.Warn().Str("url", device.URL).Msg("[onvif] unsupported")
				continue
			}

			u.Scheme = "onvif"
			u.User = url.UserPassword("user", "pass")

			if u.Path == onvif.PathDevice {
				u.Path = ""
			}

			items = append(items, &api.Source{
				Name: u.Host,
				URL:  u.String(),
				Info: device.Name + " " + device.Hardware,
			})
		}
	} else {
		client, err := onvif.NewClient(src)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if l := log.Trace(); l.Enabled() {
			b, _ := client.MediaRequest(onvif.MediaGetProfiles)
			l.Msgf("[onvif] src=%s profiles:\n%s", src, b)
		}

		name, err := client.GetName()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		tokens, err := client.GetProfilesTokens()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		for i, token := range tokens {
			items = append(items, &api.Source{
				Name: name + " stream" + strconv.Itoa(i),
				URL:  src + "?subtype=" + token,
			})
		}

		if len(tokens) > 0 && client.HasSnapshots() {
			items = append(items, &api.Source{
				Name: name + " snapshot",
				URL:  src + "?subtype=" + tokens[0] + "&snapshot",
			})
		}
	}

	api.ResponseSources(w, items)
}
