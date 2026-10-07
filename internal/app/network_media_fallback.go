package app

import (
	"context"
	"errors"
	"net/url"
	"strings"
)

func mediaFallbackVariants(media providerMedia) []providerMedia {
	seen := map[string]bool{media.URL: true}
	fallbacks := make([]providerMedia, 0, len(media.Variants))
	for _, fallback := range media.Variants {
		if !isProviderHTTPMediaURL(fallback.URL) || seen[fallback.URL] || media.Quality > 0 && fallback.Quality != media.Quality {
			continue
		}
		seen[fallback.URL] = true
		if fallback.Referer == "" {
			fallback.Referer = media.Referer
		}
		if fallback.credentials == nil {
			fallback.credentials = media.credentials
		}
		if !validProviderMediaCredentials(fallback.credentials, providerMediaCredentialReserve) {
			continue
		}
		if fallback.Duration == 0 {
			fallback.Duration = media.Duration
		}
		if fallback.Quality == 0 {
			fallback.Quality = media.Quality
		}
		fallback.HLSKey = firstNonEmptyBytes(fallback.HLSKey, media.HLSKey)
		fallback.CENCKey = firstNonEmptyBytes(fallback.CENCKey, media.CENCKey)
		fallback.Variants = media.Variants
		fallbacks = append(fallbacks, fallback)
	}
	return fallbacks
}

func firstNonEmptyBytes(values ...[]byte) []byte {
	for _, value := range values {
		if len(value) > 0 {
			return value
		}
	}
	return nil
}

func (d *Downloader) fetchMediaPlaylistForMedia(ctx context.Context, media providerMedia) (providerMedia, error) {
	fetch := func(candidate providerMedia) (providerMedia, error) {
		playlist, finalURL, err := d.fetchMediaPlaylist(providerMediaContext(ctx, candidate.credentials), candidate.URL, candidate.Referer)
		if err != nil {
			return providerMedia{}, err
		}
		candidate.Playlist, candidate.URL = playlist, finalURL
		if duration := m3u8Duration(playlist); duration > 0 {
			candidate.Duration = duration
		}
		return candidate, nil
	}
	selected, err := fetch(media)
	if err == nil {
		return selected, nil
	}
	var failures []error
	failures = append(failures, err)
	for _, fallback := range mediaFallbackVariants(media) {
		address, parseErr := url.Parse(fallback.URL)
		if parseErr != nil || !strings.HasSuffix(strings.ToLower(address.Path), ".m3u8") {
			continue
		}
		selected, err = fetch(fallback)
		if err == nil {
			return selected, nil
		}
		failures = append(failures, err)
	}
	return providerMedia{}, errors.Join(failures...)
}
