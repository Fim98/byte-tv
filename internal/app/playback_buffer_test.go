package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestPlaybackPrefetchYieldsResourcesUntilPromoted(t *testing.T) {
	for _, promoted := range []bool{false, true} {
		t.Run(fmt.Sprintf("promoted=%t", promoted), func(t *testing.T) {
			app, _ := prefetchFixtureApp(t)
			resources := app.mediaResources()
			cache := newPlaybackPrefetch(2, 1)
			defer cache.cancel()
			release, err := resources.acquire(cache.ctx, "video", true)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			if promoted && !cache.promote() {
				t.Fatal("prefetch could not become foreground")
			}
			if backgroundPlayback(cache.ctx) == promoted {
				t.Fatal("prefetch context did not reflect current priority")
			}
			stopped := make(chan struct{})
			go func() {
				<-cache.ctx.Done()
				release()
				close(stopped)
			}()
			defer func() { cache.cancel(); <-stopped }()
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			defer cancel()
			foreground, err := resources.acquire(ctx, "video", false)
			if promoted {
				if !errors.Is(err, context.DeadlineExceeded) || cache.ctx.Err() != nil {
					t.Fatal("current playback was mistaken for disposable prefetch", err)
				}
			} else {
				if err != nil {
					t.Fatal("optional prefetch blocked foreground playback", err)
				}
				foreground()
				if cache.promote() {
					t.Fatal("preempted bytes were promoted into a new episode")
				}
			}
		})
	}
}

func TestPlaybackNativeBuffersAheadAcrossThreePrefetchedEpisodes(t *testing.T) {
	app, session := nativePlaybackFixture(t, "64", "160x90")
	waitAhead := func(cache *playbackNative) {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for {
			cache.mu.Lock()
			ready := len(cache.segments[playbackNativeBatchSegments]) > 0
			failure, size := cache.err, cache.bytes
			cache.mu.Unlock()
			if failure != nil || size > playbackNativeCacheBytes {
				t.Fatal("native buffering failed or exceeded its byte limit", failure, size)
			}
			if ready {
				return
			}
			if time.Now().After(deadline) {
				t.Fatal("next batch was deferred until the current batch was nearly exhausted")
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	for episode := 1; episode <= 3; episode++ {
		var prefetched *playbackNative
		if episode > 1 {
			prefetched = session.prefetch.native
		}
		opened := nativeOpenFixture(t, app, episode, 0, episode)
		cache := session.native
		if episode > 1 && (cache != prefetched || string(opened["prefetched"]) != "true" || backgroundPlayback(cache.ctx)) {
			t.Fatal("next episode did not adopt its cache with foreground priority")
		}
		waitAhead(cache)
		if episode == 3 {
			break
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		for index := 0; index < 32; index++ {
			if _, err := cache.segment(ctx, index); err != nil {
				cancel()
				t.Fatal(err)
			}
		}
		cancel()
		result := prefetchRequest(app, fmt.Sprintf("{\"session\":\"fixture\",\"episode\":%d,\"run\":%d,\"version\":1}", episode+1, session.run))
		if result.Code != http.StatusAccepted {
			t.Fatal("next episode prefetch was not scheduled", result.Code, result.Body.String())
		}
		next := session.prefetch.native
		select {
		case <-next.firstBatch:
		case <-time.After(8 * time.Second):
			t.Fatal("next episode's first batch did not complete")
		}
		next.mu.Lock()
		count, failure := len(next.segments), next.err
		next.mu.Unlock()
		if failure != nil || count != playbackNativeBatchSegments {
			t.Fatal("background prefetch should stop after a bounded first batch", count, failure)
		}
	}
}
