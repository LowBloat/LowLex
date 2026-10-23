package main

import (
	"log"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
)

// setMemoryLimit keeps the Go heap under the container's memory limit. Without it the
// garbage collector does not know about the cgroup limit, lets the heap grow to it and the
// kernel then throttles the process. GOMEMLIMIT, when set, wins.
func setMemoryLimit() {
	if os.Getenv("GOMEMLIMIT") != "" {
		return
	}
	for _, path := range []string{
		"/sys/fs/cgroup/memory.max",                   // cgroup v2
		"/sys/fs/cgroup/memory/memory.limit_in_bytes", // cgroup v1
	} {
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		limit, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
		if err != nil || limit <= 0 || limit > 1<<50 { // "max" or the v1 "unlimited" value
			return
		}
		soft := limit / 10 * 8
		debug.SetMemoryLimit(soft)
		log.Printf("memory limit %d MB: keeping the heap under %d MB", limit>>20, soft>>20)
		return
	}
}
