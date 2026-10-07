package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const providerMaxBodyBytes = 20 * 1024 * 1024
const providerTimeout = 12 * time.Second

func (d *Downloader) fetchProviderText(ctx context.Context, rawURL, referer string) (string, error) {
	body, _, err := d.fetchProviderTextURL(ctx, rawURL, referer)
	return body, err
}

func (d *Downloader) fetchProviderTextURL(ctx context.Context, rawURL, referer string) (string, string, error) {
	retries := d.cfg.Retries
	if retries <= 0 {
		retries = 3
	}
	if single, _ := ctx.Value(providerTextSingleAttemptKey{}).(bool); single {
		retries = 1
	}
	if referer == "" {
		referer = rawURL
	}
	candidates := d.providerURLCandidates(rawURL)
	var lastErr error
	tried := 0
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return "", "", err
		}
		attempts := retries
		if len(candidates) > 1 {
			attempts = 1
		}
		for attempt := 1; attempt <= attempts; attempt++ {
			tried++
			if attempt > 1 {
				select {
				case <-time.After(time.Duration(attempt) * time.Second):
				case <-ctx.Done():
					return "", "", ctx.Err()
				}
			}
			timeout := providerTimeout
			if len(candidates) > 1 {
				timeout = 5 * time.Second
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, candidate, nil)
			if err != nil {
				return "", "", err
			}
			req.Header.Set("User-Agent", userAgent)
			if agent, _ := ctx.Value(providerTextUserAgentKey{}).(string); agent != "" {
				req.Header.Set("User-Agent", agent)
			}
			if noCache, _ := ctx.Value(providerTextNoCacheKey{}).(bool); noCache {
				req.Header.Set("Cache-Control", "no-cache")
				req.Header.Set("Pragma", "no-cache")
			}
			if providerSourceForURL(rawURL) != "" {
				req.Header.Set("Referer", providerRefererForURL(candidate, referer))
			} else {
				req.Header.Set("Referer", referer)
			}
			req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
			resp, err := d.doCatalogRequestWithTimeout(req, timeout)
			if err != nil {
				lastErr = err
				var backoff *requestBackoff
				if errors.As(err, &backoff) {
					break
				}
				continue
			}
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, providerMaxBodyBytes+1))
			closeErr := resp.Body.Close()
			if readErr != nil {
				lastErr = readErr
				continue
			}
			if closeErr != nil {
				lastErr = closeErr
				continue
			}
			if len(body) > providerMaxBodyBytes {
				lastErr = fmt.Errorf("response exceeds %d bytes", providerMaxBodyBytes)
				continue
			}
			if resp.StatusCode < 200 || resp.StatusCode >= 300 || catalogResponseBlockReason(resp, body) != "" {
				lastErr = d.catalogResponseError(req, resp, body)
				if catalogResponseBlockReason(resp, body) != "" || resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != 408 {
					break
				}
				continue
			}
			effectiveURL := req.URL
			if resp.Request != nil && resp.Request.URL != nil {
				effectiveURL = resp.Request.URL
			}
			if source := providerSourceForURL(rawURL); source != "" {
				d.providerMu.Lock()
				d.providerHosts[source] = effectiveURL.Scheme + "://" + effectiveURL.Host
				d.providerMu.Unlock()
			}
			return string(body), effectiveURL.String(), nil
		}
	}
	if len(candidates) > 1 && lastErr != nil {
		return "", "", fmt.Errorf("红果站点请求失败：已尝试 %d 个域名，最后错误：%w", tried, lastErr)
	}
	return "", "", lastErr
}
