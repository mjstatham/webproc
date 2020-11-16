package agent

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/jpillora/requestlog"
	"github.com/jpillora/velox"
)

type agent struct {
	//log
	log      *log.Logger
	msgQueue chan msg
	//proc
	procState int64
	procReqs  chan string
	procSigs  chan os.Signal
	//http
	root http.Handler
	fs   http.Handler
	sync http.Handler
	//sync
	data struct {
		sync.Mutex
		velox.State
		Version       string
		Config        Config
		ChangedAt     time.Time
		Running       bool
		Manual        bool
		Pid, ExitCode int
		Files         map[string]string
		LogOffset     int64
		LogMaxSize    int64
		Log           map[int64]msg
	}
}

func Run(version string, c Config) error {
	a := &agent{}
	a.msgQueue = make(chan msg, 10000)
	agentWriter := &msgQueuer{"agent", a.msgQueue}
	a.log = log.New(io.MultiWriter(os.Stdout, agentWriter), "[webproc] ", log.LstdFlags)
	a.procState = procChanging
	a.procReqs = make(chan string)
	a.procSigs = make(chan os.Signal)
	//sync state
	a.data.State.Throttle = 250 * time.Millisecond
	a.data.Version = version
	a.data.Config = c
	a.data.Running = false
	a.data.Manual = c.OnExit == OnExitIgnore
	a.data.Files = map[string]string{}
	a.data.Log = map[int64]msg{}
	a.data.LogOffset = 0
	a.sync = velox.SyncHandler(&a.data)
	//http
	h := http.Handler(http.HandlerFunc(a.router))
	// log middleware (log everything!)
	var reqlogs io.Writer

	io.MultiWriter(os.Stdout, agentWriter)

	h = requestlog.WrapWith(h, requestlog.Options{
		Writer: reqlogs,
		Colors: &requestlog.Colors{},
		Format: `[webproc] {{ if .Timestamp }}{{ .Timestamp }} {{end}}` +
			`{{ .Method }} {{ .Path }} {{ .Code }} ` +
			`{{ .Duration }}{{ if .Size }} {{ .Size }}{{end}}` +
			`{{ if .IP }} ({{ .IP }}){{end}}` + "\n",
	})
	a.root = h

	//grab listener
	l, err := net.Listen("tcp", fmt.Sprintf("%s:%d", c.Host, c.Port))
	if err != nil {
		return fmt.Errorf("failed to start server: %s", err)
	}
	//threads
	go a.runProc(c)
	//load from disk
	a.readFiles()
	//watch files
	if c := a.watchFiles(); c != nil {
		defer c.Close()
	}
	//catch all signals
	go func() {
		signals := make(chan os.Signal)
		signal.Notify(signals)
		for sig := range signals {
			if sig.String() == "urgent I/O condition" {
				//ignore due to https://github.com/golang/go/issues/37942
				continue
			}
			if sig == os.Interrupt {
				a.log.Printf("webproc interupted, exiting...")
				if a.running() {
					a.procSigs <- os.Kill
					time.Sleep(100 * time.Millisecond)
				}
				os.Exit(0)
			}
			if !a.running() {
				a.log.Printf("ignored signal: %s", sig)
				continue
			}
			//proxy through to proc
			a.procSigs <- sig
		}
	}()
	//serve agent's root handler
	a.log.Printf("agent listening on http://%s:%d...", c.Host, c.Port)
	return http.Serve(l, a)
}

func (a *agent) setRunning(running bool, value int) {
	a.data.Lock()
	a.data.Running = running
	a.data.ChangedAt = time.Now()
	if running {
		atomic.StoreInt64(&a.procState, procRunning)
		a.data.Pid = value
		a.data.ExitCode = 0
	} else {
		atomic.StoreInt64(&a.procState, procExited)
		a.data.Pid = 0
		a.data.ExitCode = value
	}
	a.data.Unlock()
	a.data.Push()
}

func (a *agent) running() bool {
	return atomic.LoadInt64(&a.procState) == procRunning
}

func (a *agent) restart() {
	a.procReqs <- "restart"
}

func (a *agent) readFiles() bool {
	a.data.Lock()
	changed := false
	for i, path := range a.data.Config.ConfigurationFiles {
		b, err := ioutil.ReadFile(path)
		if err != nil {
			a.log.Printf("failed to read configuration file (#%d): %s", i, path)
			continue
		}
		existing := a.data.Files[path]
		curr := string(b)
		if curr != existing {
			a.data.Files[path] = curr
			a.log.Printf("loaded config file '%s' from disk", path)
			changed = true
		}
	}
	a.data.Unlock()
	if changed {
		a.data.Push()
	}
	return changed
}

func (a *agent) watchFiles() io.Closer {
	restart := a.data.Config.RestartWatch
	//fsnotify
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil
	}
	//watch handler
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&fsnotify.Write != fsnotify.Write {
					continue
				}
				if !a.readFiles() {
					continue
				}
				if restart {
					a.restart()
				}
			}

		}
	}()
	//watch all config files
	for _, path := range a.data.Config.ConfigurationFiles {
		if err := watcher.Add(path); err != nil {
			return nil
		}
	}
	//success
	return watcher
}
