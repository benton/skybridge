package ui

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"sync"
	"time"

	"git.h.usky.io/huskynet/skybridge/internal/bridge"
	"git.h.usky.io/huskynet/skybridge/internal/discovery"
)

// Deps is what the controller needs from the rest of the program; the cli
// package wires the real hardware in and tests wire fakes.
type Deps struct {
	// Open validates s and returns the opener the bridge will use. opened is
	// called with the path and speed each time a port is opened.
	Open func(s Settings, log *slog.Logger, opened func(path string, baud int)) (bridge.Opener, error)
	// Ports lists the machine's serial ports, best candidate first.
	Ports func(ctx context.Context) ([]discovery.Candidate, error)
	// Probe talks to the mount on the configured port and identifies it.
	Probe func(ctx context.Context, s Settings, log *slog.Logger) (ProbeResult, error)
	// Listen creates the TCP listener SkySafari connects to.
	Listen func(network, addr string) (net.Listener, error)
	// Save persists settings; nil means they are not persisted.
	Save    func(Settings) error
	Retry   time.Duration
	Version string
}

// ProbeResult is the outcome of the last probe, success or not.
type ProbeResult struct {
	Profile string `json:"profile,omitempty"`
	Path    string `json:"path,omitempty"`
	Baud    int    `json:"baud,omitempty"`
	Reply   string `json:"reply,omitempty"`
	Error   string `json:"error,omitempty"`
	At      string `json:"at"` // HH:MM:SS
}

// SerialState is the serial side of the chain.
type SerialState struct {
	Status string `json:"status"` // closed, waiting or open
	Path   string `json:"path,omitempty"`
	Baud   int    `json:"baud,omitempty"`
	Since  string `json:"since,omitempty"`
}

// ClientState is the SkySafari side of the chain.
type ClientState struct {
	Connected bool   `json:"connected"`
	Addr      string `json:"addr,omitempty"`
	Since     string `json:"since,omitempty"`
}

// Traffic counts bytes relayed since the bridge was last started.
type Traffic struct {
	ToApp   int64 `json:"toApp"`   // mount to SkySafari
	ToMount int64 `json:"toMount"` // SkySafari to mount
}

// State is everything the panel shows.
type State struct {
	// Seq rises with every published change so the page can ignore a
	// snapshot that arrives after a newer event.
	Seq      uint64       `json:"seq"`
	Version  string       `json:"version"`
	Running  bool         `json:"running"`
	Listen   string       `json:"listen,omitempty"` // bound address while running
	Serial   SerialState  `json:"serial"`
	Client   ClientState  `json:"client"`
	Traffic  Traffic      `json:"traffic"`
	Settings Settings     `json:"settings"`
	Error    string       `json:"error,omitempty"` // why the bridge is not running
	Probe    *ProbeResult `json:"probe,omitempty"`
}

// Event is one server-sent event: a state snapshot or a log line.
type Event struct {
	Name string
	Data []byte
}

var (
	ErrRunning = errors.New("stop the bridge before probing")
)

// Controller owns the bridge's lifecycle and the state the panel shows.
type Controller struct {
	deps Deps
	log  *slog.Logger
	logs *LogBuffer

	lc sync.Mutex // serialises Start, Stop, Apply and Probe

	mu      sync.Mutex
	state   State
	run     *run
	subs    map[chan Event]struct{}
	pending bool        // a traffic publish is scheduled
	gone    *time.Timer // counts down the client grace period

	closing   chan struct{} // closed once by BeginShutdown
	closeOnce sync.Once
}

// clientGrace is how long a client is still shown as connected after it
// drops. SkySafari closes and reopens its connection constantly, often
// several times a second, and the panel should not flicker along with it.
// Tests shorten it.
var clientGrace = 2 * time.Second

type run struct {
	cancel context.CancelFunc
	done   chan struct{}
}

const trafficPublishEvery = 250 * time.Millisecond

// NewController makes a stopped controller. Its logger writes into the
// returned buffer and, when extra is non-nil, there too.
func NewController(deps Deps, settings Settings, level slog.Level, extra slog.Handler) *Controller {
	c := &Controller{deps: deps, subs: map[chan Event]struct{}{}, closing: make(chan struct{})}
	c.logs = NewLogBuffer(level, func(l LogLine) {
		b, _ := json.Marshal(l)
		c.broadcast(Event{"log", b})
	})
	var h slog.Handler = c.logs
	if extra != nil {
		h = Tee(c.logs, extra)
	}
	c.log = slog.New(h)
	c.state = State{Version: deps.Version, Settings: settings, Serial: SerialState{Status: "closed"}}
	return c
}

// Logger is the controller's logger; the cli uses it for its own messages.
func (c *Controller) Logger() *slog.Logger { return c.log }

// Logs returns the buffered log lines, oldest first.
func (c *Controller) Logs() []LogLine { return c.logs.Lines() }

// State returns a snapshot.
func (c *Controller) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// Settings returns the current settings.
func (c *Controller) Settings() Settings {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state.Settings
}

// BeginShutdown tells every open event stream to end so the HTTP server can
// finish its graceful shutdown. It does not stop the bridge; call Stop for
// that once no request can start it again.
func (c *Controller) BeginShutdown() {
	c.closeOnce.Do(func() { close(c.closing) })
}

// Closing is closed once BeginShutdown has been called.
func (c *Controller) Closing() <-chan struct{} { return c.closing }

// Subscribe returns a channel of events and a function to stop them.
func (c *Controller) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 64)
	c.mu.Lock()
	c.subs[ch] = struct{}{}
	c.mu.Unlock()
	return ch, func() {
		c.mu.Lock()
		delete(c.subs, ch)
		c.mu.Unlock()
	}
}

func (c *Controller) broadcast(ev Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for ch := range c.subs {
		select {
		case ch <- ev:
		default: // a slow reader misses an event; the next state resyncs it
		}
	}
}

// publish sends the current state to every subscriber. Call without the lock.
func (c *Controller) publish() {
	c.mu.Lock()
	c.state.Seq++
	snapshot := c.state
	c.mu.Unlock()
	b, _ := json.Marshal(snapshot)
	c.broadcast(Event{"state", b})
}

func clock() string { return time.Now().Format("15:04:05") }

// Start brings the bridge up with the current settings. It is a no-op when
// already running. Errors are also recorded in State.Error.
func (c *Controller) Start() error {
	c.lc.Lock()
	defer c.lc.Unlock()
	return c.start()
}

func (c *Controller) start() error {
	c.mu.Lock()
	if c.run != nil {
		c.mu.Unlock()
		return nil
	}
	settings := c.state.Settings
	c.mu.Unlock()

	opener, err := c.deps.Open(settings, c.log, c.portOpened)
	if err == nil {
		var ln net.Listener
		ln, err = c.deps.Listen("tcp", settings.Listen)
		if err == nil {
			c.launch(ln, opener)
			c.log.Info("listening", "addr", ln.Addr().String(), "mount", settings.Mount)
			c.publish()
			return nil
		}
	}
	c.mu.Lock()
	c.state.Error = err.Error()
	c.mu.Unlock()
	c.log.Error("could not start", "err", err)
	c.publish()
	return err
}

func (c *Controller) launch(ln net.Listener, opener bridge.Opener) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &run{cancel: cancel, done: make(chan struct{})}
	c.mu.Lock()
	c.run = r
	c.state.Running = true
	c.state.Listen = ln.Addr().String()
	c.state.Error = ""
	c.state.Serial = SerialState{Status: "waiting", Since: clock()}
	c.state.Client = ClientState{}
	c.state.Traffic = Traffic{}
	c.mu.Unlock()

	go func() {
		defer close(r.done)
		err := bridge.Run(ctx, bridge.Config{
			Listener:   ln,
			Open:       opener,
			Logger:     c.log,
			RetryDelay: c.deps.Retry,
			Events: bridge.Events{
				PortLost:        c.portLost,
				ClientConnected: c.clientConnected,
				ClientGone:      c.clientGone,
				Traffic:         c.traffic,
			},
		})
		c.mu.Lock()
		if c.run == r {
			c.run = nil
		}
		c.state.Running = false
		c.state.Listen = ""
		c.state.Serial = SerialState{Status: "closed"}
		c.dropClient()
		if err != nil {
			c.state.Error = err.Error()
		}
		c.mu.Unlock()
		if err != nil {
			c.log.Error("bridge stopped", "err", err)
		} else {
			c.log.Info("stopped")
		}
		c.publish()
	}()
}

// Stop takes the bridge down and waits for it. It is a no-op when stopped.
func (c *Controller) Stop() {
	c.lc.Lock()
	defer c.lc.Unlock()
	c.stop()
}

func (c *Controller) stop() {
	c.mu.Lock()
	r := c.run
	c.mu.Unlock()
	if r == nil {
		return
	}
	r.cancel()
	<-r.done
}

// Running reports whether the bridge is up.
func (c *Controller) Running() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.run != nil
}

// Apply validates and saves s, then restarts the bridge if it was running.
func (c *Controller) Apply(s Settings) error {
	if _, err := c.deps.Open(s, slog.New(slog.DiscardHandler), nil); err != nil {
		return err
	}
	if c.deps.Save != nil {
		if err := c.deps.Save(s); err != nil {
			c.log.Warn("could not save settings", "err", err)
		}
	}
	c.lc.Lock()
	defer c.lc.Unlock()
	was := c.Running()
	c.stop()
	c.mu.Lock()
	c.state.Settings = s
	c.mu.Unlock()
	if was {
		return c.start()
	}
	c.publish()
	return nil
}

// Probe identifies the mount on the configured port. The bridge must be stopped.
func (c *Controller) Probe(ctx context.Context) (ProbeResult, error) {
	c.lc.Lock()
	defer c.lc.Unlock()
	if c.Running() {
		return ProbeResult{}, ErrRunning
	}
	res, err := c.deps.Probe(ctx, c.Settings(), c.log)
	res.At = clock()
	if err != nil {
		res.Error = err.Error()
	}
	c.mu.Lock()
	c.state.Probe = &res
	c.mu.Unlock()
	c.publish()
	return res, err
}

// Ports lists the serial ports, best first.
func (c *Controller) Ports(ctx context.Context) ([]discovery.Candidate, error) {
	ports, err := c.deps.Ports(ctx)
	if ports == nil {
		ports = []discovery.Candidate{}
	}
	return ports, err
}

func (c *Controller) portOpened(path string, baud int) {
	c.mu.Lock()
	c.state.Serial = SerialState{Status: "open", Path: path, Baud: baud, Since: clock()}
	c.mu.Unlock()
	c.publish()
}

func (c *Controller) portLost(error) {
	c.mu.Lock()
	if c.state.Running {
		c.state.Serial = SerialState{Status: "waiting", Since: clock()}
	}
	c.dropClient()
	c.mu.Unlock()
	c.publish()
}

// dropClient clears the client at once. Call with the lock held.
func (c *Controller) dropClient() {
	if c.gone != nil {
		c.gone.Stop()
		c.gone = nil
	}
	c.state.Client = ClientState{}
}

func (c *Controller) clientConnected(addr string) {
	c.mu.Lock()
	if c.gone != nil {
		// Back within the grace period: the same session as far as the
		// panel is concerned, so keep its start time.
		c.gone.Stop()
		c.gone = nil
		c.state.Client.Addr = addr
	} else {
		c.state.Client = ClientState{Connected: true, Addr: addr, Since: clock()}
	}
	c.mu.Unlock()
	c.publish()
}

func (c *Controller) clientGone() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.state.Client.Connected || c.gone != nil {
		return
	}
	var t *time.Timer
	t = time.AfterFunc(clientGrace, func() {
		c.mu.Lock()
		if c.gone != t { // a reconnect cancelled this countdown
			c.mu.Unlock()
			return
		}
		c.dropClient()
		c.mu.Unlock()
		c.publish()
	})
	c.gone = t
}

// traffic adds to the counters and publishes at most a few times a second.
func (c *Controller) traffic(toClient, toMount int) {
	c.mu.Lock()
	c.state.Traffic.ToApp += int64(toClient)
	c.state.Traffic.ToMount += int64(toMount)
	schedule := !c.pending
	c.pending = true
	c.mu.Unlock()
	if schedule {
		time.AfterFunc(trafficPublishEvery, func() {
			c.mu.Lock()
			c.pending = false
			c.mu.Unlock()
			c.publish()
		})
	}
}
