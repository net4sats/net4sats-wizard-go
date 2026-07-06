// net4sats-wizard — cross-platform router onboarding wizard.
// Single binary: serves web UI + API, auto-discovers routers,
// deploys net4sats over SSH.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

var lnAddrRe = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

// validLightningAddress reports whether s is a plausible Lightning/LNURL
// address (email-shaped: localpart@domain.tld). The Lightning address is a
// required MVP field — payouts route here, so an empty/invalid value would
// silently send payments nowhere. The check is intentionally lenient about
// the domain; actual resolution happens at payout time on the router.
func validLightningAddress(s string) bool {
	return lnAddrRe.MatchString(strings.TrimSpace(s))
}

const listenAddr = ":8099"

// ─── Job tracking ─────────────────────────────────────────────

type Step struct {
	Name   string `json:"name"`
	Desc   string `json:"desc"`
	Status string `json:"status"` // pending | running | done | failed
	Detail string `json:"detail,omitempty"`
}

type LogEntry struct {
	Time float64 `json:"time"`
	Msg  string  `json:"msg"`
}

type Job struct {
	mu     sync.Mutex
	IP     string     `json:"ip"`
	Status string     `json:"status"` // running | done | failed
	Step   int        `json:"step"`
	Steps  []Step     `json:"steps"`
	Log    []LogEntry `json:"log"`
	Error  string     `json:"error,omitempty"`
}

var (
	jobs      = make(map[string]*Job)
	jobsMutex sync.RWMutex
)

func newJob(ip string) *Job {
	return &Job{
		IP:     ip,
		Status: "running",
		Step:   0,
		Steps:  deploySteps(),
		Log:    []LogEntry{},
	}
}

func (j *Job) addLog(msg string) {
	j.mu.Lock()
	j.Log = append(j.Log, LogEntry{Time: float64(time.Now().Unix()), Msg: msg})
	j.mu.Unlock()
}

func (j *Job) setStep(i int, status, detail string) {
	j.mu.Lock()
	if i < len(j.Steps) {
		j.Step = i
		j.Steps[i].Status = status
		if detail != "" {
			j.Steps[i].Detail = detail
		}
	}
	j.mu.Unlock()
}

// ─── API handlers ─────────────────────────────────────────────

func handleScan(w http.ResponseWriter, r *http.Request) {
	routers := discoverRouters()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"routers": routers})
}

type deployRequest struct {
	IP       string `json:"ip"`
	Password string `json:"password"`
	Mode     string `json:"mode"`     // wan | sta
	SSID     string `json:"ssid"`     // for sta mode
	WifiPass string `json:"wifiPass"` // for sta mode
	LNURL    string `json:"lnurl"`    // Lightning address
}

func handleDeploy(w http.ResponseWriter, r *http.Request) {
	var req deployRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	if req.IP == "" || req.Password == "" {
		writeError(w, 400, "IP and password required")
		return
	}
	if !validLightningAddress(req.LNURL) {
		writeError(w, 400, "a valid Lightning address is required")
		return
	}

	jobID := fmt.Sprintf("%d", time.Now().UnixNano()%100000000)
	job := newJob(req.IP)

	jobsMutex.Lock()
	jobs[jobID] = job
	jobsMutex.Unlock()

	go runDeployment(job, req)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"job_id": jobID})
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	jobID := r.URL.Path[len("/api/status/"):]
	jobsMutex.RLock()
	job, ok := jobs[jobID]
	jobsMutex.RUnlock()
	if !ok {
		writeError(w, 404, "job not found")
		return
	}
	// Return a snapshot for thread-safe JSON
	job.mu.Lock()
	snapshot := struct {
		IP     string     `json:"ip"`
		Status string     `json:"status"`
		Step   int        `json:"step"`
		Steps  []Step     `json:"steps"`
		Log    []LogEntry `json:"log"`
		Error  string     `json:"error,omitempty"`
	}{job.IP, job.Status, job.Step, job.Steps, job.Log, job.Error}
	job.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(snapshot)
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(indexHTML)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/scan", handleScan)
	mux.HandleFunc("/api/deploy", handleDeploy)
	mux.HandleFunc("/api/status/", handleStatus)
	mux.HandleFunc("/", handleIndex)

	// CORS for local dev
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		mux.ServeHTTP(w, r)
	})

	fmt.Printf("net4sats wizard running on http://localhost%s\n", listenAddr)
	fmt.Println("Open this URL in your browser to set up a router.")
	log.Fatal(http.ListenAndServe(listenAddr, handler))
	_ = io.Discard // keep import
}
