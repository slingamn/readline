package readline

import (
	"io"
	"os"
	"sync"
	"sync/atomic"
)

var (
	Stdin  io.ReadCloser  = os.Stdin
	Stdout io.WriteCloser = os.Stdout
	Stderr io.WriteCloser = os.Stderr
)

var (
	std     *Instance
	stdOnce sync.Once
)

// global instance will not submit history automatic
func getInstance() *Instance {
	stdOnce.Do(func() {
		std, _ = NewEx(&Config{
			DisableAutoSaveHistory: true,
		})
	})
	return std
}

// let readline load history from filepath
// and try to persist history into disk
// set fp to "" to prevent readline persisting history to disk
// so the `AddHistory` will return nil error forever.
func SetHistoryPath(fp string) {
	ins := getInstance()
	cfg := ins.Config.Clone()
	cfg.HistoryFile = fp
	ins.SetConfig(cfg)
}

// set auto completer to global instance
func SetAutoComplete(completer AutoCompleter) {
	ins := getInstance()
	cfg := ins.Config.Clone()
	cfg.AutoComplete = completer
	ins.SetConfig(cfg)
}

// add history to global instance manually
// raise error only if `SetHistoryPath` is set with a non-empty path
func AddHistory(content string) error {
	ins := getInstance()
	return ins.SaveHistory(content)
}

func Password(prompt string) ([]byte, error) {
	ins := getInstance()
	return ins.ReadPassword(prompt)
}

// readline with global configs
func Line(prompt string) (string, error) {
	ins := getInstance()
	ins.SetPrompt(prompt)
	return ins.Readline()
}

type CancelableStdin struct {
	r      io.Reader
	mutex  sync.Mutex
	stop   chan struct{}
	closed int32
	notify chan struct{}
	data   []byte
	read   int
	err    error
}

func NewCancelableStdin(r io.Reader) *CancelableStdin {
	c := &CancelableStdin{
		r:      r,
		notify: make(chan struct{}),
		stop:   make(chan struct{}),
	}
	go c.ioloop()
	return c
}

func (c *CancelableStdin) ioloop() {
loop:
	for {
		select {
		case <-c.notify:
			c.read, c.err = c.r.Read(c.data)
			select {
			case c.notify <- struct{}{}:
			case <-c.stop:
				break loop
			}
		case <-c.stop:
			break loop
		}
	}
}

func (c *CancelableStdin) Read(b []byte) (n int, err error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if atomic.LoadInt32(&c.closed) == 1 {
		return 0, io.EOF
	}

	c.data = b
	select {
	case c.notify <- struct{}{}:
	case <-c.stop:
		return 0, io.EOF
	}
	select {
	case <-c.notify:
		return c.read, c.err
	case <-c.stop:
		return 0, io.EOF
	}
}

func (c *CancelableStdin) Close() error {
	if atomic.CompareAndSwapInt32(&c.closed, 0, 1) {
		close(c.stop)
	}
	return nil
}

// FillableStdin is a stdin reader which can prepend some data before
// reading into the real stdin
type FillableStdin struct {
	sync.Mutex
	stdin       io.ReadCloser
	buf         []byte
}

// NewFillableStdin gives you FillableStdin
func NewFillableStdin(stdin io.ReadCloser) io.ReadWriteCloser {
	return &FillableStdin{
		stdin:       stdin,
	}
}

// Write adds data to the buffer that is prepended to the real stdin.
func (s *FillableStdin) Write(p []byte) (n int, err error) {
	s.Lock()
	defer s.Unlock()
	s.buf = append(s.buf, p...)
	return len(p), nil
}

// Read will read from the local buffer and if no data, read from stdin
func (s *FillableStdin) Read(p []byte) (n int, err error) {
	s.Lock()
	if len(s.buf) > 0 {
		// copy buffered data, slide back and reslice
		n = copy(p, s.buf)
		remaining := copy(s.buf, s.buf[n:])
		s.buf = s.buf[:remaining]
	}
	s.Unlock()

	if n > 0 {
		return n, nil
	}

	return s.stdin.Read(p)
}

func (s *FillableStdin) Close() error {
	return s.stdin.Close()
}
