package certname

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/ano/ano/internal/store"
)

// Watch reads certificate names for recorded HTTP and HTTPS traffic and tags those names.
func Watch(ctx context.Context, st *store.Store) {
	_ = st.ClearPlaceholderNames()
	failed := map[string]time.Time{}
	pass := func() {
		namePass(ctx, st, failed)
	}
	pass()
	tick := time.NewTicker(2 * time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			pass()
		}
	}
}

func namePass(ctx context.Context, st *store.Store, failed map[string]time.Time) {
	list, err := st.UnnamedWeb(400)
	if err != nil || len(list) == 0 {
		return
	}
	now := time.Now()
	var jobs []store.WebEndpoint
	for _, item := range list {
		ip, _, err := net.SplitHostPort(item.Destination)
		if err != nil {
			ip = ""
		}
		parsed := net.ParseIP(ip)
		if parsed == nil || parsed.IsPrivate() || parsed.IsLoopback() || parsed.IsLinkLocalUnicast() {
			continue
		}
		if at, ok := failed[item.Destination]; ok && now.Sub(at) < 6*time.Hour {
			continue
		}
		jobs = append(jobs, item)
	}
	if len(jobs) == 0 {
		return
	}
	sem := make(chan struct{}, 4)
	var wait sync.WaitGroup
	var mu sync.Mutex
	mark := func(dest string, ok bool) {
		mu.Lock()
		defer mu.Unlock()
		if ok {
			delete(failed, dest)
			return
		}
		failed[dest] = time.Now()
	}
	for _, item := range jobs {
		if ctx.Err() != nil {
			break
		}
		wait.Add(1)
		sem <- struct{}{}
		go func(item store.WebEndpoint) {
			defer wait.Done()
			defer func() { <-sem }()
			ip, _, err := net.SplitHostPort(item.Destination)
			if err != nil {
				mark(item.Destination, false)
				return
			}
			id, err := Identify(ctx, ip, item.Port)
			if err != nil {
				mark(item.Destination, false)
				return
			}
			if id.NoCertificate {
				if err := st.TagNoCertificate(item.Destination); err != nil {
					mark(item.Destination, false)
					return
				}
				mark(item.Destination, true)
				return
			}
			if id.Name == "" && !id.SelfSigned {
				mark(item.Destination, false)
				return
			}
			if err := st.TagActivity(item.Destination, id.Name, id.SelfSigned); err != nil {
				mark(item.Destination, false)
				return
			}
			mark(item.Destination, true)
		}(item)
	}
	wait.Wait()
}
